package fold

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Retention defaults: snapshots of a closed lane are kept
// DefaultKeepDays after close, pinned ones for ever, under DefaultSnapshotCap in total.
const (
	DefaultKeepDays    = 30
	DefaultSnapshotCap = 500 << 20
)

// ExcludeLine is what Shepherd adds to a repo's info/exclude.
const ExcludeLine = "/" + store.ArtifactDir

// ExcludeSetting is the store setting that, set to "off" for a repo, refuses the
// info/exclude line: the person's choice, kept per repo name.
func ExcludeSetting(repo string) string { return "artifacts.exclude." + repo }

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ensureExclude adds the Shepherd directory to the repo's info/exclude once, unless the
// person refused it. The note says what it did; announce also puts it in the feed.
func (f *Fold) ensureExclude(ctx context.Context, repo store.Repo, announce bool) string {
	if v, err := f.Store.Setting(ctx, ExcludeSetting(repo.Name)); err == nil && v == "off" {
		return ""
	}
	added, file, err := git.EnsureExclude(ctx, repo.Path, ExcludeLine)
	switch {
	case err != nil:
		return "could not add " + ExcludeLine + " to the repo's info/exclude: " + err.Error()
	case !added:
		return ""
	}
	note := fmt.Sprintf("added %s to %s, so files agents keep there stay out of git; refuse with the setting %s=off", ExcludeLine, file, ExcludeSetting(repo.Name))
	if announce {
		f.Store.AddFeed(ctx, store.FeedArtifact, note, 0)
	}
	return note
}

// EnsureExclude adds the line for a repo by id and says so in the feed: what adding a
// repo does. A lane's open does the same silently, for a repo added before.
func (f *Fold) EnsureExclude(ctx context.Context, repoID int64) (string, error) {
	repo, err := f.Store.Repo(ctx, repoID)
	if err != nil {
		return "", err
	}
	return f.ensureExclude(ctx, repo, true), nil
}

// readLive reads the regular file at rel in a worktree, refusing a symlink anywhere on
// the way, and returns its bytes and raw sha-256. Unlike ArtifactContent it does not
// need the path to match the repo's globs: a declared file may not.
func readLive(worktree, rel string, limit int64) (data []byte, sum string, err error) {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return nil, "", err
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if real, err := filepath.EvalSymlinks(p); err != nil || real != p {
		return nil, "", refuse("%s is not a plain file inside the worktree", rel)
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return nil, "", refuse("%s is not a regular file in the worktree", rel)
	}
	if st.Size() > limit {
		return nil, "", refuse("%s is %d bytes, over the %d byte cap", rel, st.Size(), limit)
	}
	data, err = os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:]), nil
}

func (f *Fold) snapshotFile(id int64, sha string) string {
	return filepath.Join(f.ArtifactDir, strconv.FormatInt(id, 10), sha)
}

// wantsSnapshot says whether an artifact's bytes are to be stored: not scratch that
// nobody pinned, and an image only when declared or pinned (it cannot be redacted).
func wantsSnapshot(a store.Artifact) bool {
	if a.Intent == store.ArtifactScratch && !a.Pinned {
		return false
	}
	if a.Kind == store.ArtifactImage && !a.Pinned && a.Source != store.ArtifactDeclared {
		return false
	}
	return true
}

