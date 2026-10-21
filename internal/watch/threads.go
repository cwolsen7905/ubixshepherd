package watch

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// seenThread is a review thread already handed back to the lane's agent: how many notes
// it had then, and whether it has gone back a second time.
type seenThread struct {
	id      string
	notes   int
	retried bool
}

// String is the stored form: the id, "@n" when the thread had other than one note, and
// "+" once it has been handed back again.
func (s seenThread) String() string {
	out := s.id
	if s.notes != 1 {
		out += "@" + strconv.Itoa(s.notes)
	}
	if s.retried {
		out += "+"
	}
	return out
}

func parseSeen(stored string) []seenThread {
	var out []seenThread
	for _, e := range strings.Split(stored, ",") {
		if e == "" {
			continue
		}
		s := seenThread{notes: 1}
		if strings.HasSuffix(e, "+") {
			s.retried, e = true, strings.TrimSuffix(e, "+")
		}
		if id, n, ok := strings.Cut(e, "@"); ok {
			if v, err := strconv.Atoi(n); err == nil {
				s.notes = v
			}
			e = id
		}
		s.id = e
		out = append(out, s)
	}
	return out
}

// reviewThreads hands the open request's unresolved review threads to the lane's agent,
// on the same per-request try budget as a failed pipeline: the fresh ones, and once more
// any handed back earlier that are still open with no reply, because the agent's run
// ended without answering them. A thread the agent replied to (or resolved, which drops
// it from the unresolved list) is not sent again. It records what it handed (or decided
// to leave to the person) in next, so a restart or the next poll does not repeat it. The
// agent replies and resolves its own threads; Shepherd never does.
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
	if ft := wc.FollowThrough; ft.External() {
		threads = w.ownerThreads(ctx, lane, mr, threads, ft)
	}
	have := map[string]seenThread{}
	for _, s := range parseSeen(next.ThreadsSeen) {
		have[s.id] = s
	}
	var kept []string // still open and handed back: stays recorded; resolved ones drop out
	var fresh, again []string
	for _, t := range threads {
		n := max(t.Notes, 1)
		s, ok := have[t.ID]
		switch {
		case !ok:
			fresh = append(fresh, t.ID)
		case !s.retried && n <= s.notes:
			again = append(again, t.ID) // no reply since it went back
			kept = append(kept, s.String())
		default:
			kept = append(kept, s.String())
		}
	}
	record := func(add ...seenThread) {
		out := slices.Clone(kept)
		for _, s := range add {
			out = append(out, s.String())
		}
		next.ThreadsSeen = strings.Join(out, ",")
	}
	record()
	if len(fresh)+len(again) == 0 {
		return
	}
	byID := map[string]forge.Thread{}
	for _, t := range threads {
		byID[t.ID] = t
	}
	// handed is the record of sending these now (or leaving them to the person, which
	// is final): fresh ones are new entries, the repeats are marked so they go back only
	// once more.
	handed := func(final bool) {
		var add []seenThread
		for _, id := range fresh {
			add = append(add, seenThread{id: id, notes: max(byID[id].Notes, 1), retried: final})
		}
		for _, id := range again {
			kept = slices.DeleteFunc(kept, func(e string) bool { return parseSeenOne(e) == id })
			add = append(add, seenThread{id: id, notes: have[id].notes, retried: true})
		}
		record(add...)
	}
	all := append(slices.Clone(fresh), again...)

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
	limit := wc.Tries()
	switch {
	case w.Runner == nil || last == nil:
		w.feed(ctx, store.FeedPipeline, lane.ID, "!%d (%s) has %d unresolved review thread(s) and lane %s has no agent conversation to hand them to; they are yours: %s", mr.IID, mr.URL, len(all), lane.Name, threadLinks(all, byID))
		handed(true)
		return
	case next.FixTries >= limit:
		w.feed(ctx, store.FeedPipeline, lane.ID, "!%d has %d unresolved review thread(s) to hand back, but %d fixes by %s are spent (watch.fix_tries); they are yours: %s", mr.IID, len(all), next.FixTries, last.Agent, threadLinks(all, byID))
		handed(true)
		return
	}

	var list strings.Builder
	for _, id := range all {
		t := byID[id]
		at := "general"
		if t.File != "" {
			at = t.File
			if t.Line > 0 {
				at = fmt.Sprintf("%s:%d", t.File, t.Line)
			}
		}
		fmt.Fprintf(&list, "== thread %s (%s) ==\n", t.Link(), at)
		if slices.Contains(again, id) {
			list.WriteString("(handed to you before and still unanswered: reply on it this time)\n")
		}
		fmt.Fprintf(&list, "%s\n", t.Body)
		fmt.Fprintf(&list, "reply:   %s\nresolve: %s\n\n", replyCommand(f, mr.IID, id), resolveCommand(f, mr.IID, id))
	}
	prompt := fmt.Sprintf("[Shepherd] Your merge request [!%d](%s) has unresolved review threads.\n\n%s"+
		"Address each one within your scope: fix what it asks, run the gate, commit, and push to this merge request's branch "+
		"(pushing to it is allowed here whatever your brief says; the pre-push hook still checks scope). "+
		"Then, for each thread you fixed, reply on it saying what changed with a link to the commit (%s/-/commit/<sha>), "+
		"and resolve it, using the commands given with the thread. "+
		"If you disagree with a thread, reply starting `Dismissed: <reason>` and leave it open for the person. "+
		"Never approve or merge the request. "+
		"In your report say, per thread, what you changed or why you did not, naming each thread by the clickable link given above (never by its bare id).",
		mr.IID, mr.URL, list.String(), projectURL(mr.URL))
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
	handed(false)
	w.feed(ctx, store.FeedPipeline, lane.ID, "asked %s to address %d review thread(s) on !%d (run %d, try %d of %d)", last.Agent, len(all), mr.IID, run.ID, next.FixTries, limit)
}

func parseSeenOne(e string) string {
	if s := parseSeen(e); len(s) == 1 {
		return s[0].id
	}
	return ""
}

// replyCommand and resolveCommand are the commands that answer and resolve a thread, from
// the forge when it can say, else a plain description.
func replyCommand(f forge.Forge, mr int, id string) string {
	if c, ok := f.(forge.ThreadCommander); ok {
		return c.ReplyCommand(mr, id)
	}
	return "reply on discussion " + id + " with your forge's CLI"
}

func resolveCommand(f forge.Forge, mr int, id string) string {
	if c, ok := f.(forge.ThreadCommander); ok {
		return c.ResolveCommand(mr, id)
	}
	return "resolve discussion " + id + " with your forge's CLI"
}

// projectURL is a merge request's project address: its link without /-/merge_requests/N.
func projectURL(mrURL string) string {
	if i := strings.Index(mrURL, "/-/merge_requests/"); i >= 0 {
		return mrURL[:i]
	}
	return mrURL
}

// threadLinks is the threads as markdown links to their first notes, comma separated.
func threadLinks(ids []string, byID map[string]forge.Thread) string {
	var out []string
	for _, id := range ids {
		out = append(out, byID[id].Link())
	}
	return strings.Join(out, ", ")
}
