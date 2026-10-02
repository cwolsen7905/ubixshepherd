// Package watch keeps lanes in step with their forge: it polls the merge request for
// each open lane's branch and acts on what changed. A merged request is proof, and the
// lane closes; a failed pipeline goes back to the lane's agent to fix; everything else
// is a line in the person's thread. Polling, because a Shepherd on a laptop cannot
// receive webhooks.
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// MaxFixTries is how many times Shepherd hands a failed pipeline back to the lane's agent
// on its own before leaving it to the person.
const MaxFixTries = 2

// Watcher polls forges for open lanes.
type Watcher struct {
	Store    store.Store
	Fold     *fold.Fold
	Runner   *dispatch.Runner
	Log      *slog.Logger
	Interval time.Duration
	// ForgeFor returns a repo's forge; tests replace it.
	ForgeFor func(remote string) (forge.Forge, error)

	mu      sync.Mutex
	skipped map[int64]bool // repos with no readable forge, logged once
}

// Run checks every Interval until ctx is done.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check looks at every open lane once.
func (w *Watcher) Check(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.skipped == nil {
		w.skipped = map[int64]bool{}
	}
	wss, err := w.Store.Workspaces(ctx)
	if err != nil {
		w.Log.Error("watch: workspaces", "err", err)
		return
	}
	for _, ws := range wss {
		repos, err := w.Store.Repos(ctx, ws.ID)
		if err != nil {
			continue
		}
		for _, repo := range repos {
			lanes, err := w.Store.Lanes(ctx, repo.ID)
			if err != nil || len(lanes) == 0 {
				continue
			}
			f, err := w.forgeFor(repo)
			if err != nil {
				if !w.skipped[repo.ID] {
					w.skipped[repo.ID] = true
					w.Log.Info("watch: not watching repo", "repo", repo.Name, "why", err)
				}
				continue
			}
			for _, lane := range lanes {
				if lane.State == store.LaneOpen {
					w.checkLane(ctx, f, lane)
				}
			}
		}
	}
}

func (w *Watcher) forgeFor(repo store.Repo) (forge.Forge, error) {
	if w.ForgeFor != nil {
		return w.ForgeFor(repo.Remote)
	}
	return forge.For(repo.Remote)
}

func (w *Watcher) feed(ctx context.Context, kind string, ref int64, format string, a ...any) {
	if err := w.Store.AddFeed(ctx, kind, redact.String(fmt.Sprintf(format, a...)), ref); err != nil {
		w.Log.Error("watch: feed", "err", err)
	}
}

func (w *Watcher) checkLane(ctx context.Context, f forge.Forge, lane store.Lane) {
	mr, err := f.MRForBranch(ctx, lane.Branch)
	if err != nil {
		w.Log.Error("watch: merge request", "lane", lane.Name, "err", err)
		return
	}
	if mr == nil {
		return
	}
	prev, err := w.Store.LaneForge(ctx, lane.ID)
	if err != nil {
		return
	}
	next := prev
	next.MR, next.MRState, next.MRURL = mr.IID, mr.State, mr.URL
	if mr.IID != prev.MR {
		next.FixTries, next.Pipeline, next.PipelineStatus = 0, 0, ""
		w.feed(ctx, store.FeedMR, lane.ID, "!%d %s for lane %s: %s", mr.IID, mr.State, lane.Name, mr.URL)
	}

	if p := mr.Pipeline; p != nil && mr.State == "opened" && (p.ID != prev.Pipeline || p.Status != prev.PipelineStatus) {
		next.Pipeline, next.PipelineStatus = p.ID, p.Status
		switch p.Status {
		case "success":
			w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d passed for !%d (lane %s)", p.ID, mr.IID, lane.Name)
		case "failed":
			next.FixTries = w.pipelineFailed(ctx, f, lane, mr, p, next.FixTries)
		case "canceled":
			w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d was canceled for !%d (lane %s)", p.ID, mr.IID, lane.Name)
		}
	}

	if mr.State == "merged" && prev.MRState != "merged" {
		w.merged(ctx, lane, mr)
	}
	if mr.State == "closed" && prev.MRState != "closed" && mr.IID == prev.MR {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d was closed without merging; lane %s stays open", mr.IID, lane.Name)
	}
	if next != prev {
		if err := w.Store.PutLaneForge(ctx, next); err != nil {
			w.Log.Error("watch: save", "lane", lane.Name, "err", err)
		}
	}
}