// SnapshotLane records what an open lane's artifacts hold now: it finds files the run
// ends have not recorded yet (the Shepherd directory and those matching the repo's
// globs), notes new content as a version, and stores a redacted, content-addressed copy
// of each artifact worth keeping under ArtifactDir. It is what runs before a close, and
// it changes no file of the worktree. A lane with no worktree on disk is left alone.
func (f *Fold) SnapshotLane(ctx context.Context, laneID int64) error {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return err
	}
	if lane.Worktree == "" || f.ArtifactDir == "" {
		return nil
	}
	if _, err := os.Stat(lane.Worktree); err != nil {
		return nil
	}
	repo, err := f.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return err
	}
	f.recordUnseen(ctx, lane, repo)
	list, err := f.Store.ListArtifacts(ctx, store.ArtifactFilter{LaneID: lane.ID, State: store.ArtifactLive, Limit: 1000})
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range list {
		if err := f.snapshotArtifact(ctx, a, lane.Worktree); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// recordUnseen stores artifacts a close finds that no run's end recorded: a file the
// person wrote by hand, or one a run had not ended to report. They belong to no run.
func (f *Fold) recordUnseen(ctx context.Context, lane store.Lane, repo store.Repo) {
	cands := map[string]string{}
	if la, err := f.Artifacts(ctx, lane.ID); err == nil {
		for _, a := range la.Artifacts {
			if !a.Readable {
				continue
			}
			src := store.ArtifactDiscovered
			if a.Source == "committed" {
				src = store.ArtifactCommitted
			}
			cands[a.Path] = src
		}
	}
	root := filepath.Join(lane.Worktree, filepath.FromSlash(strings.TrimSuffix(store.ArtifactDir, "/")))
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if rel, err := filepath.Rel(lane.Worktree, p); err == nil {
				cands[filepath.ToSlash(rel)] = store.ArtifactDiscovered
			}
		}
		return nil
	})
	paths := make([]string, 0, len(cands))
	for p := range cands {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if _, err := f.Store.LaneArtifact(ctx, lane.ID, p); err == nil {
			continue
		}
		_, sum, err := readLive(lane.Worktree, p, 64<<20)
		if err != nil {
			continue
		}
		intent := store.ArtifactUndecided
		if strings.HasPrefix(p, store.ArtifactDir) {
			intent = store.ArtifactKeep
		}
		st, _ := os.Stat(filepath.Join(lane.Worktree, filepath.FromSlash(p)))
		a, created, err := f.Store.UpsertArtifact(ctx, store.Artifact{
			WorkspaceID: repo.WorkspaceID, RepoID: repo.ID, LaneID: lane.ID, Path: p, Kind: store.ArtifactKindOf(p),
			Source: cands[p], Intent: intent, IntentBy: store.IntentByDefault, SHA256: sum, Size: st.Size(),
		})
		if err != nil {
			continue
		}
		f.Store.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: sum, Size: st.Size()})
		if created {
			f.Store.AddFeed(ctx, store.FeedArtifact, fmt.Sprintf("%s found in lane %s at its close, outside a run", p, lane.Name), a.ID)
		}
	}
}

// snapshotArtifact notes the file's current content as a version (run 0 when no run's
// end had seen it) and stores a copy of it when the artifact is worth one.
func (f *Fold) snapshotArtifact(ctx context.Context, a store.Artifact, worktree string) error {
	limit := int64(ArtifactMaxText)
	if a.Kind == store.ArtifactImage {
		limit = ArtifactMaxImage
	}
	data, sum, err := readLive(worktree, a.Path, 64<<20)
	if err != nil {
		return nil // gone or unreadable: nothing to take
	}
	if sum != a.SHA256 {
		f.Store.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: sum, Size: int64(len(data))})
	}
	if !wantsSnapshot(a) || int64(len(data)) > limit {
		return nil
	}
	dst := f.snapshotFile(a.ID, sum)
	if _, err := os.Stat(dst); err == nil {
		return f.Store.MarkVersionSnapshotted(ctx, a.ID, sum)
	}
	if a.Kind != store.ArtifactImage {
		if !utf8.Valid(data) {
			return nil
		}
		data = []byte(redact.String(string(data)))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".snap-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return f.Store.MarkVersionSnapshotted(ctx, a.ID, sum)
}

// SnapshotKnown is what a run's end and a declaration call: the lane's artifacts, no
// discovery of new ones (the runner records those with the run's attribution). It never
// fails the caller.
func (f *Fold) SnapshotKnown(ctx context.Context, laneID int64) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil || lane.Worktree == "" || f.ArtifactDir == "" {
		return
	}
	list, err := f.Store.ListArtifacts(ctx, store.ArtifactFilter{LaneID: laneID, State: store.ArtifactLive, Limit: 1000})
	if err != nil {
		return
	}
	for _, a := range list {
		f.snapshotArtifact(ctx, a, lane.Worktree)
	}
}

// closeDirt splits an open lane's uncommitted files into those that need not stop a
// close (an untracked artifact the person or agent said to keep, or that is scratch, and
// anything in the Shepherd directory) and the rest. It lists every file, never a folder.
func (f *Fold) closeDirt(ctx context.Context, lane store.Lane) (ignorable, blocking []string, err error) {
	out, err := git.Run(ctx, lane.Worktree, "status", "--porcelain", "-uall")
	if err != nil {
		return nil, nil, err
	}
	for p, code := range git.DirtyPaths(out) {
		if code == "??" && strings.HasPrefix(p, store.ArtifactDir) {
			ignorable = append(ignorable, p)
			continue
		}
		if a, err := f.Store.LaneArtifact(ctx, lane.ID, p); err == nil && code == "??" &&
			(a.Intent == store.ArtifactKeep || a.Intent == store.ArtifactScratch) {
			ignorable = append(ignorable, p)
			continue
		}
		blocking = append(blocking, p)
	}
	sort.Strings(ignorable)
	sort.Strings(blocking)
	return ignorable, blocking, nil
}

