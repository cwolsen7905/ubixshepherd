// Package fold keeps the flock's work apart: lanes, each a branch and a worktree with a
// declared scope. The daemon runs it; clients reach it through the API.
package fold

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ErrRefused is wrapped by errors that are the caller's to fix (a dirty worktree, an
// unmerged branch, a name in use), as opposed to failures.
var ErrRefused = errors.New("refused")

// refusal reads as its reason alone and matches ErrRefused.
type refusal struct{ msg string }

func (r refusal) Error() string        { return r.msg }
func (r refusal) Is(target error) bool { return target == ErrRefused }

func refuse(format string, a ...any) error {
	return refusal{fmt.Sprintf(format, a...)}
}

// Fold opens and closes lanes.
type Fold struct {
	Store store.Store
	// Config is the configuration until the first SetConfig; after that SetConfig's
	// wins. A daemon that reloads its configuration calls SetConfig, never assigns this.
	Config config.Config
	// Exe is the shepherd binary the pre-push hook runs; empty skips installing it.
	Exe string
	// ForgeFor, when set, lets import ask the forge whether a branch's merge request has
	// merged (git alone cannot tell after a squash merge or a history rewrite).
	ForgeFor func(remote string) (forge.Forge, error)
	// ArtifactDir is where artifact snapshots are kept (~/.shepherd/artifacts); empty
	// takes none, and a close then keeps nothing of a lane's artifacts.
	ArtifactDir string

	cfg   atomic.Pointer[config.Config]
	mu    sync.Mutex
	repos map[int64]*sync.Mutex
}

// SetConfig replaces the configuration, safely while lanes are opened, closed and
// checked. Each operation reads the configuration once, so a reload never splits one.
func (f *Fold) SetConfig(c config.Config) { f.cfg.Store(&c) }

// Conf is the configuration for one operation: the last SetConfig's, else Config.
func (f *Fold) Conf() config.Config {
	if c := f.cfg.Load(); c != nil {
		return *c
	}
	return f.Config
}

// repoLock serialises git work on one repo; different repos proceed in parallel.
func (f *Fold) repoLock(id int64) *sync.Mutex {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.repos == nil {
		f.repos = map[int64]*sync.Mutex{}
	}
	if f.repos[id] == nil {
		f.repos[id] = &sync.Mutex{}
	}
	return f.repos[id]
}

// OpenRequest asks for a lane.
type OpenRequest struct {
	RepoID int64  `json:"repo_id"`
	Name   string `json:"name"`
	// Branch defaults to Name.
	Branch string   `json:"branch,omitempty"`
	Scope  []string `json:"scope"`
	// Origin is who asks, and from where; the lane keeps it.
	Origin store.Origin `json:"origin"`
}

// A lane name is a branch-like slug with at most one slash: fix-login, feat/m2-leases.
var laneName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)?$`)

// Opened is a new lane, with the shared paths its scope takes and anything worth
// telling the person who opened it.
type Opened struct {
	store.Lane
	Shared []string `json:"shared,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// Open creates the lane's branch from a fresh copy of the repo's base branch, its
