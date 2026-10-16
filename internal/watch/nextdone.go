package watch

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ClosedRecheck is how long the watcher waits before asking the forge again about a
// closed lane whose item is still open and whose request was not merged. Each answer
// that settles nothing doubles the wait, up to ClosedRecheckMax, so a lane whose request
// never merges is asked about weekly, not hourly for ever.
const (
	ClosedRecheck    = time.Hour
	ClosedRecheckMax = 7 * 24 * time.Hour
)

// closedPoll is when the forge was last asked about a closed lane, and how many times.
type closedPoll struct {
	at time.Time
	n  int
}

// wait is how long to leave the lane alone after n unsettling answers.
func (p closedPoll) wait() time.Duration {
	d := ClosedRecheck
	for i := 1; i < p.n && d < ClosedRecheckMax; i++ {
		d *= 2
	}
	if d > ClosedRecheckMax {
		d = ClosedRecheckMax
	}
	return d
}

// pruneClosedRead forgets the lanes that no longer have an open item linked. A lane whose
// read failed still has its item, so its entry stays; when the items could not be read
// (linked is nil) nothing is forgotten.
func (w *Watcher) pruneClosedRead(linked map[int64]bool) {
	if linked == nil {
		return
	}
	for id := range w.closedRead {
		if !linked[id] {
			delete(w.closedRead, id)
		}
	}
}

// coversBranch reports whether the lane's local branch, if it still exists, holds nothing
// beyond the merged request's head: work pushed after the merge is not in it. A branch
// that is gone has nothing more; a head the forge did not give cannot be checked.
func coversBranch(ctx context.Context, repoPath string, lane store.Lane, head string) bool {
	if !git.RefExists(ctx, repoPath, "refs/heads/"+lane.Branch) {
		return true
	}
	if head == "" {
		return false
	}
	if !git.Ok(ctx, repoPath, "cat-file", "-e", head+"^{commit}") {
		git.Run(ctx, repoPath, "fetch", "--quiet", "origin")
	}
	return git.Ok(ctx, repoPath, "merge-base", "--is-ancestor", "refs/heads/"+lane.Branch, head)
}

// settleClosed marks done the items whose lane closed before the watcher saw its merge.
// A lane closed by hand is judged by git ancestry, which a squash merge fails, so the
// lane's stored request never became merged and the item stayed open. The forge's word
// is the proof: a merged request for the lane's branch records the merge on the lane and
// finishes the item, with the lane's close time as done_at. It reads the forge and never
// changes the lane. Forge errors go to note and the backoff.
func (w *Watcher) settleClosed(ctx context.Context, f forge.Forge, host, repoPath string, closed []closedLane, note func(error)) {
	if w.closedRead == nil {
		w.closedRead = map[int64]closedPoll{}
	}
	for _, cl := range closed {
		lane, its := cl.lane, cl.items
		lf, err := w.Store.LaneForge(ctx, lane.ID)
		if err != nil {
			continue
		}
		if !strings.EqualFold(lf.MRState, "merged") {
			if p, ok := w.closedRead[lane.ID]; ok && w.now().Sub(p.at) < p.wait() {
				continue
			}
			if w.skipHost(host) {
				return
			}
			// A failed call leaves the wait where it was; only an answer that settles
			// nothing lengthens it.
			p := w.closedRead[lane.ID]
			w.closedRead[lane.ID] = closedPoll{w.now(), p.n}
			unsettled := func() { w.closedRead[lane.ID] = closedPoll{w.now(), p.n + 1} }
			mr, err := f.MRForBranch(ctx, lane.Branch)
			if err != nil {
				note(err)
				if !w.result(host, err) {
					return
				}
				continue
			}
			w.result(host, nil)
			if mr == nil || !strings.EqualFold(mr.State, "merged") {
				unsettled()
				continue
			}
			// A branch name can be reused: only a request made after the lane opened is
			// this lane's. A forge that gives no creation time proves nothing.
			if mr.Created.IsZero() || mr.Created.Before(lane.Created) {
				unsettled()
				continue
			}
			// Work pushed after the merge is not in it: the item is not done.
			if !coversBranch(ctx, repoPath, lane, mr.SHA) {
				unsettled()
				continue
			}
			lf.MR, lf.MRState, lf.MRURL, lf.MergeSHA = mr.IID, mr.State, mr.URL, mr.MergeSHA
			lf.MRCreated, lf.MRUpdated = mr.Created.UTC(), mr.Updated.UTC()
			if err := w.Store.PutLaneForge(ctx, lf); err != nil {
				w.Log.Error("watch: save", "lane", lane.Name, "err", err)
				continue
			}
		}
		for _, it := range its {
			w.finishItem(ctx, it, lane, lf)
		}
	}
}

// finishItem marks an item done because its lane's request merged.
func (w *Watcher) finishItem(ctx context.Context, it store.NextItem, lane store.Lane, lf store.LaneForge) {
	if !store.Settle(it, &lane, lf).Merged {
		return
	}
	done := store.NextDone
	patch := store.NextPatch{State: &done, DoneAt: lane.Closed}
	if lf.MR != 0 && it.MR == "" {
		mr := "!" + strconv.Itoa(lf.MR)
		patch.MR = &mr
	}
	upd, err := w.Store.UpdateNextItem(ctx, it.ID, patch)
	if err != nil {
		w.Log.Warn("watch: finish a next item", "item", it.ID, "err", err)
		return
	}
	w.Log.Info("next item done: its lane's merge request merged", "item", it.ID, "lane", lane.Name)
	w.feed(ctx, store.FeedPlan, it.ID, "%s", fmt.Sprintf("next-up item %d done: %s (lane %s merged)", it.ID, upd.Title, lane.Name))
}

// closedLane is a closed lane and the open items linked to it.
type closedLane struct {
	lane  store.Lane
	items []store.NextItem
}

// closedItemLanes finds the closed lanes that still have an open item linked, by repo:
// Lanes lists only the lanes not closed, so these are found through the items. It reads
// the list once, for the whole pass. linked is every lane with an open item, closed or
// not, and nil when the items could not be read.
func (w *Watcher) closedItemLanes(ctx context.Context) (out map[int64][]closedLane, linked map[int64]bool) {
	items, err := w.Store.NextItems(ctx)
	if err != nil {
		w.Log.Warn("watch: read next-up items", "err", err)
		return nil, nil
	}
	by := map[int64][]store.NextItem{}
	var order []int64
	for _, it := range items {
		if it.LaneID == 0 || it.State == store.NextDone || it.State == store.NextDropped {
			continue
		}
		if _, ok := by[it.LaneID]; !ok {
			order = append(order, it.LaneID)
		}
		by[it.LaneID] = append(by[it.LaneID], it)
	}
	out = map[int64][]closedLane{}
	linked = map[int64]bool{}
	for _, id := range order {
		linked[id] = true
		lane, err := w.Store.Lane(ctx, id)
		if err != nil || lane.State != store.LaneClosed {
			continue
		}
		out[lane.RepoID] = append(out[lane.RepoID], closedLane{lane, by[id]})
	}
	return out, linked
}