// artifactPlan is what a close does to the lane's artifact rows once the lane is gone.
type artifactPlan map[int64]string

// planArtifacts decides each live artifact's state at close, while the branch and the
// worktree still exist: committed when the branch holds the file, kept when a snapshot
// of its current content is stored, gone otherwise.
func (f *Fold) planArtifacts(ctx context.Context, lane store.Lane, repoPath string) artifactPlan {
	plan := artifactPlan{}
	list, err := f.Store.ListArtifacts(ctx, store.ArtifactFilter{LaneID: lane.ID, State: store.ArtifactLive, Limit: 1000})
	if err != nil {
		return plan
	}
	branchOK := git.RefExists(ctx, repoPath, "refs/heads/"+lane.Branch)
	for _, a := range list {
		switch {
		case a.Intent == store.ArtifactScratch && !a.Pinned && !(branchOK && git.Ok(ctx, repoPath, "cat-file", "-e", "refs/heads/"+lane.Branch+":"+a.Path)):
			// Scratch nobody pinned: whatever copy an earlier look took goes with it.
			plan[a.ID] = store.ArtifactGone
			if f.ArtifactDir != "" {
				os.RemoveAll(filepath.Join(f.ArtifactDir, strconv.FormatInt(a.ID, 10)))
			}
		case branchOK && git.Ok(ctx, repoPath, "cat-file", "-e", "refs/heads/"+lane.Branch+":"+a.Path):
			plan[a.ID] = store.ArtifactInRepo
		case f.ArtifactDir != "" && a.SHA256 != "" && fileExists(f.snapshotFile(a.ID, a.SHA256)):
			plan[a.ID] = store.ArtifactKept
		default:
			plan[a.ID] = store.ArtifactGone
		}
	}
	return plan
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// settleArtifacts applies a close's plan. A scratch file nobody pinned has no snapshot
// to drop; it is simply gone.
func (f *Fold) settleArtifacts(ctx context.Context, plan artifactPlan) {
	for id, state := range plan {
		f.Store.SetArtifactState(ctx, id, state, true)
	}
}

// blockingWords says what a refused close names: the blocking files, those that are
// artifacts first, with what to do about them.
func (f *Fold) blockingWords(ctx context.Context, lane store.Lane, blocking []string) string {
	var arts, other []string
	for _, p := range blocking {
		if a, err := f.Store.LaneArtifact(ctx, lane.ID, p); err == nil {
			arts = append(arts, fmt.Sprintf("%s (artifact %d, %s)", p, a.ID, a.Intent))
		} else {
			other = append(other, p)
		}
	}
	var b strings.Builder
	if len(arts) > 0 {
		fmt.Fprintf(&b, "%d file(s) to keep or discard: %s", len(arts), strings.Join(firstN2(arts, 10), ", "))
		b.WriteString("; mark each keep or discard in the web's Artifacts tab, or ask the lane's agent to commit it")
	}
	if len(other) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("uncommitted: " + strings.Join(firstN2(other, 10), ", "))
	}
	return b.String()
}

func firstN2(s []string, n int) []string {
	if len(s) > n {
		return append(append([]string{}, s[:n]...), fmt.Sprintf("and %d more", len(s)-n))
	}
	return s
}

// removeWorktree removes a worktree, forced when the only things in it are files a
// close has just snapshotted and the person said to let go.
func removeWorktree(ctx context.Context, repoPath, wt string, force bool) error {
	args := []string{"worktree", "remove", wt}
	if force {
		args = []string{"worktree", "remove", "--force", wt}
	}
	_, err := git.Run(ctx, repoPath, args...)
	return err
}

// Artifact edits: the person's click in the web, through the daemon.

// ArtifactPatch is a change to an artifact; nil fields are left alone. Move "keep" moves
// an untracked file into the Shepherd directory.
type ArtifactPatch struct {
	Intent *string
	Pinned *bool
	Title  *string
	Move   string
}