// worktree, and its record, or nothing at all. A scope that overlaps an open lane's is
// refused, naming the lane and the paths.
func (f *Fold) Open(ctx context.Context, req OpenRequest) (Opened, error) {
	if !laneName.MatchString(req.Name) {
		return Opened{}, refuse("lane name %q: use lowercase letters, digits, '.', '_' and '-', with at most one '/' (fix-login, feat/m2-leases)", req.Name)
	}
	if req.Branch == "" {
		req.Branch = req.Name
	}
	if err := checkScope(req.Scope); err != nil {
		return Opened{}, err
	}
	repo, err := f.Store.Repo(ctx, req.RepoID)
	if err != nil {
		return Opened{}, fmt.Errorf("repo %d: %w", req.RepoID, err)
	}
	lock := f.repoLock(repo.ID)
	lock.Lock()
	defer lock.Unlock()

	if !git.Ok(ctx, repo.Path, "check-ref-format", "--branch", req.Branch) {
		return Opened{}, refuse("%q is not a valid branch name", req.Branch)
	}
	prof := f.Conf().Profile(repo.Name)
	wt := f.worktreePath(repo, prof, req.Name)
	if _, err := os.Stat(wt); err == nil {
		return Opened{}, refuse("%s already exists", wt)
	}

	start, err := freshBase(ctx, repo.Path, prof.BaseBranch)
	if err != nil {
		return Opened{}, err
	}
	if git.RefExists(ctx, repo.Path, "refs/heads/"+req.Branch) {
		return Opened{}, refuse("branch %s already exists in %s", req.Branch, repo.Name)
	}
	if git.RefExists(ctx, repo.Path, "refs/remotes/origin/"+req.Branch) {
		return Opened{}, refuse("branch %s already exists on origin", req.Branch)
	}

	// Scopes are leases: no two open lanes may claim the same path. Judged against the
	// files at the lane's starting point, plus globs for paths not created yet.
	files, err := listFiles(ctx, repo.Path, start)
	if err != nil {
		return Opened{}, err
	}
	others, err := f.Store.Lanes(ctx, repo.ID)
	if err != nil {
		return Opened{}, err
	}
	// Approved check files are nobody's to change: they are left out of the overlap
	// test, and reported as excluded from this lane's reach.
	protected, err := f.Protected(ctx, repo.ID)
	if err != nil {
		return Opened{}, err
	}
	excluded, err := judgeScope(req.Scope, files, protected)
	if err != nil {
		return Opened{}, err
	}
	for _, o := range others {
		if both := scope.OverlapWithout(req.Scope, o.Scope, files, CheckPaths(protected)); len(both) > 0 {
			return Opened{}, refuse("scope overlaps lane %s (scope %s) on %s. Narrow the scope, or wait for %s to close",
				o.Name, strings.Join(o.Scope, ", "), firstN(both, 5), o.Name)
		}
	}
	out := Opened{Shared: sharedTouched(prof.SharedPaths, req.Scope, files)}
	if note := excludedNote(excluded); note != "" {
		out.Notes = append(out.Notes, note)
	}

	// Record first, so a concurrent open of the same name loses at the store.
	lane, err := f.Store.CreateLane(ctx, store.Lane{
		RepoID: repo.ID, Name: req.Name, Branch: req.Branch, Base: prof.BaseBranch,
		Worktree: wt, Scope: req.Scope, State: store.LaneOpening, Origin: req.Origin,
	})
	if errors.Is(err, store.ErrConflict) {
		return Opened{}, refuse("%v", err)
	}
	if err != nil {
		return Opened{}, err
	}
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		f.Store.DeleteLane(ctx, lane.ID)
		return Opened{}, err
	}
	if _, err := git.Run(ctx, repo.Path, "worktree", "add", "--quiet", "--no-track", "-b", req.Branch, wt, start); err != nil {
		f.Store.DeleteLane(ctx, lane.ID)
		return Opened{}, err
	}
	if err := f.Store.SetLaneState(ctx, lane.ID, store.LaneOpen); err != nil {
		return Opened{}, err
	}
	if note := f.setup(ctx, repo, prof.Setup, wt); note != "" {
		out.Notes = append(out.Notes, note)
	}
	if note := f.ensureHook(ctx, repo.Path, wt); note != "" {
		out.Notes = append(out.Notes, note)
	}
	// Silent here: adding the repo announced it, and a note would repeat on every open.
	f.ensureExclude(ctx, repo, false)
	out.Lane, err = f.Store.Lane(ctx, lane.ID)
	return out, err
}

// SetupTimeout bounds a lane's setup command.
const SetupTimeout = 15 * time.Minute

