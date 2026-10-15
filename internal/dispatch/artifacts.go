package dispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// maxArtifactBytes is the largest file Shepherd will hash as an artifact version.
const maxArtifactBytes = 64 << 20

// maxScanArtifacts bounds how many files one run end records, so a repo full of
// markdown cannot flood the store.
const maxScanArtifacts = 200

// ArtifactScan lists a lane's files that match the repo's artifact globs; the daemon
// sets it to Fold.Artifacts. Nil means the run end records only what agents declared
// and what sits under the Shepherd directory.
type ArtifactScan func(ctx context.Context, laneID int64) (fold.LaneArtifacts, error)

// artifactPath turns what an agent wrote into a clean worktree-relative path with
// slashes, or says why it cannot be one. An absolute path inside the worktree is made
// relative.
func artifactPath(worktree, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", refuse("an artifact needs a path")
	}
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(worktree, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", refuse("%s is outside this lane's worktree", p)
		}
		p = rel
	}
	p = path.Clean(filepath.ToSlash(p))
	switch {
	case p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || filepath.VolumeName(p) != "":
		return "", refuse("%s is not a path inside the worktree", p)
	case p == ".git" || strings.HasPrefix(p, ".git/"):
		return "", refuse("%s is git's own", p)
	}
	return p, nil
}

// hashArtifact hashes the regular file at rel in worktree. It refuses a symlink (the
// file itself or any directory on the way), a directory and a file over the cap.
func hashArtifact(worktree, rel string) (sum string, size int64, err error) {
	full := worktree
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		full = filepath.Join(full, part)
		st, err := os.Lstat(full)
		switch {
		case os.IsNotExist(err):
			return "", 0, refuse("%s does not exist in this lane's worktree", rel)
		case err != nil:
			return "", 0, err
		case st.Mode()&os.ModeSymlink != 0:
			return "", 0, refuse("%s goes through a symlink", rel)
		case i == len(parts)-1 && !st.Mode().IsRegular():
			return "", 0, refuse("%s is not a regular file", rel)
		case i == len(parts)-1 && st.Size() > maxArtifactBytes:
			return "", 0, refuse("%s is %d MiB; the limit is %d", rel, st.Size()>>20, maxArtifactBytes>>20)
		}
	}
	f, err := os.Open(full)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxArtifactBytes+1))
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// intentWords describes an intent for the thread.
func intentWords(intent string) string {
	switch intent {
	case store.ArtifactKeep:
		return " (kept local)"
	case store.ArtifactCommit:
		return " (to commit)"
	case store.ArtifactScratch:
		return " (scratch)"
	}
	return ""
}

// artifactSeen is what recording one file did.
type artifactSeen struct {
	Artifact store.Artifact
	New      bool
	Version  bool
}

// recordArtifact stores the file at rel as an artifact of the run's lane and, when its
// content is new, a version attributed to the run. It writes the feed line: one per
// new artifact and one per new version, so a rescan of unchanged files is silent.
func (r *Runner) recordArtifact(ctx context.Context, run store.Run, lane store.Lane, rel, source, title, intent, by string) (artifactSeen, error) {
	sum, size, err := hashArtifact(lane.Worktree, rel)
	if err != nil {
		return artifactSeen{}, err
	}
	if intent == "" {
		intent, by = store.ArtifactUndecided, store.IntentByDefault
		if strings.HasPrefix(rel, store.ArtifactDir) {
			intent = store.ArtifactKeep
		}
	}
	var workspace int64
	if repo, err := r.Store.Repo(ctx, lane.RepoID); err == nil {
		workspace = repo.WorkspaceID
	}
	a, created, err := r.Store.UpsertArtifact(ctx, store.Artifact{
		WorkspaceID: workspace, RepoID: lane.RepoID, LaneID: lane.ID, RunID: run.ID,
		Path: rel, Title: redact.String(clip(title, 200)), Kind: store.ArtifactKindOf(rel),
		Source: source, Intent: intent, IntentBy: by, SHA256: sum, Size: size,
	})
	if err != nil {
		return artifactSeen{}, err
	}
	added, err := r.Store.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: sum, Size: size, RunID: run.ID})
	if err != nil {
		return artifactSeen{}, err
	}
	switch {
	case created:
		r.feed(ctx, store.FeedArtifact, a.ID, "run %d produced %s%s", run.ID, rel, intentWords(a.Intent))
	case added:
		r.feed(ctx, store.FeedArtifact, a.ID, "run %d changed %s", run.ID, rel)
	}
	return artifactSeen{Artifact: a, New: created, Version: added}, nil
}