// PatchArtifact applies the person's edit. A change of intent is the person's, so it
// outranks the agent's and a later declaration cannot move it.
func (f *Fold) PatchArtifact(ctx context.Context, id int64, p ArtifactPatch) (store.Artifact, error) {
	a, err := f.Store.Artifact(ctx, id)
	if err != nil {
		return a, err
	}
	if p.Intent != nil {
		switch *p.Intent {
		case store.ArtifactCommit, store.ArtifactKeep, store.ArtifactScratch, store.ArtifactUndecided:
		default:
			return a, refuse("intent %q: want commit, keep, scratch or undecided", *p.Intent)
		}
		if err := f.Store.SetArtifactIntent(ctx, id, *p.Intent, store.IntentByPerson); err != nil {
			return a, err
		}
	}
	if p.Pinned != nil {
		if err := f.Store.SetArtifactPinned(ctx, id, *p.Pinned); err != nil {
			return a, err
		}
	}
	if p.Title != nil {
		if err := f.Store.SetArtifactTitle(ctx, id, redact.String(strings.TrimSpace(*p.Title))); err != nil {
			return a, err
		}
	}
	switch p.Move {
	case "":
	case "keep":
		if err := f.moveToKeep(ctx, a); err != nil {
			return a, err
		}
	default:
		return a, refuse("move %q: want keep", p.Move)
	}
	if p.Pinned != nil && *p.Pinned {
		// A pin asks for a copy now, even of scratch or an image.
		if cur, err := f.Store.Artifact(ctx, id); err == nil && cur.State == store.ArtifactLive {
			if lane, err := f.Store.Lane(ctx, cur.LaneID); err == nil {
				f.snapshotArtifact(ctx, cur, lane.Worktree)
			}
		}
	}
	return f.Store.Artifact(ctx, id)
}

// moveToKeep moves an untracked file of an open lane into the Shepherd directory, inside
// the same worktree, so it stops counting as uncommitted work.
func (f *Fold) moveToKeep(ctx context.Context, a store.Artifact) error {
	lane, err := f.Store.Lane(ctx, a.LaneID)
	if err != nil {
		return err
	}
	if lane.State != store.LaneOpen || a.State != store.ArtifactLive {
		return refuse("%s is not in an open lane's worktree", a.Path)
	}
	if strings.HasPrefix(a.Path, store.ArtifactDir) {
		return refuse("%s is already in %s", a.Path, store.ArtifactDir)
	}
	if git.Ok(ctx, lane.Worktree, "ls-files", "--error-unmatch", "--", a.Path) {
		return refuse("%s is tracked by git: moving it would delete it from the branch; keep it by committing it", a.Path)
	}
	if _, _, err := readLive(lane.Worktree, a.Path, 64<<20); err != nil {
		return err
	}
	rel := path.Join(strings.TrimSuffix(store.ArtifactDir, "/"), a.Path)
	dst := filepath.Join(lane.Worktree, filepath.FromSlash(rel))
	if _, err := os.Lstat(dst); err == nil {
		return refuse("%s already exists", rel)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(lane.Worktree, filepath.FromSlash(a.Path)), dst); err != nil {
		return err
	}
	if err := f.Store.RenameArtifact(ctx, a.ID, rel); err != nil {
		os.Rename(dst, filepath.Join(lane.Worktree, filepath.FromSlash(a.Path)))
		return err
	}
	f.Store.SetArtifactIntent(ctx, a.ID, store.ArtifactKeep, store.IntentByPerson)
	f.Store.AddFeed(ctx, store.FeedArtifact, fmt.Sprintf("%s moved to %s in lane %s, on the person's word", a.Path, rel, lane.Name), a.ID)
	return nil
}

// DiscardArtifact deletes an artifact's file from the worktree and its snapshots, on the
// person's word. A file git tracks is refused: it is in the lane's history, or its
// change is work to commit or revert, and that is not this action.
func (f *Fold) DiscardArtifact(ctx context.Context, id int64) (store.Artifact, error) {
	a, err := f.Store.Artifact(ctx, id)
	if err != nil {
		return a, err
	}
	if a.State == store.ArtifactDiscarded {
		return a, refuse("%s is already discarded", a.Path)
	}
	lane, err := f.Store.Lane(ctx, a.LaneID)
	if err != nil {
		return a, err
	}
	if lane.State == store.LaneOpen && a.State == store.ArtifactLive && lane.Worktree != "" {
		if _, err := os.Stat(lane.Worktree); err == nil {
			if git.Ok(ctx, lane.Worktree, "ls-files", "--error-unmatch", "--", a.Path) {
				return a, refuse("%s is tracked by git: discard it with git, not here", a.Path)
			}
			if _, _, err := readLive(lane.Worktree, a.Path, 1<<62); err == nil {
				if err := os.Remove(filepath.Join(lane.Worktree, filepath.FromSlash(a.Path))); err != nil {
					return a, err
				}
			}
		}
	}
	if f.ArtifactDir != "" {
		os.RemoveAll(filepath.Join(f.ArtifactDir, strconv.FormatInt(id, 10)))
	}
	if err := f.Store.SetArtifactState(ctx, id, store.ArtifactDiscarded, true); err != nil {
		return a, err
	}
	f.Store.AddFeed(ctx, store.FeedArtifact, fmt.Sprintf("%s discarded from lane %s, on the person's word", a.Path, lane.Name), id)
	return f.Store.Artifact(ctx, id)
}

