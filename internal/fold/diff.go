package fold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Diff modes: how much of a lane's changes a read returns.
const (
	// DiffStat: the commits and one line per changed file (the default).
	DiffStat = "stat"
	// DiffFiles: the stat plus each file's hunk headers, no bodies.
	DiffFiles = "files"
	// DiffFile: one file's patch, or an untracked file's start.
	DiffFile = "file"
)

// Limits on a diff read, enforced here so the CLI, the desk and the web get the same
// bounds. A read that hits one says so (LaneDiff.Notes, Truncated).
const (
	DiffMaxCommits   = 30
	DiffMaxFiles     = 200
	DiffMaxFilePatch = 64 << 10
	DiffMaxTotal     = 256 << 10
	// diffHunksPerFile bounds the hunk headers listed for one file in files mode.
	diffHunksPerFile = 20
)

// DiffRequest says what to read. Since and Until narrow the commits and the committed
// diff to a range of commits (a run's start_sha..end_sha); with Until set, the worktree's
// uncommitted and untracked files are left out.
type DiffRequest struct {
	Mode  string `json:"mode,omitempty"`
	File  string `json:"file,omitempty"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	// Run reads one run's own range, its start_sha..end_sha (a run still going or with no
	// recorded end reads from its start, with the worktree). It excludes Since and Until.
	Run int64 `json:"run,omitempty"`
}

// DiffCommit is one commit of a lane.
type DiffCommit struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Files   int       `json:"files"`
}

// DiffFileInfo is one changed file.
type DiffFileInfo struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	// Status: added, modified, deleted, renamed, copied, typechange or untracked.
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	// Binary files carry no line counts; Size is their bytes (and an untracked file's).
	Binary bool  `json:"binary,omitempty"`
	Size   int64 `json:"size,omitempty"`
	// InScope is whether the lane's scope covers the path.
	InScope bool `json:"in_scope"`
	// Hunks are the hunk headers (files mode).
	Hunks []string `json:"hunks,omitempty"`
}

// LaneDiff is what a lane holds against its base, at one moment. A comparison of two
// lanes can hold two of these.
type LaneDiff struct {
	LaneID    int64     `json:"lane_id"`
	Lane      string    `json:"lane"`
	Repo      string    `json:"repo,omitempty"`
	Branch    string    `json:"branch"`
	Base      string    `json:"base"`
	BaseRef   string    `json:"base_ref"`
	MergeBase string    `json:"merge_base,omitempty"`
	Head      string    `json:"head"`
	Mode      string    `json:"mode"`
	ReadAt    time.Time `json:"read_at"`
	// Gate is what Shepherd knows about its gate on Head, whatever the range read.
	Gate store.GateBadge `json:"gate"`

	// Commits are the newest DiffMaxCommits, oldest first; CommitsTotal is all of them.
	Commits      []DiffCommit `json:"commits"`
	CommitsTotal int          `json:"commits_total"`
	// Committed, Uncommitted and Untracked are the three groups of files. Together they
	// hold at most DiffMaxFiles; FilesMore counts those left out.
	Committed   []DiffFileInfo `json:"committed"`
	Uncommitted []DiffFileInfo `json:"uncommitted"`
	Untracked   []DiffFileInfo `json:"untracked"`
	FilesMore   int            `json:"files_more,omitempty"`
	// OutOfScope counts the listed files the scope does not cover.
	OutOfScope int `json:"out_of_scope"`

	// File and Patch answer a file-mode read: the patch (or an untracked file's start).
	File  string `json:"file,omitempty"`
	Patch string `json:"patch,omitempty"`

	// Truncated says a limit cut the answer; Notes name what and how to read more.
	Truncated bool     `json:"truncated,omitempty"`
	Notes     []string `json:"notes,omitempty"`
	// Redacted is how many spans were masked: the view is not the raw bytes.
	Redacted int `json:"redacted,omitempty"`
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// Diff reads what is in a lane against its base: commits, committed changes, uncommitted
// changes and untracked files, each file marked in or outside the lane's scope. It
// changes nothing. Every string it returns is redacted, before any size cut.
func (f *Fold) Diff(ctx context.Context, laneID int64, req DiffRequest) (res LaneDiff, err error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return LaneDiff{}, err
	}
	if lane.State != store.LaneOpen {
		return LaneDiff{}, refuse("lane %s is %s: there is no worktree to read", lane.Name, lane.State)
	}
	if st, err := os.Stat(lane.Worktree); err != nil || !st.IsDir() {
		return LaneDiff{}, refuse("lane %s has no worktree on disk at %s", lane.Name, lane.Worktree)
	}
	mode := req.Mode
	switch {
	case mode == "" && req.File != "":
		mode = DiffFile
	case mode == "":
		mode = DiffStat
	case mode != DiffStat && mode != DiffFiles && mode != DiffFile:
		return LaneDiff{}, refuse("unknown mode %q: stat, files or file", mode)
	}
	if mode == DiffFile && req.File == "" {
		return LaneDiff{}, refuse("file mode needs a path")
	}
	if req.Run != 0 {
		if req.Since != "" || req.Until != "" {
			return LaneDiff{}, refuse("pass a run or a since/until range, not both")
		}
		run, err := f.Store.Run(ctx, req.Run)
		if err != nil {
			return LaneDiff{}, err
		}
		if run.LaneID != lane.ID {
			return LaneDiff{}, refuse("run %d belongs to another lane, not %s", run.ID, lane.Name)
		}
		if run.StartSHA == "" {
			return LaneDiff{}, refuse("run %d recorded no start commit, so it has no range of its own", run.ID)
		}
		req.Since, req.Until = run.StartSHA, run.EndSHA
	}
	for _, s := range []string{req.Since, req.Until} {
		if s != "" && !shaRe.MatchString(s) {
			return LaneDiff{}, refuse("%q is not a commit SHA", s)
		}
	}
	if req.Until != "" && req.Since == "" {
		return LaneDiff{}, refuse("until needs since")
	}

	wt := lane.Worktree
	d := LaneDiff{LaneID: lane.ID, Lane: lane.Name, Branch: lane.Branch, Base: lane.Base,
		BaseRef: baseRef(ctx, lane), Mode: mode, ReadAt: time.Now().UTC(),
		Commits: []DiffCommit{}, Committed: []DiffFileInfo{}, Uncommitted: []DiffFileInfo{}, Untracked: []DiffFileInfo{}}
	if repo, err := f.Store.Repo(ctx, lane.RepoID); err == nil {
		d.Repo = repo.Name
	}
	if d.Head, err = git.Run(ctx, wt, "rev-parse", "HEAD"); err != nil {
		return d, err
	}
	d.Gate = f.GateBadge(ctx, lane.ID, d.Head)
	d.MergeBase, _ = git.Run(ctx, wt, "merge-base", d.BaseRef, "HEAD")

	// The committed side: the base to HEAD (or a run's own range).
	from, to := d.MergeBase, "HEAD"
	if from == "" {
		from = d.BaseRef
		d.Notes = append(d.Notes, "no common history with "+d.BaseRef+": compared with it directly")
	}
	if req.Since != "" {
		from = req.Since
		if req.Until != "" {
			to = req.Until
		}
	}
	worktree := req.Until == ""

	var masked int
	clean := func(s string) string {
		out := redact.String(s)
		if out != s {
			masked += strings.Count(out, redact.Mask) - strings.Count(s, redact.Mask)
		}
		return out
	}
	defer func() {
		res.Redacted = masked
		if masked > 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("%d span(s) masked as credentials: this view is not the raw bytes", masked))
		}
	}()

	if mode == DiffFile {
		return d, f.diffFile(ctx, lane, &d, req, from, to, worktree, clean)
	}

	commits, total, err := git.LogRange(ctx, wt, from+".."+to, DiffMaxCommits)
	if err != nil {
		return d, err
	}
	d.CommitsTotal = total
	for _, c := range commits {
		d.Commits = append(d.Commits, DiffCommit{SHA: c.SHA, Subject: clean(c.Subject),
			Author: clean(c.Author), Date: c.Date, Files: c.Files})
	}
	if total > len(commits) {
		d.Truncated = true
		d.Notes = append(d.Notes, fmt.Sprintf("%d older commit(s) not listed: only the newest %d are shown (git log %s..%s)",
			total-len(commits), DiffMaxCommits, from, to))
	}

	budget := DiffMaxFiles
	var hunkBytes int
	addGroup := func(group *[]DiffFileInfo, stats []git.FileStat, hunks map[string][]string) {
		for _, s := range stats {
			if budget == 0 {
				d.FilesMore++
				continue
			}
			budget--
			fi := fileInfo(lane, s)
			if hunks != nil {
				for _, h := range hunks[s.Path] {
					if hunkBytes+len(h) > DiffMaxTotal {
						d.Truncated = true
						break
					}
					hunkBytes += len(h)
					fi.Hunks = append(fi.Hunks, clean(h))
				}
			}
			if s.Binary {
				fi.Size = fileSize(ctx, wt, s.Path, to)
			}
			fi.Path, fi.OldPath = clean(fi.Path), clean(fi.OldPath)
			if !fi.InScope {
				d.OutOfScope++
			}
			*group = append(*group, fi)
		}
	}
	var chunks, wchunks map[string][]string
	if mode == DiffFiles {
		chunks, _ = git.DiffHunks(ctx, wt, diffHunksPerFile, from+".."+to)
		if worktree {
			wchunks, _ = git.DiffHunks(ctx, wt, diffHunksPerFile, "HEAD")
		}
	}
	committed, err := git.DiffStat(ctx, wt, from+".."+to)
	if err != nil {
		return d, err
	}
	addGroup(&d.Committed, committed, chunks)
	if worktree {
		dirty, err := git.DiffStat(ctx, wt, "HEAD")
		if err != nil {
			return d, err
		}
		addGroup(&d.Uncommitted, dirty, wchunks)
		files, more, err := git.UntrackedFiles(ctx, wt, DiffMaxFiles+1)
		if err != nil {
			return d, err
		}
		if more || len(files) > DiffMaxFiles {
			d.Truncated = true
		}
		for _, p := range files {
			if budget == 0 {
				d.FilesMore++
				continue
			}
			budget--
			fi := DiffFileInfo{Path: clean(p), Status: "untracked", InScope: scope.Any(lane.Scope, p)}
			if st, err := os.Lstat(filepath.Join(wt, p)); err == nil {
				fi.Size = st.Size()
			}
			if !fi.InScope {
				d.OutOfScope++
			}
			d.Untracked = append(d.Untracked, fi)
		}
	}
	if d.FilesMore > 0 {
		d.Truncated = true
		d.Notes = append(d.Notes, fmt.Sprintf("%d more file(s) not listed: the read stops at %d files; read one with --file", d.FilesMore, DiffMaxFiles))
	}
	if d.Truncated && hunkBytes >= DiffMaxTotal-256 {
		d.Notes = append(d.Notes, fmt.Sprintf("hunk headers stop at %d KiB in all; read one file with --file", DiffMaxTotal>>10))
	}
	return d, nil
}

// diffFile fills d for file mode: the file's patch from the diff's start to the worktree
// (committed and uncommitted together, as the lane would ship it), or an untracked
// file's start.
func (f *Fold) diffFile(ctx context.Context, lane store.Lane, d *LaneDiff, req DiffRequest, from, to string, worktree bool, clean func(string) string) error {
	wt := lane.Worktree
	rel := filepath.ToSlash(filepath.Clean(req.File))
	if filepath.IsAbs(req.File) || rel == ".." || strings.HasPrefix(rel, "../") || rel == "." {
		return refuse("%q is not a path inside the lane's worktree", req.File)
	}
	d.File = clean(rel)
	inScope := scope.Any(lane.Scope, rel)
	if !inScope {
		d.OutOfScope = 1
		d.Notes = append(d.Notes, rel+" is outside the lane's scope")
	}

	untracked, _, err := git.UntrackedFiles(ctx, wt, 1, ":(literal)"+rel)
	if err != nil {
		return err
	}
	if worktree && len(untracked) == 1 && untracked[0] == rel {
		return untrackedStart(wt, rel, d, clean)
	}

	// Read up to twice the cap, redact, then cut: a cut never splits a secret.
	args := []string{from}
	if !worktree {
		args = append(args, to)
	}
	args = append(args, "--", rel)
	patch, cut, err := git.Patch(ctx, wt, 2*DiffMaxFilePatch, args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(patch) == "" {
		d.Notes = append(d.Notes, rel+" has no changes against "+from)
		return nil
	}
	patch = clean(patch)
	if len(patch) > DiffMaxFilePatch {
		patch = cutPatch(patch, DiffMaxFilePatch)
		cut = true
	}
	d.Patch = patch
	if cut {
		d.Truncated = true
		d.Notes = append(d.Notes, fmt.Sprintf("the patch is cut at %d KiB on a hunk boundary: read the rest in the worktree (git diff %s -- %s)",
			DiffMaxFilePatch>>10, shortSHA(from), rel))
	}
	return nil
}

// untrackedStart reads the first bytes of an untracked file, never following a symlink
// out of the worktree.
func untrackedStart(wt, rel string, d *LaneDiff, clean func(string) string) error {
	p := filepath.Join(wt, filepath.FromSlash(rel))
	st, err := os.Lstat(p)
	if err != nil {
		return refuse("%s: %v", rel, err)
	}
	if !st.Mode().IsRegular() {
		d.Notes = append(d.Notes, fmt.Sprintf("%s is untracked and not a regular file (%s): not read", rel, st.Mode().Type()))
		return nil
	}
	fh, err := os.Open(p)
	if err != nil {
		return err
	}
	defer fh.Close()
	buf := make([]byte, DiffMaxFilePatch)
	n, _ := fh.Read(buf)
	buf = buf[:n]
	if n > 0 && (strings.IndexByte(string(buf[:min(n, 8000)]), 0) >= 0 || !utf8.Valid(trimPartialRune(buf))) {
		d.Notes = append(d.Notes, fmt.Sprintf("%s is untracked: binary, %d bytes", rel, st.Size()))
		return nil
	}
	d.Patch = clean(string(trimPartialRune(buf)))
	d.Notes = append(d.Notes, fmt.Sprintf("%s is untracked: its start, as it is in the worktree, not a diff", rel))
	if st.Size() > int64(n) {
		d.Truncated = true
		d.Notes = append(d.Notes, fmt.Sprintf("only the first %d KiB of %d bytes are shown", DiffMaxFilePatch>>10, st.Size()))
	}
	return nil
}

// trimPartialRune drops an incomplete UTF-8 sequence a byte cut left at the end.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax-1 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return b
}

// cutPatch cuts a patch at most max bytes long on a hunk boundary when one is in range,
// else on a line.
func cutPatch(p string, max int) string {
	head := p[:max]
	if i := strings.LastIndex(head, "\n@@"); i > 0 {
		return head[:i+1]
	}
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		return head[:i+1]
	}
	return head
}

var diffStatus = map[string]string{"A": "added", "M": "modified", "D": "deleted", "R": "renamed", "C": "copied", "T": "typechange"}

func fileInfo(lane store.Lane, s git.FileStat) DiffFileInfo {
	st := diffStatus[s.Status]
	if st == "" {
		st = "modified"
	}
	return DiffFileInfo{Path: s.Path, OldPath: s.OldPath, Status: st, Added: s.Added, Removed: s.Removed,
		Binary: s.Binary, InScope: scope.Any(lane.Scope, s.Path)}
}

// fileSize is a binary file's bytes: the worktree's copy, else its size at rev.
func fileSize(ctx context.Context, wt, rel, rev string) int64 {
	if st, err := os.Lstat(filepath.Join(wt, filepath.FromSlash(rel))); err == nil {
		return st.Size()
	}
	if out, err := git.Run(ctx, wt, "cat-file", "-s", rev+":"+rel); err == nil {
		var n int64
		fmt.Sscan(out, &n)
		return n
	}
	return 0
}

// baseRef is what a lane's changes are counted from: its base on origin if there.
func baseRef(ctx context.Context, lane store.Lane) string {
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Base) {
		return "origin/" + lane.Base
	}
	return lane.Base
}

// GateBadge is the lane's gate badge for the commit head: passed or failed when Shepherd
// ran the gate on exactly that commit, stale when its newest record is for another, and
// not_run for none. A store error reads as not_run: the badge never claims a pass.
func (f *Fold) GateBadge(ctx context.Context, laneID int64, head string) store.GateBadge {
	var onHead *store.GateResult
	if head != "" {
		onHead, _ = f.Store.GateResultFor(ctx, laneID, head)
	}
	last := onHead
	if last == nil {
		last, _ = f.Store.LastGateResult(ctx, laneID)
	}
	return store.BadgeFor(head, onHead, last)
}

// BranchHeads returns the commit each lane's branch is at, for the lanes whose branch
// exists in the repo (a lane's worktree is on its branch), from one read of the refs.
func BranchHeads(ctx context.Context, repoPath string, lanes []store.Lane) map[int64]string {
	out := map[int64]string{}
	refs, err := git.Run(ctx, repoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	if err != nil {
		return out
	}
	sha := map[string]string{}
	for _, line := range strings.Split(refs, "\n") {
		if name, id, ok := strings.Cut(line, " "); ok {
			sha[name] = id
		}
	}
	for _, l := range lanes {
		if id, ok := sha["refs/heads/"+l.Branch]; ok {
			out[l.ID] = id
		}
	}
	return out
}