// setup runs the repo's setup command in a new worktree. A failure is a note on the
// open, not a failed open: the lane exists, and the person can finish the setup.
func (f *Fold) setup(ctx context.Context, repo store.Repo, cmdline, dir string) string {
	if strings.TrimSpace(cmdline) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, SetupTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", cmdline)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdline)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SHEPHERD_REPO="+repo.Path, "SHEPHERD_WORKTREE="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(redact.String(string(out))), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		return fmt.Sprintf("setup failed (%v); finish it by hand in the worktree:\n    %s", err, strings.Join(lines, "\n    "))
	}
	return "setup done: " + cmdline
}

// worktreePath is <workspace>/<repo>-worktrees/<lane>, or under the profile's root.
// A slash in the lane name becomes a dash, so each lane is one directory.
func (f *Fold) worktreePath(repo store.Repo, prof config.Profile, name string) string {
	root := prof.WorktreeRoot
	switch {
	case root == "":
		root = repo.Path + "-worktrees"
	case !filepath.IsAbs(root):
		root = filepath.Join(filepath.Dir(repo.Path), root)
	}
	return filepath.Join(root, strings.ReplaceAll(name, "/", "-"))
}

// freshBase fetches origin and returns origin/<base>, so a lane starts from what the
// remote has now, not from a stale local branch. Without an origin it uses local <base>.
func freshBase(ctx context.Context, repo, base string) (string, error) {
	if !git.HasRemote(ctx, repo, "origin") {
		if !git.RefExists(ctx, repo, "refs/heads/"+base) {
			return "", refuse("no origin remote and no local branch %s", base)
		}
		return base, nil
	}
	if _, err := git.Run(ctx, repo, "fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	if !git.RefExists(ctx, repo, "refs/remotes/origin/"+base) {
		return "", refuse("origin has no branch %s (set base_branch in this repo's profile)", base)
	}
	return "origin/" + base, nil
}

// checkScope accepts globs relative to the repo: "internal/lease/**", "README.md".
func checkScope(scope []string) error {
	if len(scope) == 0 {
		return refuse("a lane needs a scope: the paths its work stays inside (--scope 'internal/lease/**')")
	}
	for _, g := range scope {
		clean := path.Clean(g)
		switch {
		case strings.TrimSpace(g) == "":
			return refuse("empty scope glob")
		case path.IsAbs(g) || filepath.IsAbs(g):
			return refuse("scope %q: globs are relative to the repo", g)
		case clean == ".." || strings.HasPrefix(clean, "../"):
			return refuse("scope %q leaves the repo", g)
		}
		if _, err := path.Match(strings.ReplaceAll(g, "**", "*"), ""); err != nil {
			return refuse("scope %q: %v", g, err)
		}
	}
	return nil
}

// Rescope adds globs to a lane's scope and removes others. An addition that overlaps
// another open lane's scope is refused, as at lane open; so is one that reaches only
// approved check files.
func (f *Fold) Rescope(ctx context.Context, laneID int64, add, remove []string) (store.Lane, error) {
	lane, _, err := f.RescopeExcluding(ctx, laneID, add, remove)
	return lane, err
}