// Snapshot content.

// ArtifactSnapshot is the stored copy of an artifact's content: a version's (sha) or, for
// "", the newest one with a copy. It is already redacted.
func (f *Fold) ArtifactSnapshot(ctx context.Context, id int64, sha string) (ArtifactContent, error) {
	a, err := f.Store.Artifact(ctx, id)
	if err != nil {
		return ArtifactContent{}, err
	}
	if f.ArtifactDir == "" {
		return ArtifactContent{}, refuse("this daemon keeps no artifact snapshots")
	}
	var found *store.ArtifactVersion
	for i, v := range a.Versions { // newest first
		if sha != "" && v.SHA256 != sha {
			continue
		}
		if fileExists(f.snapshotFile(a.ID, v.SHA256)) {
			found = &a.Versions[i]
			break
		}
	}
	if found == nil || !sha256Hex.MatchString(found.SHA256) {
		return ArtifactContent{}, fmt.Errorf("%w: no stored copy of %s", store.ErrNotFound, a.Path)
	}
	data, err := os.ReadFile(f.snapshotFile(a.ID, found.SHA256))
	if err != nil {
		return ArtifactContent{}, err
	}
	return ArtifactContent{Path: a.Path, Mime: artifactMime(artifactKind(a.Path), a.Path), Data: data}, nil
}

// Retention.

// GCResult says what a retention pass did.
type GCResult struct {
	Expired   int   `json:"expired"`
	Evicted   int   `json:"evicted"`
	Orphans   int   `json:"orphans"`
	FreedByte int64 `json:"freed_bytes"`
	KeptByte  int64 `json:"kept_bytes"`
}

// ArtifactGC applies retention: the snapshots of an artifact whose lane closed more than
// keepDays ago go unless pinned; then, over capBytes in total, the oldest unpinned
// snapshots of closed lanes go first. Directories no artifact owns are removed. dry only
// counts. A keepDays or capBytes of 0 takes the default.
func (f *Fold) ArtifactGC(ctx context.Context, keepDays int, capBytes int64, dry bool, now time.Time) (GCResult, error) {
	var res GCResult
	if f.ArtifactDir == "" {
		return res, nil
	}
	if keepDays <= 0 {
		keepDays = DefaultKeepDays
	}
	if capBytes <= 0 {
		capBytes = DefaultSnapshotCap
	}
	entries, err := os.ReadDir(f.ArtifactDir)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	type snap struct {
		id     int64
		size   int64
		closed time.Time
		live   bool
		pinned bool
	}
	var snaps []snap
	drop := func(s snap) {
		if !dry {
			os.RemoveAll(filepath.Join(f.ArtifactDir, strconv.FormatInt(s.id, 10)))
		}
	}
	cutoff := now.AddDate(0, 0, -keepDays)
	for _, e := range entries {
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.IsDir() {
			continue
		}
		s := snap{id: id, size: dirSize(filepath.Join(f.ArtifactDir, e.Name()))}
		a, err := f.Store.Artifact(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			res.Orphans++
			res.FreedByte += s.size
			drop(s)
			continue
		case err != nil:
			return res, err
		}
		s.live, s.pinned = a.State == store.ArtifactLive, a.Pinned
		if a.Closed != nil {
			s.closed = *a.Closed
		}
		if !s.live && !s.pinned && !s.closed.IsZero() && s.closed.Before(cutoff) {
			res.Expired++
			res.FreedByte += s.size
			drop(s)
			if !dry {
				f.Store.SetArtifactState(ctx, id, store.ArtifactGone, true)
			}
			continue
		}
		snaps = append(snaps, s)
		res.KeptByte += s.size
	}
	if res.KeptByte > capBytes {
		sort.Slice(snaps, func(i, j int) bool { return snaps[i].closed.Before(snaps[j].closed) })
		for _, s := range snaps {
			if res.KeptByte <= capBytes {
				break
			}
			if s.live || s.pinned {
				continue
			}
			res.Evicted++
			res.FreedByte += s.size
			res.KeptByte -= s.size
			drop(s)
			if !dry {
				f.Store.SetArtifactState(ctx, s.id, store.ArtifactGone, true)
			}
		}
	}
	return res, nil
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
