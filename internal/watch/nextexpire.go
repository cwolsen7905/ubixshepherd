package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// expireProposals defers the proposals nobody approved. A proposed item is one the desk
// or an agent put up, so it waits on the person; if it has sat planned and untouched for
// next.expire_days (0 turns this off) it moves to deferred. The person did not choose
// that, so it is recorded three ways: a plan feed line, a sentence in the item's note
// with the date, and the item's updated time. They can approve it from deferred as usual.
// The person's own and chat-registered items are never expired: they wait on no one.
func (w *Watcher) expireProposals(ctx context.Context) {
	days := w.conf().Next.Expiry()
	if days <= 0 {
		return
	}
	items, err := w.Store.NextItems(ctx)
	if err != nil {
		w.Log.Warn("watch: read next-up items to expire proposals", "err", err)
		return
	}
	now := w.now()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	for _, it := range items {
		proposed := it.Source == store.NextByDesk || it.Source == store.NextByAgent
		if !proposed || it.State != store.NextPlanned || it.Updated.After(cutoff) {
			continue
		}
		deferred := store.NextDeferred
		why := fmt.Sprintf("Expired to deferred on %s: proposed and not approved for %d days.", now.UTC().Format("2006-01-02"), days)
		note := why
		if it.Note != "" {
			note = it.Note + "\n" + why
		}
		if _, err := w.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{State: &deferred, Note: &note}); err != nil {
			w.Log.Warn("watch: expire a next item", "item", it.ID, "err", err)
			continue
		}
		w.Log.Info("next item expired to deferred", "item", it.ID, "days", days)
		w.feed(ctx, store.FeedPlan, it.ID, "next-up item %d expired to deferred after %d days unapproved: %s", it.ID, days, it.Title)
	}
}