// RescopeExcluding is Rescope that also returns the approved check files the lane's new
// scope reaches but may not change.
func (f *Fold) RescopeExcluding(ctx context.Context, laneID int64, add, remove []string) (store.Lane, []string, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return lane, nil, err
	}
	if lane.State != store.LaneOpen {
		return lane, nil, refuse("lane %s is %s", lane.Name, lane.State)
	}
	if len(add) > 0 {
		if err := checkScope(add); err != nil {
			return lane, nil, err
		}
	}
	lock := f.repoLock(lane.RepoID)
	lock.Lock()
	defer lock.Unlock()
	repo, err := f.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return lane, nil, err
	}
	protected, err := f.Protected(ctx, repo.ID)
	if err != nil {
		return lane, nil, err
	}
	if len(add) > 0 {
		// A repo whose directory is gone has no files to judge by; the globs still are.
		// Any other failure to list is an error, never an empty listing.
		var files []string
		if exists(repo.Path) {
			if files, err = listFiles(ctx, repo.Path, "HEAD"); err != nil {
				return lane, nil, err
			}
		}
		if _, err := judgeScope(add, files, protected); err != nil {
			return lane, nil, err
		}
		others, err := f.Store.Lanes(ctx, repo.ID)
		if err != nil {
			return lane, nil, err
		}
		for _, o := range others {
			if o.ID == lane.ID {
				continue
			}
			if both := scope.OverlapWithout(add, o.Scope, files, CheckPaths(protected)); len(both) > 0 {
				return lane, nil, refuse("adding %s overlaps lane %s (scope %s) on %s", strings.Join(add, ", "), o.Name, strings.Join(o.Scope, ", "), firstN(both, 5))
			}
		}
	}
	drop := map[string]bool{}
	for _, r := range remove {
		drop[r] = true
	}
	var next []string
	seen := map[string]bool{}
	for _, g := range append(append([]string{}, lane.Scope...), add...) {
		if !drop[g] && !seen[g] {
			seen[g] = true
			next = append(next, g)
		}
	}
	if len(next) == 0 {
		return lane, nil, refuse("a lane needs a scope; that would leave lane %s with none", lane.Name)
	}
	if err := f.Store.SetLaneScope(ctx, lane.ID, next); err != nil {
		return lane, nil, err
	}
	lane, err = f.Store.Lane(ctx, lane.ID)
	return lane, ExcludedFrom(lane.Scope, protected), err
}

// CloseResult says what closing did.
type CloseResult struct {
	Lane          store.Lane `json:"lane"`
	BranchDeleted bool       `json:"branch_deleted"`
	Notes         []string   `json:"notes,omitempty"`
}

// Close removes a lane's worktree and closes it. Without force it refuses a worktree
// with uncommitted changes, and a branch git cannot see merged into origin/<base>. A
// squash merge looks unmerged to git; proving merges from the forge comes later.
func (f *Fold) Close(ctx context.Context, laneID int64, force bool) (CloseResult, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return CloseResult{}, err
	}
	if lane.State == store.LaneClosed {
		return CloseResult{}, refuse("lane %s is already closed", lane.Name)
	}
	if running, err := f.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err == nil && len(running) > 0 {
		return CloseResult{}, refuse("agent run %d is going in lane %s; stop it first (shepherd run stop %d)", running[0].ID, lane.Name, running[0].ID)
	}
	repo, err := f.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return CloseResult{}, err
	}
	lock := f.repoLock(repo.ID)
	lock.Lock()
	defer lock.Unlock()

	res := CloseResult{}
	_, statErr := os.Stat(lane.Worktree)
	present := statErr == nil
	// Snapshot first: whatever the close does next, the artifacts are copied out.
	if present {
		if err := f.SnapshotLane(ctx, lane.ID); err != nil {
			res.Notes = append(res.Notes, "artifact snapshot: "+err.Error())
		}
	}
	letGo := false
	if present {
		dirty, err := git.Dirty(ctx, lane.Worktree)
		if err != nil {
			return res, err
		}
		if dirty != "" && !force {
			ignorable, blocking, err := f.closeDirt(ctx, lane)
			if err != nil {
				return res, err
			}
			if len(blocking) > 0 {
				return res, refuse("%s has uncommitted changes:\n%s\ncommit or discard them, or close with --force (which discards them)", lane.Worktree,
					f.blockingWords(ctx, lane, blocking))
			}
			letGo = len(ignorable) > 0
		}
	}

	merged, where := f.merged(ctx, repo.Path, lane)
	if !merged {
		// A squash merge looks unmerged to git: the forge's word counts.
		if ok, why := f.forgeMerged(ctx, repo, lane); ok {
			merged, where = true, "the forge's merged request"
			res.Notes = append(res.Notes, "merged, per the forge")
			if why != "" {
				res.Notes = append(res.Notes, why)
			}
		}
	}
	if !merged && !force {
		return res, refuse("branch %s is not in %s as far as git can tell. A squash merge looks like this too: once the MR is merged, close with --force (the branch is kept)", lane.Branch, where)
	}

	plan := f.planArtifacts(ctx, lane, repo.Path)
	if present {
		if err := removeWorktree(ctx, repo.Path, lane.Worktree, force || letGo); err != nil {
			return res, err
		}
	} else {
		res.Notes = append(res.Notes, "worktree was already gone; pruned git's record of it")
		git.Run(ctx, repo.Path, "worktree", "prune")
	}

	if git.RefExists(ctx, repo.Path, "refs/heads/"+lane.Branch) {
		if merged {
			// Merged into origin/<base> is checked above; -d would check against HEAD.
			if _, err := git.Run(ctx, repo.Path, "branch", "-D", lane.Branch); err != nil {
				return res, err
			}
			res.BranchDeleted = true
		} else {
			res.Notes = append(res.Notes, fmt.Sprintf("kept branch %s: git cannot see it merged", lane.Branch))
		}
	}
	if err := f.Store.SetLaneState(ctx, lane.ID, store.LaneClosed); err != nil {
		return res, err
	}
	f.settleArtifacts(ctx, plan)
	res.Lane, err = f.Store.Lane(ctx, lane.ID)
	return res, err
}

