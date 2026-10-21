package fold

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Approved check files are test files the person marked as "this is what done means".
// The mark lives in the store, pinned to a blob and a base commit; no lane may change a
// marked file whatever its scope says. This file holds the rules around the mark:
// approving, releasing, and the exclusion every path that reads a lane's scope applies.

// Protected returns the repo's live check files.
func (f *Fold) Protected(ctx context.Context, repoID int64) ([]store.CheckFile, error) {
	return f.Store.CheckFiles(ctx, repoID, true)
}

// CheckPaths are the paths of the check files.
func CheckPaths(cs []store.CheckFile) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path
	}
	return out
}

// ExcludedFrom lists the marked files a scope reaches: the files the lane would have
// held had they not been approved checks. It is for lane views, so it needs no git.
func ExcludedFrom(laneScope []string, protected []store.CheckFile) []string {
	var out []string
	for _, c := range protected {
		if scope.Any(laneScope, c.Path) {
			out = append(out, c.Path)
		}
	}
	return out
}

// askForChange is how a lane that thinks a check is wrong gets it changed.
const askForChange = "approved checks are the person's to change: raise a decision (ask_human) saying which check is wrong and why"

// judgeScope applies the exclusion rules to globs a lane is about to take, against the
// repo's files, and returns the marked files they reach. A scope whose every match is a
// marked file, or a literal naming one, is refused: the lane could do nothing with it.
func judgeScope(globs, files []string, protected []store.CheckFile) ([]string, error) {
	if len(protected) == 0 {
		return nil, nil
	}
	paths := CheckPaths(protected)
	for _, g := range globs {
		for _, p := range paths {
			if path.Clean(g) == p {
				return nil, refuse("scope %q names an approved check file; the lane could not change it. %s", g, askForChange)
			}
		}
	}
	matched := scope.Without(globs, files, nil)
	if len(matched) > 0 && len(scope.Without(globs, files, paths)) == 0 {
		return nil, refuse("scope %s reaches only approved check files (%s). %s", strings.Join(globs, ", "), firstN(matched, 5), askForChange)
	}
	return ExcludedFrom(globs, protected), nil
}

// excludedNote is the line a lane open or scope change prints for the files its scope
// reaches but may not change.
func excludedNote(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	return fmt.Sprintf("excluded: %d approved check files (%s)", len(excluded), firstN(excluded, 5))
}

// ChecksProblem is the push problem for changes to marked files, shared by the pre-push
// check and ship.
func ChecksProblem(files []string) string {
	return "changes approved check files (decided by the person; raise a decision to change them):\n    " +
		strings.Join(firstLinesN(files, 20), "\n    ")
}

// protectedAmong returns the files that are marked, in the order given.
func protectedAmong(files []string, protected []store.CheckFile) []string {
	mark := map[string]bool{}
	for _, c := range protected {
		mark[c.Path] = true
	}
	var out []string
	for _, f := range files {
		if mark[f] {
			out = append(out, f)
		}
	}
	return out
}

// ApproveRequest asks to mark files already on the base branch.
type ApproveRequest struct {
	RepoID int64
	Paths  []string
	// ItemID links the checks to a next-up item (0: none); Set labels a group.
	ItemID int64
	Set    string
	// By is who approved: "person" and the surface, e.g. "person (cli)".
	By string
}

// cleanCheckPath normalises a repo-relative path and refuses anything else.
func cleanCheckPath(p string) (string, error) {
	c := path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"))
	switch {
	case strings.TrimSpace(p) == "" || c == ".":
		return "", refuse("empty path")
	case path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../"):
		return "", refuse("%q: check files are paths inside the repo", p)
	case strings.ContainsAny(c, "*?[\\"):
		return "", refuse("%q: name each check file; patterns are not accepted", p)
	}
	return c, nil
}

