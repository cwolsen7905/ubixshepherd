package muster

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Artifact kinds of finding.
const (
	KindArtifactMissing   = "artifact.missing"
	KindArtifactSettle    = "artifact.settle"
	KindArtifactUndecided = "artifact.undecided"
)

// checkArtifacts keeps the artifact list to files that exist, for the artifacts of every workspace's lanes:
//   - a live artifact whose file is gone is marked gone (it stays in history);
//   - a live one on a closed lane gets the state its intent calls for;
//   - the ones undecided for too long join one batched finding.
func checkArtifacts(ctx context.Context, r *run) (int, error) {
	p := r.p
	wss, err := p.Store.Workspaces(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	lanes := map[int64]store.Lane{}
	repos := map[int64]store.Repo{}
	lookup := func(laneID int64) (store.Lane, store.Repo, bool) {
		l, ok := lanes[laneID]
		if !ok {
			var err error
			if l, err = p.Store.Lane(ctx, laneID); err != nil {
				return l, store.Repo{}, false
			}
			lanes[laneID] = l
		}
		rp, ok := repos[l.RepoID]
		if !ok {
			var err error
			if rp, err = p.Store.Repo(ctx, l.RepoID); err != nil {
				return l, rp, false
			}
			repos[l.RepoID] = rp
		}
		return l, rp, true
	}
	r.ran[KindArtifactMissing], r.ran[KindArtifactSettle], r.ran[KindArtifactUndecided] = true, true, true
	batchDays := p.conf().Muster.Batch()
	cutoff := p.now().Add(-time.Duration(batchDays) * 24 * time.Hour)
	for _, ws := range wss {
		live, err := p.Store.ListArtifacts(ctx, store.ArtifactFilter{WorkspaceID: ws.ID, State: store.ArtifactLive, Limit: 1000})
		if err != nil {
			return n, err
		}
		var undecided []store.Artifact
		for _, a := range live {
			l, rp, ok := lookup(a.LaneID)
			if !ok {
				continue
			}
			if l.State == store.LaneClosed {
				got, err := r.settleClosed(ctx, a, l, rp)
				n += got
				if err != nil {
					return n, err
				}
				continue
			}
			if l.Worktree != "" && !p.Probe.Exists(filepath.Join(l.Worktree, filepath.FromSlash(a.Path))) {
				got, err := r.raise(ctx, candidate{
					key: fmt.Sprintf("%s:%d:%s", KindArtifactMissing, a.ID, a.SHA256), kind: KindArtifactMissing,
					subjectKind: "artifact", subjectID: a.ID, repo: rp.Name,
					reason: fmt.Sprintf("%s in lane %s is gone from the worktree, so it leaves the list; its history stays", a.Path, l.Name),
					fix:    FixArtifactGone, auto: true, undoable: true,
				})
				n += got
				if err != nil {
					return n, err
				}
				continue
			}
			if a.Intent == store.ArtifactUndecided && batchDays > 0 && a.Created.Before(cutoff) {
				undecided = append(undecided, a)
			}
		}
		if len(undecided) > 0 {
			got, err := r.raiseUndecided(ctx, ws.ID, undecided, repos, lanes, batchDays)
			n += got
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

// settleClosed gives a live artifact of a closed lane the state its intent calls for:
// committed when the branch holds the file, kept when a copy is stored, gone otherwise;
// scratch nobody pinned is gone. An undecided one is left to the person.
func (r *run) settleClosed(ctx context.Context, a store.Artifact, l store.Lane, rp store.Repo) (int, error) {
	p := r.p
	if a.Intent == store.ArtifactUndecided {
		return 0, nil
	}
	state := store.ArtifactGone
	inBranch := p.Probe.InBranch != nil && p.Probe.InBranch(ctx, rp.Path, l.Branch, a.Path)
	switch {
	case inBranch:
		state = store.ArtifactInRepo
	case a.Intent == store.ArtifactScratch && !a.Pinned:
	case p.Probe.Snapshot != nil && p.Probe.Snapshot(a.ID, a.SHA256):
		state = store.ArtifactKept
	}
	return r.raise(ctx, candidate{
		key: fmt.Sprintf("%s:%d", KindArtifactSettle, a.ID), kind: KindArtifactSettle,
		subjectKind: "artifact", subjectID: a.ID, repo: rp.Name,
		reason: fmt.Sprintf("%s was left live when lane %s closed; its intent is %s, so it is now %s", a.Path, l.Name, a.Intent, state),
		fix:    FixArtifactSettle, fixArg: map[string]string{"state": state}, auto: true, undoable: true,
	})
}

// raiseUndecided makes one finding for a workspace's artifacts that have waited long
// enough. The batch is keyed by its members, so a changed set is a new batch and the
// old one clears itself.
func (r *run) raiseUndecided(ctx context.Context, wsID int64, list []store.Artifact, repos map[int64]store.Repo, lanes map[int64]store.Lane, days int) (int, error) {
	ids := make([]int64, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var names []string
	for _, a := range list {
		names = append(names, a.Path)
	}
	sort.Strings(names)
	more := ""
	if len(names) > 5 {
		more = fmt.Sprintf(" and %d more", len(names)-5)
		names = names[:5]
	}
	return r.raise(ctx, candidate{
		key:  fmt.Sprintf("%s:%d:%s", KindArtifactUndecided, wsID, joinIDs(ids)),
		kind: KindArtifactUndecided, subjectKind: "artifact", subjectID: ids[0],
		reason: fmt.Sprintf("%d artifacts have been undecided for %d days or more: %s%s. Keep them all, or decide each one", len(ids), days, strings.Join(names, ", "), more),
		fix:    FixArtifactKeepAll, fixArg: map[string][]int64{"ids": ids},
	})
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return strings.Join(parts, ",")
}

// Collapse shows artifacts with identical content once: of those sharing a hash within a
// lane, the first (the list's own order) stays and the rest are left out. Artifacts with
// no hash are never merged.
func Collapse(list []store.Artifact) []store.Artifact {
	type key struct {
		lane int64
		sha  string
	}
	seen := map[key]bool{}
	out := make([]store.Artifact, 0, len(list))
	for _, a := range list {
		if a.SHA256 != "" {
			k := key{a.LaneID, a.SHA256}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		out = append(out, a)
	}
	return out
}