// CloseMerged closes a lane whose merge the forge has proven (proof is the merge or
// squash commit), so a squash merge needs no --force. It still refuses a worktree with
// uncommitted changes, and a lane with an agent running: those are someone's work.
//
// head is the merged request's head commit: the lane's branch must hold nothing beyond
// it, or there is work the merge does not cover and the lane stays open.
func (f *Fold) CloseMerged(ctx context.Context, laneID int64, proof, head string) (CloseResult, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return CloseResult{}, err
	}
	if lane.State == store.LaneClosed {
		return CloseResult{}, refuse("lane %s is already closed", lane.Name)
	}
	if running, err := f.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err == nil && len(running) > 0 {
		return CloseResult{}, refuse("agent run %d is going in lane %s", running[0].ID, lane.Name)
	}
	repo, err := f.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return CloseResult{}, err
	}
	lock := f.repoLock(repo.ID)
	lock.Lock()
	defer lock.Unlock()

	res := CloseResult{}
	if git.RefExists(ctx, repo.Path, "refs/heads/"+lane.Branch) {
		if head == "" {
			return res, refuse("the forge gave no head commit for the merged request, so Shepherd cannot tell whether branch %s holds more", lane.Branch)
		}
		if !git.Ok(ctx, repo.Path, "cat-file", "-e", head+"^{commit}") {
			git.Run(ctx, repo.Path, "fetch", "--quiet", "origin")
		}
		if !git.Ok(ctx, repo.Path, "merge-base", "--is-ancestor", "refs/heads/"+lane.Branch, head) {
			return res, refuse("branch %s has commits beyond its merged request (head %s): work the merge does not cover", lane.Branch, shortSHA(head))
		}
	}
	plan := artifactPlan{}
	if _, err := os.Stat(lane.Worktree); err == nil {
		if err := f.SnapshotLane(ctx, lane.ID); err != nil {
			res.Notes = append(res.Notes, "artifact snapshot: "+err.Error())
		}
		dirty, err := git.Dirty(ctx, lane.Worktree)
		if err != nil {
			return res, err
		}
		letGo := false
		if dirty != "" {
			ignorable, blocking, err := f.closeDirt(ctx, lane)
			if err != nil {
				return res, err
			}
			if len(blocking) > 0 {
				return res, refuse("lane %s merged, but %s has uncommitted changes:\n%s", lane.Name, lane.Worktree, f.blockingWords(ctx, lane, blocking))
			}
			letGo = len(ignorable) > 0
		}
		plan = f.planArtifacts(ctx, lane, repo.Path)
		if err := removeWorktree(ctx, repo.Path, lane.Worktree, letGo); err != nil {
			return res, err
		}
	} else {
		plan = f.planArtifacts(ctx, lane, repo.Path)
		git.Run(ctx, repo.Path, "worktree", "prune")
	}
	if git.RefExists(ctx, repo.Path, "refs/heads/"+lane.Branch) {
		if _, err := git.Run(ctx, repo.Path, "branch", "-D", lane.Branch); err != nil {
			return res, err
		}
		res.BranchDeleted = true
	}
	res.Notes = append(res.Notes, "merged at "+shortSHA(proof)+", per the forge")
	if err := f.Store.SetLaneState(ctx, lane.ID, store.LaneClosed); err != nil {
		return res, err
	}
	f.settleArtifacts(ctx, plan)
	res.Lane, err = f.Store.Lane(ctx, lane.ID)
	return res, err
}