// ApproveChecks marks files that are already on the repo's base branch (origin/<base>,
// freshly fetched), pinned to the blob and commit it reads there. It refuses a path
// that is missing, not a regular file, already marked, or inside an open lane's scope
// (a revision in flight). All or none.
func (f *Fold) ApproveChecks(ctx context.Context, req ApproveRequest) ([]store.CheckFile, error) {
	if len(req.Paths) == 0 {
		return nil, refuse("name the check files to approve")
	}
	if strings.TrimSpace(req.By) == "" {
		return nil, refuse("an approval records who made it")
	}
	var paths []string
	seen := map[string]bool{}
	for _, p := range req.Paths {
		c, err := cleanCheckPath(p)
		if err != nil {
			return nil, err
		}
		if !seen[c] {
			seen[c] = true
			paths = append(paths, c)
		}
	}
	repo, err := f.Store.Repo(ctx, req.RepoID)
	if err != nil {
		return nil, fmt.Errorf("repo %d: %w", req.RepoID, err)
	}
	if req.ItemID != 0 {
		if _, err := f.Store.NextItem(ctx, req.ItemID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, refuse("no next-up item %d", req.ItemID)
			}
			return nil, err
		}
	}
	lock := f.repoLock(repo.ID)
	lock.Lock()
	defer lock.Unlock()

	prof := f.Conf().Profile(repo.Name)
	base, err := freshBase(ctx, repo.Path, prof.BaseBranch)
	if err != nil {
		return nil, err
	}
	commit, err := git.Run(ctx, repo.Path, "rev-parse", base+"^{commit}")
	if err != nil {
		return nil, err
	}
	live, err := f.Protected(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	lanes, err := f.Store.Lanes(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	var marks []store.CheckFile
	for _, p := range paths {
		if len(protectedAmong([]string{p}, live)) > 0 {
			return nil, refuse("%s is already an approved check file", p)
		}
		for _, l := range lanes {
			// Store.Lanes leaves closed lanes out; a closed lane never blocks an approval.
			if l.State != store.LaneClosed && scope.Any(l.Scope, p) {
				return nil, refuse("%s is inside the scope of open lane %s (%s): a revision may be in flight. Close the lane or merge it first, then approve", p, l.Name, strings.Join(l.Scope, ", "))
			}
		}
		blob, err := blobOnBase(ctx, repo.Path, base, p)
		if err != nil {
			return nil, err
		}
		marks = append(marks, store.CheckFile{RepoID: repo.ID, Path: p, Blob: blob, BaseCommit: commit,
			ItemID: req.ItemID, Set: req.Set, ApprovedBy: req.By})
	}
	out, err := f.Store.ApproveCheckFiles(ctx, marks)
	if errors.Is(err, store.ErrConflict) {
		return nil, refuse("%v", err)
	}
	return out, err
}

// blobOnBase returns the blob id of a regular file at rev, or a refusal saying why not.
func blobOnBase(ctx context.Context, repo, rev, p string) (string, error) {
	mode, typ, sha, found, err := treeEntry(ctx, repo, rev, p)
	if err != nil {
		return "", err
	}
	if !found {
		return "", refuse("%s is not on %s: merge it first, then approve it", p, rev)
	}
	switch {
	case typ == "tree":
		return "", refuse("%s is a directory: name each check file", p)
	case typ != "blob" || (mode != "100644" && mode != "100755"):
		return "", refuse("%s is not a regular file on %s", p, rev)
	}
	return sha, nil
}

// treeEntry reads one path's entry at rev. found is false when the path is not there; any
// other failure of git is an error, never "absent".
func treeEntry(ctx context.Context, repo, rev, p string) (mode, typ, sha string, found bool, err error) {
	out, err := git.Run(ctx, repo, "ls-tree", "-z", rev, "--", p)
	if err != nil {
		return "", "", "", false, err
	}
	meta, _, _ := strings.Cut(out, "\t")
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return "", "", "", false, nil
	}
	return fields[0], fields[1], fields[2], true, nil
}

// ReleaseRequest asks to lift the marks on files.
type ReleaseRequest struct {
	RepoID int64
	Paths  []string
	Why    string
	// Force releases although a build lane for the checks' item is open.
	Force bool
}

// ReleaseChecks lifts the marks on paths with a reason. It refuses while a lane linked
// to the checks' next-up item is open, unless Force (the person typed it).
func (f *Fold) ReleaseChecks(ctx context.Context, req ReleaseRequest) ([]store.CheckFile, error) {
	if strings.TrimSpace(req.Why) == "" {
		return nil, refuse("say why the checks are released (--why)")
	}
	if len(req.Paths) == 0 {
		return nil, refuse("name the check files to release")
	}
	var paths []string
	for _, p := range req.Paths {
		c, err := cleanCheckPath(p)
		if err != nil {
			return nil, err
		}
		paths = append(paths, c)
	}
	if _, err := f.Store.Repo(ctx, req.RepoID); err != nil {
		return nil, fmt.Errorf("repo %d: %w", req.RepoID, err)
	}
	live, err := f.Protected(ctx, req.RepoID)
	if err != nil {
		return nil, err
	}
	byPath := map[string]store.CheckFile{}
	for _, c := range live {
		byPath[c.Path] = c
	}
	for _, p := range paths {
		c, ok := byPath[p]
		if !ok {
			return nil, refuse("%s is not an approved check file", p)
		}
		if req.Force || c.ItemID == 0 {
			continue
		}
		// Only "no such item" and "no lane" let a release through; a lookup that fails
		// for any other reason must not, or the guard is skipped when it is needed.
		it, err := f.Store.NextItem(ctx, c.ItemID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("check for a lane open for next-up item %d: %w", c.ItemID, err)
		}
		if it.LaneID == 0 {
			continue
		}
		l, err := f.Store.Lane(ctx, it.LaneID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("check for a lane open for next-up item %d: %w", c.ItemID, err)
		}
		if l.State != store.LaneClosed {
			return nil, refuse("lane %s is open for next-up item %d, which %s belongs to. Release once it closes, or release with force if that is what you mean", l.Name, c.ItemID, p)
		}
	}
	out, err := f.Store.ReleaseCheckFiles(ctx, req.RepoID, paths, strings.TrimSpace(req.Why))
	if errors.Is(err, store.ErrNotFound) {
		return nil, refuse("%v", err)
	}
	return out, err
}

