package watch

import (
	"context"
	"slices"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ownedByFollowThrough says a review thread is the follow-through owner's:
// the owner opened it or has a note in it, such as a triage summary or an answer to a
// finding with a proposal or a dismissal. Authorship is read from the notes, never from
// their text.
func ownedByFollowThrough(t forge.Thread, ft config.FollowThrough) bool {
	if ft.OwnsAuthor(t.Author) {
		return true
	}
	for _, n := range t.Entries {
		if ft.OwnsAuthor(n.Author) {
			return true
		}
	}
	return false
}

// raiseOwnerThreads raises to the person an approved request under after_approval that
// waits only on open threads the follow-through owner wrote or answered: nobody else is
// going to resolve them, so the merge would otherwise wait silently. It files one feed
// item naming the request and linking each thread's first note, and records the thread
// ids in next.OwnerBlocking, which the waiting list reads. It files again only when the
// set of threads changes, and clears the record once the threads are resolved, the
// approval is gone, another blocker holds the merge or the request is not open. A read that did not settle (forge error,
// head pipeline still running) leaves the record as it was.
func (w *Watcher) raiseOwnerThreads(ctx context.Context, lane store.Lane, mr *forge.MR, next *store.LaneForge, who []string, rd waitRead, ft config.FollowThrough) {
	if !rd.settled {
		return
	}
	var ids []string
	byID := map[string]forge.Thread{}
	if len(who) > 0 && len(rd.threads) > 0 {
		for _, t := range rd.threads {
			if !ownedByFollowThrough(t, ft) {
				ids, byID = nil, map[string]forge.Thread{}
				break
			}
			ids = append(ids, t.ID)
			byID[t.ID] = t
		}
	}
	slices.Sort(ids)
	set := strings.Join(ids, ",")
	// Resolving the threads must be all that is left: a red head pipeline, a conflict or
	// a needed rebase also holds the merge, so the person is not told the threads do. A
	// status that may yet settle (checking) leaves the record as it was.
	if set != "" {
		switch {
		case otherBlocker(mr):
			set = ""
		case mr.MergeStatus != "" && mr.MergeStatus != "mergeable":
			return
		}
	}
	if set == next.OwnerBlocking {
		return
	}
	next.OwnerBlocking = set
	if set == "" {
		return
	}
	w.feed(ctx, store.FeedMR, lane.ID, "!%d (%s) is approved by %s and waits only on %d review thread(s) %s has not resolved; they are yours: %s",
		mr.IID, mr.URL, strings.Join(who, ", "), len(ids), ft.Label(), threadLinks(ids, byID))
}

// otherBlocker says something besides open threads holds the request's merge for good:
// a failed head pipeline, a conflict, a needed rebase or a draft.
func otherBlocker(mr *forge.MR) bool {
	if mr.Draft || (mr.Pipeline != nil && mr.Pipeline.Status != "success") {
		return true
	}
	switch mr.MergeStatus {
	case "need_rebase", "conflict", "draft_status":
		return true
	}
	return false
}