func shortSHA(s string) string {
	if len(s) > 9 {
		return s[:9]
	}
	return s
}

// MergeCovers is the one proof that a forge's merged request finished a lane, shared by
// lane close and the watcher so they cannot disagree. The request must be merged with a
// merge commit and a head, created after the lane opened (a branch name can be reused),
// and the lane's local branch, if it still exists, must hold nothing beyond the request's
// head. fetch is called, at most once and only when the head is not here yet, to get it;
// the caller decides how often and how long that may take.
func MergeCovers(ctx context.Context, repoPath string, lane store.Lane, mr *forge.MR, fetch func(context.Context)) bool {
	if mr == nil || !strings.EqualFold(mr.State, "merged") || mr.MergeSHA == "" || mr.SHA == "" {
		return false
	}
	if mr.Created.IsZero() || mr.Created.Before(lane.Created) {
		return false
	}
	if !git.RefExists(ctx, repoPath, "refs/heads/"+lane.Branch) {
		return true
	}
	if !git.Ok(ctx, repoPath, "cat-file", "-e", mr.SHA+"^{commit}") && fetch != nil {
		fetch(ctx)
	}
	return git.Ok(ctx, repoPath, "merge-base", "--is-ancestor", "refs/heads/"+lane.Branch, mr.SHA)
}

// forgeMerged reports whether the forge has merged a request for the lane's branch that
// covers it: created after the lane opened (a branch name can be reused), and the branch
// holds nothing beyond the request's head. It records the merge on the lane, so the
// lane's next-up item is settled as done. Without a forge it is false.
func (f *Fold) forgeMerged(ctx context.Context, repo store.Repo, lane store.Lane) (bool, string) {
	if f.ForgeFor == nil {
		return false, ""
	}
	fg, err := f.ForgeFor(repo.Remote)
	if err != nil || fg == nil {
		return false, ""
	}
	mr, err := fg.MRForBranch(ctx, lane.Branch)
	if err != nil || !MergeCovers(ctx, repo.Path, lane, mr, func(ctx context.Context) {
		git.Run(ctx, repo.Path, "fetch", "--quiet", "origin")
	}) {
		return false, ""
	}
	// The merge is recorded even for a lane the watcher never saw (no row yet), so its
	// next-up item settles as done; a failure to record is not hidden.
	lf, err := f.Store.LaneForge(ctx, lane.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return true, "could not record the merge on the lane: " + err.Error()
	}
	lf.LaneID = lane.ID
	lf.MR, lf.MRState, lf.MRURL, lf.MergeSHA = mr.IID, mr.State, mr.URL, mr.MergeSHA
	lf.MRCreated, lf.MRUpdated = mr.Created.UTC(), mr.Updated.UTC()
	if err := f.Store.PutLaneForge(ctx, lf); err != nil {
		return true, "could not record the merge on the lane: " + err.Error()
	}
	return true, ""
}