// The states of a mark against the base branch (CheckState.State).
const (
	CheckCurrent = "current" // the base branch has the approved blob
	CheckChanged = "changed" // the base branch has a different blob
	CheckMissing = "missing" // the base branch no longer has the file
)

// CheckState is a live mark and whether the base branch still holds what was approved.
type CheckState struct {
	store.CheckFile
	State string `json:"state"`
	// BaseBlob is the blob now on the base branch, when it differs.
	BaseBlob string `json:"base_blob,omitempty"`
}

// CheckStatus reads each live mark against the base branch as of the repo's last fetch
// (origin/<base>, else the local base): a read does not reach for the network, and it
// works offline.
func (f *Fold) CheckStatus(ctx context.Context, repoID int64) ([]CheckState, error) {
	repo, err := f.Store.Repo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	live, err := f.Protected(ctx, repoID)
	if err != nil || len(live) == 0 {
		return nil, err
	}
	base := f.Conf().Profile(repo.Name).BaseBranch
	if git.RefExists(ctx, repo.Path, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	} else if !git.RefExists(ctx, repo.Path, "refs/heads/"+base) {
		return nil, refuse("no branch %s to read the approved checks against", base)
	}
	var out []CheckState
	for _, c := range live {
		st := CheckState{CheckFile: c, State: CheckCurrent}
		switch blob, err := blobOnBase(ctx, repo.Path, base, c.Path); {
		case err != nil && !errors.Is(err, ErrRefused):
			return nil, err
		case err != nil:
			st.State = CheckMissing
		case blob != c.Blob:
			st.State, st.BaseBlob = CheckChanged, blob
		}
		out = append(out, st)
	}
	return out, nil
}

// ShipProblems judges a lane's branch before Shepherd pushes it, because the pre-push
// hook can be skipped. A change to a marked file is a problem; so is a marked file whose
// blob at the lane's head is not the approved one, which also catches an edit that
// arrived by a rebase or merge. It returns nothing for a repo with no marks.
func ShipProblems(ctx context.Context, st store.Store, lane store.Lane) ([]string, error) {
	live, err := st.CheckFiles(ctx, lane.RepoID, true)
	if err != nil || len(live) == 0 {
		return nil, err
	}
	base := lane.Base
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Base) {
		base = "origin/" + lane.Base
	}
	out, err := git.Run(ctx, lane.Worktree, "diff", "--name-only", "-z", "--no-renames", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	changed := protectedAmong(splitNUL(out), live)
	var problems []string
	if len(changed) > 0 {
		problems = append(problems, ChecksProblem(changed))
	}
	var drift []string
	for _, c := range live {
		mode, typ, blob, found, err := treeEntry(ctx, lane.Worktree, "HEAD", c.Path)
		if err != nil {
			// A git that fails cannot show the file is unchanged.
			return nil, err
		}
		if !found {
			// Absent at the lane's head: a deletion the diff reported, unless the lane
			// forked before the file existed.
			continue
		}
		if (blob != c.Blob || typ != "blob" || mode == "120000") && !contains(changed, c.Path) {
			drift = append(drift, c.Path)
		}
	}
	if len(drift) > 0 {
		problems = append(problems, "approved check files differ from the approved copy at the lane's head. Either a rebase or merge carried an edit in, "+
			"or the lane is behind "+base+" and the files changed there: rebase the lane onto "+base+", and if they still differ raise a decision to change them:\n    "+
			strings.Join(firstLinesN(drift, 20), "\n    "))
	}
	return problems, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// CheckShip is ShipProblems over the Fold's store.
func (f *Fold) CheckShip(ctx context.Context, lane store.Lane) ([]string, error) {
	return ShipProblems(ctx, f.Store, lane)
}
