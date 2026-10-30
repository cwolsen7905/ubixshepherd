package watch

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A repo's merge requests may be followed through by an external owner (watch.follow_through):
// another service that fixes failed pipelines and answers review threads. Shepherd then
// stands back from what the owner is to act on, and takes an item over when the owner
// does not act within its patience, or says it could not (UnfixedMarker). Takeover is per
// item and final for it: a failed pipeline or a thread is either the owner's or
// Shepherd's, never both at once. The owner's progress is read from what the watcher
// already polls: a new pipeline, a thread that gained a note or went away. Shepherd's
// part is unchanged once it takes over: the lane's agent, on the same try budget.

// UnfixedMarker begins a note by the owner that says it could not act on a thread.
const UnfixedMarker = "Unfixed:"

// ownerItem is when Shepherd began waiting on the owner for one item, and whether it has
// taken the item over.
type ownerItem struct {
	id    string
	notes int       // a thread's note count when last seen; 0 for a pipeline
	since time.Time // when the wait began, or restarted by the owner acting
	taken bool

	// Pipelines only. attempts counts what the owner has tried on this failure: a retry of
	// a job in the same pipeline, or a newer pipeline. retried is set when the pipeline
	// was seen running again after it failed. finished is the failed pipeline's end time
	// when last seen, so a retry that ran and failed between two polls still shows.
	attempts int
	retried  bool
	finished time.Time
}

func (o ownerItem) String() string {
	state := "wait"
	if o.taken {
		state = "taken"
	}
	if o.retried && !o.taken {
		state = "retry"
	}
	return fmt.Sprintf("%s|%d|%d|%s|%d|%d", o.id, o.notes, o.since.Unix(), state, o.attempts, o.finished.Unix())
}

func parseOwnerItems(stored string) []ownerItem {
	var out []ownerItem
	for _, e := range strings.Split(stored, "\n") {
		p := strings.Split(e, "|")
		if len(p) != 4 && len(p) != 6 {
			continue
		}
		n, _ := strconv.Atoi(p[1])
		sec, _ := strconv.ParseInt(p[2], 10, 64)
		it := ownerItem{id: p[0], notes: n, since: time.Unix(sec, 0), taken: p[3] == "taken", retried: p[3] == "retry"}
		if len(p) == 6 {
			it.attempts, _ = strconv.Atoi(p[4])
			if fin, _ := strconv.ParseInt(p[5], 10, 64); fin > 0 {
				it.finished = time.Unix(fin, 0)
			}
		}
		out = append(out, it)
	}
	return out
}

func ownerKey(lane int64, what string) string { return fmt.Sprintf("owner/%d/%s", lane, what) }

func (w *Watcher) ownerItems(ctx context.Context, lane int64, what string) []ownerItem {
	s, _ := w.Store.Setting(ctx, ownerKey(lane, what))
	return parseOwnerItems(s)
}

func (w *Watcher) saveOwnerItems(ctx context.Context, lane int64, what string, items []ownerItem) {
	var lines []string
	for _, it := range items {
		lines = append(lines, it.String())
	}
	if err := w.Store.SetSetting(ctx, ownerKey(lane, what), strings.Join(lines, "\n")); err != nil {
		w.Log.Error("watch: save follow-through", "lane", lane, "err", err)
	}
}

// ownerPipeline waits for the external owner on a failed head pipeline, and hands the
// failure to the lane's agent once the owner's patience is spent or it has used up the
// repo's try cap. It runs on every poll of an open request. It returns the lane's fix
// tries after any handoff.
//
// An owner that retries a failed job is working, not giving up: the retry is one attempt,
// and when it fails again in the same pipeline the wait starts over, as it does for a newer
// pipeline. Both count against the cap (watch.fix_tries), the same number Shepherd's own
// rounds get, so an owner that keeps failing is taken over after that many tries without
// waiting out the patience each time.
func (w *Watcher) ownerPipeline(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, tries int, wc config.Watch) int {
	ft := wc.FollowThrough
	p := mr.Pipeline
	if !ft.External() || mr.State != "opened" || p == nil {
		return tries
	}
	id := strconv.FormatInt(p.ID, 10)
	items := w.ownerItems(ctx, lane.ID, "pipeline")
	if p.Status != "failed" {
		// The failed pipeline running again is the owner retrying it.
		if len(items) > 0 && items[0].id == id && !items[0].taken && !items[0].retried && (p.Status == "running" || p.Status == "pending") {
			items[0].retried = true
			w.saveOwnerItems(ctx, lane.ID, "pipeline", items[:1])
		}
		// The failure is over (fixed, canceled, skipped): its attempts do not carry to
		// whatever fails next.
		if len(items) > 0 && !items[0].taken && p.Status != "running" && p.Status != "pending" {
			w.saveOwnerItems(ctx, lane.ID, "pipeline", nil)
		}
		return tries
	}
	limit := wc.Tries()
	if len(items) == 0 || items[0].id != id {
		it := ownerItem{id: id, since: w.now(), finished: p.Finished}
		if len(items) > 0 && !items[0].taken {
			it.attempts = items[0].attempts + 1 // a newer pipeline: the owner pushed
		}
		w.saveOwnerItems(ctx, lane.ID, "pipeline", []ownerItem{it})
		if it.attempts > 0 && limit > 0 && it.attempts >= limit {
			return w.takeOverPipeline(ctx, f, lane, mr, tries, wc, it, "has tried %d times")
		}
		w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d (%s) failed for !%d (%s) (lane %s); %s follows it through%s and has %s to act before Shepherd does",
			p.ID, p.URL, mr.IID, mr.URL, lane.Name, ft.Label(), attemptNote(it.attempts, limit), ft.Wait())
		return tries
	}
	it := items[0]
	if it.taken {
		return tries
	}
	if it.finished.IsZero() && !p.Finished.IsZero() && !it.retried {
		// The forge gave no finish time at first sight: record it, it is not a retry.
		it.finished = p.Finished
		w.saveOwnerItems(ctx, lane.ID, "pipeline", []ownerItem{it})
	}
	if it.retried || (!it.finished.IsZero() && p.Finished.After(it.finished)) {
		// A retry in this pipeline failed again: one more attempt, and the wait restarts.
		it.attempts++
		it.retried, it.since, it.finished = false, w.now(), p.Finished
		w.saveOwnerItems(ctx, lane.ID, "pipeline", []ownerItem{it})
		if limit > 0 && it.attempts >= limit {
			return w.takeOverPipeline(ctx, f, lane, mr, tries, wc, it, "has retried %d times")
		}
		w.feed(ctx, store.FeedPipeline, lane.ID, "a retry in pipeline %d failed again for !%d (lane %s); that is one attempt by %s%s, so it has %s from now before Shepherd takes over",
			p.ID, mr.IID, lane.Name, ft.Label(), attemptNote(it.attempts, limit), ft.Wait())
		return tries
	}
	if w.now().Sub(it.since) < ft.Wait() {
		return tries
	}
	it.taken = true
	w.saveOwnerItems(ctx, lane.ID, "pipeline", []ownerItem{it})
	w.feed(ctx, store.FeedPipeline, lane.ID, "%s did not act on pipeline %d within %s; Shepherd takes it over (lane %s)", ft.Label(), p.ID, ft.Wait(), lane.Name)
	return w.pipelineFailed(ctx, f, lane, mr, p, tries, limit)
}