// DeclareArtifact records a file an agent says the person may want to read. The run
// must be the declaring run's own (the daemon checks the token); the file must exist
// in the run's worktree as a regular file.
func (r *Runner) DeclareArtifact(ctx context.Context, runID int64, p, title, intent string) (store.Artifact, bool, bool, error) {
	if intent != "" && !contains(store.ArtifactIntents, intent) {
		return store.Artifact{}, false, false, refuse("intent %q: want %s", intent, strings.Join(store.ArtifactIntents, ", "))
	}
	run, err := r.Store.Run(ctx, runID)
	if err != nil {
		return store.Artifact{}, false, false, err
	}
	lane, err := r.Store.Lane(ctx, run.LaneID)
	if err != nil {
		return store.Artifact{}, false, false, err
	}
	rel, err := artifactPath(lane.Worktree, p)
	if err != nil {
		return store.Artifact{}, false, false, err
	}
	by := store.IntentByAgent
	if intent == "" {
		by = ""
	}
	s, err := r.recordArtifact(ctx, run, lane, rel, store.ArtifactDeclared, title, intent, by)
	return s.Artifact, s.New, s.Version, err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// recordRunArtifacts is a run's end: it records the lane's files that match the repo's
// globs, everything under the Shepherd directory, and a fresh version of every artifact
// the lane already has, attributing new content to this run. A failure is logged, never
// a reason to fail the run.
func (r *Runner) recordRunArtifacts(ctx context.Context, run store.Run, lane store.Lane) {
	if lane.Worktree == "" {
		return
	}
	type cand struct{ source string }
	paths := []string{}
	cands := map[string]cand{}
	add := func(p, source string) {
		if _, ok := cands[p]; !ok && len(paths) < maxScanArtifacts {
			cands[p] = cand{source}
			paths = append(paths, p)
		}
	}
	// What the lane already has, so a changed file gets its new version.
	if have, err := r.Store.ListArtifacts(ctx, store.ArtifactFilter{LaneID: lane.ID, State: store.ArtifactLive, Limit: maxScanArtifacts}); err == nil {
		for _, a := range have {
			add(a.Path, a.Source)
		}
	}
	if r.ArtifactScan != nil {
		found, err := r.ArtifactScan(ctx, lane.ID)
		if err != nil {
			r.Log.Warn("scan artifacts", "run", run.ID, "err", err)
		}
		for _, a := range found.Artifacts {
			if !a.Readable {
				continue
			}
			source := store.ArtifactDiscovered
			if a.Source == "committed" {
				source = store.ArtifactCommitted
			}
			add(a.Path, source)
		}
	}
	root := filepath.Join(lane.Worktree, filepath.FromSlash(strings.TrimSuffix(store.ArtifactDir, "/")))
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if rel, err := filepath.Rel(lane.Worktree, p); err == nil {
			add(filepath.ToSlash(rel), store.ArtifactDiscovered)
		}
		return nil
	})
	for _, p := range paths {
		if _, err := r.recordArtifact(ctx, run, lane, p, cands[p].source, "", "", ""); err != nil {
			// A file that went away or cannot be read is not news.
			r.Log.Debug("record artifact", "run", run.ID, "path", p, "err", err)
		}
	}
}

// LinkDecisionArtifacts ties the files an agent named in a question to the decision.
// Each was declared first, so it resolves to its row.
func (r *Runner) LinkDecisionArtifacts(ctx context.Context, d store.Decision, paths []string) error {
	run, err := r.Store.Run(ctx, d.RunID)
	if err != nil {
		return err
	}
	lane, err := r.Store.Lane(ctx, run.LaneID)
	if err != nil {
		return err
	}
	for _, p := range paths {
		rel, err := artifactPath(lane.Worktree, p)
		if err != nil {
			return err
		}
		a, err := r.Store.LaneArtifact(ctx, lane.ID, rel)
		if err != nil {
			return err
		}
		if err := r.Store.LinkArtifact(ctx, store.ArtifactLink{ArtifactID: a.ID, Kind: store.ArtifactLinkDecision, Ref: fmt.Sprint(d.ID)}); err != nil {
			return err
		}
	}
	return nil
}
