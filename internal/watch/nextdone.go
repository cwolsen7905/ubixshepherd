package watch

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ClosedRecheck is how long the watcher waits before asking the forge again about a
// closed lane whose item is still open and whose request was not merged.
const ClosedRecheck = time.Hour

// settleClosed marks done the items whose lane closed before the watcher saw its merge.
// A lane closed by hand is judged by git ancestry, which a squash merge fails, so the
// lane's stored request never became merged and the item stayed open. The forge's word
// is the proof: a merged request for the lane's branch records the merge on the lane and
// finishes the item, with the lane's close time as done_at. It reads the forge and never
// changes the lane. Forge errors go to note and the backoff.
func (w *Watcher) settleClosed(ctx context.Context, f forge.Forge, host string, closed []closedLane, note func(error)) {
	if w.closedRead == nil {
		w.closedRead = map[int64]time.Time{}
	}
	for _, cl := range closed {
		lane, its := cl.lane, cl.items
		lf, err := w.Store.LaneForge(ctx, lane.ID)
		if err != nil {
			continue
		}
		if !strings.EqualFold(lf.MRState, "merged") {
			if at, ok := w.closedRead[lane.ID]; ok && w.now().Sub(at) < ClosedRecheck {
				continue
			}
			if w.skipHost(host) {
				return
			}
			w.closedRead[lane.ID] = w.now()
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
				continue
			}
			// A branch name can be reused: only a request made after the lane opened is
			// this lane's. A forge that gives no creation time proves nothing.
			if mr.Created.IsZero() || mr.Created.Before(lane.Created) {
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
// the list once, for the whole pass.
func (w *Watcher) closedItemLanes(ctx context.Context) map[int64][]closedLane {
	items, err := w.Store.NextItems(ctx)
	if err != nil {
		return nil
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
	out := map[int64][]closedLane{}
	for _, id := range order {
		lane, err := w.Store.Lane(ctx, id)
		if err != nil || lane.State != store.LaneClosed {
			continue
		}
		out[lane.RepoID] = append(out[lane.RepoID], closedLane{lane, by[id]})
	}
	return out
}
