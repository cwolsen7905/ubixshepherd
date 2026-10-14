package watch

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// reviewThreads hands the open request's unresolved review threads that have not gone
// back yet to the lane's agent, on the same per-request try budget as a failed
// pipeline. It records the ids it handed (or decided to leave to the person) in next,
// so a restart or the next poll does not send them again. It never resolves a thread:
// the reviewer does that.
func (w *Watcher) reviewThreads(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, next *store.LaneForge, wc config.Watch) {
	tr, ok := f.(forge.ThreadReader)
	if !ok || mr.State != "opened" || !wc.Threads() {
		return
	}
	threads, err := tr.Threads(ctx, mr.IID)
	if err != nil {
		w.Log.Debug("watch: review threads", "lane", lane.Name, "err", err)
		return
	}
	var seen, fresh []string
	have := map[string]bool{}
	for _, id := range strings.Split(next.ThreadsSeen, ",") {
		if id != "" {
			have[id] = true
		}
	}
	for _, t := range threads {
		if have[t.ID] {
			seen = append(seen, t.ID) // still open: stays handed back; resolved ones drop out
		} else {
			fresh = append(fresh, t.ID)
		}
	}
	record := func(ids ...string) {
		next.ThreadsSeen = strings.Join(append(slices.Clone(seen), ids...), ",")
	}
	record()
	if len(fresh) == 0 {
		return
	}
	byID := map[string]forge.Thread{}
	for _, t := range threads {
		byID[t.ID] = t
	}

	runs, _ := w.Store.Runs(ctx, lane.ID, "", 20)
	var last *store.Run
	for i := range runs {
		if runs[i].State == store.RunRunning {
			return // wait for it to end; the threads are handed back on a later poll
		}
		if last == nil && runs[i].Session != "" {
			last = &runs[i]
		}
	}
	max := wc.Tries()
	switch {
	case w.Runner == nil || last == nil:
		w.feed(ctx, store.FeedPipeline, lane.ID, "!%d has %d unresolved review thread(s) and lane %s has no agent conversation to hand them to; they are yours", mr.IID, len(fresh), lane.Name)
		record(fresh...)
		return
	case next.FixTries >= max:
		w.feed(ctx, store.FeedPipeline, lane.ID, "!%d has %d new unresolved review thread(s), but %d fixes by %s are spent (watch.fix_tries); they are yours", mr.IID, len(fresh), next.FixTries, last.Agent)
		record(fresh...)
		return
	}

	var list strings.Builder
	for _, id := range fresh {
		t := byID[id]
		at := "general"
		if t.File != "" {
			at = t.File
			if t.Line > 0 {
				at = fmt.Sprintf("%s:%d", t.File, t.Line)
			}
		}
		fmt.Fprintf(&list, "== thread %s (%s) ==\n%s\n\n", t.ID, at, t.Body)
	}
	prompt := fmt.Sprintf("[Shepherd] Your merge request !%d (%s) has unresolved review threads.\n\n%s"+
		"Address each one within your scope: fix what it asks, run the gate, and commit. "+
		"Do not resolve the threads, approve or merge: the reviewer resolves them. "+
		"In your report say, per thread id, what you changed or why you did not. Do not push unless your brief allows it.",
		mr.IID, mr.URL, list.String())
	tries := next.FixTries
	if lf, err := w.Store.LaneForge(ctx, lane.ID); err == nil {
		lf.FixTries = tries + 1
		w.Store.PutLaneForge(ctx, lf)
	}
	run, err := w.Runner.Start(ctx, dispatch.StartRequest{Continue: last.ID, Prompt: prompt, Auto: true})
	if err != nil {
		if lf, err := w.Store.LaneForge(ctx, lane.ID); err == nil {
			lf.FixTries = tries
			w.Store.PutLaneForge(ctx, lf)
		}
		w.feed(ctx, store.FeedPipeline, lane.ID, "could not hand review threads to %s in lane %s: %s", last.Agent, lane.Name, firstLine(err.Error()))
		return
	}
	next.FixTries = tries + 1
	record(fresh...)
	w.feed(ctx, store.FeedPipeline, lane.ID, "asked %s to address %d review thread(s) on !%d (run %d, try %d of %d)", last.Agent, len(fresh), mr.IID, run.ID, next.FixTries, max)
}