// merged reports whether the lane's branch is contained in its base, fetching origin
// first when there is one. A branch that no longer exists counts as merged only if the
// worktree has nothing on it either, which Close has already checked.
func (f *Fold) merged(ctx context.Context, repo string, lane store.Lane) (bool, string) {
	base := lane.Base
	target := base
	if git.HasRemote(ctx, repo, "origin") {
		git.Run(ctx, repo, "fetch", "--quiet", "origin")
		target = "origin/" + base
	}
	if !git.RefExists(ctx, repo, "refs/heads/"+lane.Branch) {
		return true, target
	}
	return git.Ok(ctx, repo, "merge-base", "--is-ancestor", "refs/heads/"+lane.Branch, target), target
}

// Stale is a worktree that looks finished, or a lane whose worktree is gone.
type Stale struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Lane   string `json:"lane,omitempty"`
	Reason string `json:"reason"`
}

// GC lists stale worktrees in a workspace's repos. It changes nothing, and it uses the
// remote refs as of each repo's last fetch.
func (f *Fold) GC(ctx context.Context, workspaceID int64) ([]Stale, error) {
	repos, err := f.Store.Repos(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var out []Stale
	for _, repo := range repos {
		lanes, err := f.Store.Lanes(ctx, repo.ID)
		if err != nil {
			return nil, err
		}
		laneAt := map[string]store.Lane{}
		for _, l := range lanes {
			laneAt[l.Worktree] = l
			if _, err := os.Stat(l.Worktree); err != nil {
				out = append(out, Stale{Repo: repo.Name, Path: l.Worktree, Branch: l.Branch, Lane: l.Name,
					Reason: "lane is open but its worktree is gone; close it with shepherd lane close --force"})
			}
		}
		wts, err := git.Worktrees(ctx, repo.Path)
		if err != nil {
			out = append(out, Stale{Repo: repo.Name, Path: repo.Path, Reason: err.Error()})
			continue
		}
		base := f.Conf().Profile(repo.Name).BaseBranch
		target := base
		if git.RefExists(ctx, repo.Path, "refs/remotes/origin/"+base) {
			target = "origin/" + base
		}
		for _, wt := range wts[1:] {
			if wt.Bare {
				continue
			}
			canon, _ := paths.Canonical(wt.Path)
			if _, ok := laneAt[canon]; ok {
				continue
			}
			s := Stale{Repo: repo.Name, Path: wt.Path, Branch: wt.Branch}
			switch {
			case !exists(wt.Path):
				s.Reason = "missing on disk (git worktree prune clears it)"
			case wt.Branch == "":
				s.Reason = "detached HEAD, no branch"
			case !git.RefExists(ctx, repo.Path, "refs/heads/"+wt.Branch):
				s.Reason = "its branch is gone"
			case wt.Branch != base && git.Ok(ctx, repo.Path, "merge-base", "--is-ancestor", "refs/heads/"+wt.Branch, target):
				s.Reason = "branch is contained in " + target + " (merged, or no new commits)"
			default:
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// listFiles lists the files at a commit, repo-relative with "/" separators.
func listFiles(ctx context.Context, repo, rev string) ([]string, error) {
	out, err := git.Run(ctx, repo, "ls-tree", "-r", "--name-only", rev)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// sharedTouched returns the shared-path globs a scope claims any file of (or would, for
// a literal shared path not created yet).
func sharedTouched(shared, sc, files []string) []string {
	var out []string
	for _, sp := range shared {
		if len(scope.Overlap([]string{sp}, sc, files)) > 0 {
			out = append(out, sp)
		}
	}
	return out
}

func firstN(s []string, n int) string {
	if len(s) > n {
		return strings.Join(s[:n], ", ") + fmt.Sprintf(" and %d more", len(s)-n)
	}
	return strings.Join(s, ", ")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], fmt.Sprintf("... and %d more", len(lines)-n))
	}
	return strings.Join(lines, "\n")
}
