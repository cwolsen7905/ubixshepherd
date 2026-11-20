package watch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// closeForgeMerged finishes a close the merge poll could not. checkLane closes a lane on
// the poll that sees its request go from open to merged; if that close was refused (a
// dirty worktree the person has since cleaned, an agent run still going, a forge that
// had not yet named the merge commit), the request is already recorded as merged and no
// later poll acts on it. This retries from the forge's own word, merge commit or squash
// alike (git cannot see a squash as merged), without force: Fold.CloseMerged still
// refuses a dirty worktree and work beyond the request's head.
//
// It acts only for the request the lane was seen with (the recorded one), merged and
// created after the lane, so a request merged before the lane existed proves nothing.
// It is tried at most once per AutoCloseSettle per lane, and reports whether it closed
// the lane.
func (w *Watcher) closeForgeMerged(ctx context.Context, lane store.Lane) bool {
	if w.Fold == nil {
		return false
	}
	lf, err := w.Store.LaneForge(ctx, lane.ID)
	if err != nil || lf.MR == 0 || lf.MRState != "merged" {
		return false
	}
	// Keyed by the negated id so it does not share autoClose's throttle slot.
	if w.reviewed == nil {
		w.reviewed = map[int64]time.Time{}
	}
	key := -lane.ID
	// The first sight starts the clock: the poll that saw the merge has just tried.
	at, ok := w.reviewed[key]
	if !ok {
		w.reviewed[key] = w.now()
		return false
	}
	if w.now().Sub(at) < AutoCloseSettle {
		return false
	}
	w.reviewed[key] = w.now()
	repo, err := w.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return false
	}
	f, err := w.forgeFor(repo)
	if err != nil {
		return false
	}
	mr, err := f.MRForBranch(ctx, lane.Branch)
	if err != nil || mr == nil {
		return false
	}
	if w.firstSightMerged(ctx, lane, mr.IID) {
		return false
	}
	if mr.IID != lf.MR || mr.State != "merged" || mr.MergeSHA == "" || mr.Created.IsZero() || !mr.Created.After(lane.Created) {
		return false
	}
	// Quiet until it works: a refusal that lasts (uncommitted work the person keeps, a
	// run left going) must not post the same feed entry every interval. The merge poll
	// has already said why it could not close.
	res, err := w.Fold.CloseMerged(ctx, lane.ID, mr.MergeSHA, mr.SHA)
	if err != nil {
		w.Log.Debug("watch: merged lane not closed", "lane", lane.Name, "mr", mr.IID, "why", firstLine(err.Error()))
		return false
	}
	w.Log.Info("watch: lane closed on merge, retried", "lane", lane.Name, "mr", mr.IID, "merge", mr.MergeSHA)
	if err := w.Fold.WriteView(ctx, lane.RepoID); err != nil {
		w.Log.Error("watch: coordination view", "err", err)
	}
	note := ""
	if res.BranchDeleted {
		note = ", local branch deleted"
	}
	w.feed(ctx, store.FeedLaneClosed, lane.ID, "!%d merged at %s; lane %s closed%s", mr.IID, short(mr.MergeSHA), lane.Name, note)
	lf.MergeSHA = mr.MergeSHA
	if err := w.Store.PutLaneForge(ctx, lf); err != nil {
		w.Log.Error("watch: save", "lane", lane.Name, "err", err)
	}
	return true
}

// firstSightMerged reports whether the merge poll deliberately left this lane open
// because the request was already merged when Shepherd first looked (checkLane reports
// it and never acts: such a request proves nothing about the work in the lane now). That
// rule stands for the retry too, so it acts only on a merge seen happening. The poll's
// own feed entry is the record of it.
func (w *Watcher) firstSightMerged(ctx context.Context, lane store.Lane, iid int) bool {
	items, err := w.Store.Feed(ctx, 0, 5000)
	if err != nil {
		return true // cannot tell: leave the lane to the person
	}
	prefix := fmt.Sprintf("!%d for lane %s was already merged when Shepherd first looked", iid, lane.Name)
	for _, it := range items {
		if it.Ref == lane.ID && strings.HasPrefix(it.Text, prefix) {
			return true
		}
	}
	return false
}