// takeOverPipeline takes a failed pipeline from the owner once it has used its attempts.
func (w *Watcher) takeOverPipeline(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, tries int, wc config.Watch, it ownerItem, why string) int {
	it.taken = true
	w.saveOwnerItems(ctx, lane.ID, "pipeline", []ownerItem{it})
	w.feed(ctx, store.FeedPipeline, lane.ID, "%s "+why+" and pipeline %s still fails; Shepherd takes it over (lane %s)", wc.FollowThrough.Label(), it.attempts, it.id, lane.Name)
	return w.pipelineFailed(ctx, f, lane, mr, mr.Pipeline, tries, wc.Tries())
}

func attemptNote(attempts, limit int) string {
	if attempts == 0 || limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" (attempt %d of %d)", attempts, limit)
}

// ownerThreads returns the threads Shepherd acts on: those not the owner's, and the
// owner's that it has taken over. An owner's thread stays the owner's until it gains a
// note (the owner acted; the wait restarts), its patience runs out, or the owner says
// UnfixedMarker. Resolved threads drop out of the record.
func (w *Watcher) ownerThreads(ctx context.Context, lane store.Lane, mr *forge.MR, threads []forge.Thread, ft config.FollowThrough) []forge.Thread {
	have := map[string]ownerItem{}
	for _, it := range w.ownerItems(ctx, lane.ID, "threads") {
		have[it.id] = it
	}
	now := w.now()
	var out []forge.Thread
	var items []ownerItem
	var waiting []forge.Thread
	for _, t := range threads {
		if !ft.OwnsAuthor(t.Author) {
			out = append(out, t)
			continue
		}
		it, ok := have[t.ID]
		switch {
		case !ok:
			it = ownerItem{id: t.ID, notes: t.Notes, since: now}
			if ownerUnfixed(t, ft) {
				it.taken = true
				w.feed(ctx, store.FeedPipeline, lane.ID, "%s could not address a review thread on !%d (%s); Shepherd takes it over", ft.Label(), mr.IID, t.Link())
			} else {
				waiting = append(waiting, t)
			}
		case it.taken:
		case ownerUnfixed(t, ft):
			it.taken = true
			w.feed(ctx, store.FeedPipeline, lane.ID, "%s could not address a review thread on !%d (%s); Shepherd takes it over", ft.Label(), mr.IID, t.Link())
		case t.Notes > it.notes:
			it.notes, it.since = t.Notes, now // the owner replied: it is still working
		case now.Sub(it.since) >= ft.Wait():
			it.taken = true
			w.feed(ctx, store.FeedPipeline, lane.ID, "%s did not act on a review thread on !%d (%s) within %s; Shepherd takes it over", ft.Label(), mr.IID, t.Link(), ft.Wait())
		}
		items = append(items, it)
		if it.taken {
			out = append(out, t)
		}
	}
	if len(waiting) > 0 {
		var ids []string
		for _, t := range waiting {
			ids = append(ids, t.Link())
		}
		w.feed(ctx, store.FeedPipeline, lane.ID, "!%d has %d review thread(s) from %s; it has %s to act before Shepherd does: %s", mr.IID, len(waiting), ft.Label(), ft.Wait(), strings.Join(ids, ", "))
	}
	if len(items) != len(have) || len(waiting) > 0 || changedOwner(items, have) {
		w.saveOwnerItems(ctx, lane.ID, "threads", items)
	}
	return out
}

func changedOwner(items []ownerItem, have map[string]ownerItem) bool {
	for _, it := range items {
		if h := have[it.id]; h != it {
			return true
		}
	}
	return false
}

// ownerUnfixed says whether the owner's own note on the thread begins with UnfixedMarker.
func ownerUnfixed(t forge.Thread, ft config.FollowThrough) bool {
	for _, line := range strings.Split(t.Body, "\n") {
		user, text, ok := strings.Cut(line, ": ")
		if ok && ft.OwnsAuthor(user) && strings.HasPrefix(strings.TrimSpace(text), UnfixedMarker) {
			return true
		}
	}
	return false
}