// merged closes the lane on the forge's proof, or says why it cannot.
func (w *Watcher) merged(ctx context.Context, lane store.Lane, mr *forge.MR) {
	if mr.MergeSHA == "" {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d is merged but the forge gives no merge commit yet; lane %s stays open until it does", mr.IID, lane.Name)
		return
	}
	res, err := w.Fold.CloseMerged(ctx, lane.ID, mr.MergeSHA)
	if err != nil {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d merged at %s, but lane %s cannot close: %s", mr.IID, short(mr.MergeSHA), lane.Name, firstLine(err.Error()))
		return
	}
	w.Log.Info("watch: lane closed on merge", "lane", lane.Name, "mr", mr.IID, "merge", mr.MergeSHA)
	note := ""
	if res.BranchDeleted {
		note = ", local branch deleted"
	}
	w.feed(ctx, store.FeedLaneClosed, lane.ID, "!%d merged at %s; lane %s closed%s", mr.IID, short(mr.MergeSHA), lane.Name, note)
}

// pipelineFailed hands a failed pipeline to the lane's agent while tries remain, and to
// the person after. It returns the tries used.
func (w *Watcher) pipelineFailed(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, p *forge.Pipeline, tries int) int {
	jobs, err := f.FailedJobs(ctx, p.ID)
	if err != nil {
		w.Log.Error("watch: failed jobs", "lane", lane.Name, "err", err)
	}
	var names []string
	var logs strings.Builder
	for _, j := range jobs {
		names = append(names, j.Name)
		tail, err := f.JobLog(ctx, j.ID, 60)
		if err != nil {
			tail = "(could not read the log: " + err.Error() + ")"
		}
		fmt.Fprintf(&logs, "== job %s (%s) ==\n%s\n\n", j.Name, j.URL, tail)
	}
	what := strings.Join(names, ", ")
	if what == "" {
		what = "no failed job listed"
	}
	w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d failed for !%d (lane %s): %s", p.ID, mr.IID, lane.Name, what)

	runs, _ := w.Store.Runs(ctx, lane.ID, "", 20)
	var last *store.Run
	for i := range runs {
		if runs[i].State == store.RunRunning {
			w.feed(ctx, store.FeedPipeline, lane.ID, "lane %s has an agent running; the failure waits for you", lane.Name)
			return tries
		}
		if last == nil && runs[i].Session != "" {
			last = &runs[i]
		}
	}
	switch {
	case w.Runner == nil || last == nil:
		w.feed(ctx, store.FeedPipeline, lane.ID, "lane %s has no agent conversation to hand the failure to; it is yours", lane.Name)
		return tries
	case tries >= MaxFixTries:
		w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline failed again after %d fixes by %s; lane %s is yours now", tries, last.Agent, lane.Name)
		return tries
	}
	prompt := fmt.Sprintf("[Shepherd] The pipeline for your merge request !%d failed (pipeline %d, %s).\n\n%s"+
		"Find the cause and fix it within your scope, run the gate, and commit. Do not push: the person pushes the fix.",
		mr.IID, p.ID, p.URL, logs.String())
	run, err := w.Runner.Start(ctx, dispatch.StartRequest{Continue: last.ID, Prompt: prompt})
	if err != nil {
		w.feed(ctx, store.FeedPipeline, lane.ID, "could not hand the failure to %s in lane %s: %s", last.Agent, lane.Name, firstLine(err.Error()))
		return tries
	}
	tries++
	w.feed(ctx, store.FeedPipeline, lane.ID, "asked %s to fix it (run %d, try %d of %d); push its fix when it is done", last.Agent, run.ID, tries, MaxFixTries)
	return tries
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
