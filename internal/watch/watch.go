// Package watch keeps lanes in step with their forge: it polls the merge request for
// each open lane's branch and acts on what changed. A merged request is proof, and the
// lane closes; a failed pipeline goes back to the lane's agent to fix; everything else
// is a line in the person's thread. It also watches the repos others follow, and a
// release of one opens a lane in each follower (follow.go). Polling, because a Shepherd on a laptop cannot
// receive webhooks.
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/workspace"
)

// MaxFixTries is how many times Shepherd hands a failure back to the lane's agent on its
// own before leaving it to the person, when the config does not say (watch.fix_tries).
const MaxFixTries = config.DefaultFixTries

// Watcher polls forges for open lanes.
type Watcher struct {
	Store    store.Store
	Fold     *fold.Fold
	Runner   *dispatch.Runner
	Log      *slog.Logger
	Interval time.Duration
	// ForgeFor returns a repo's forge; tests replace it.
	ForgeFor func(remote string) (forge.Forge, error)
	// Clock is the time, for the backoff from an unreachable host; tests replace it.
	Clock func() time.Time
	// NoAutoClose turns off closing a finished lane by itself (autoclose.go). It is
	// off by default; the daemon does not yet expose it as a setting.
	NoAutoClose bool

	mu      sync.Mutex
	skipped map[int64]bool        // repos with no readable forge, logged once
	noted   map[string]bool       // follow problems, logged once
	down    map[string]*hostState // forge hosts that did not answer, by host (backoff.go)
	// reviewed is when auto-close last judged a lane (autoclose.go), by lane id.
	reviewed map[int64]time.Time
	// approvalRead is what each lane's approval was last read for (approval.go).
	approvalRead map[int64]approvalRead
	// closedRead is when the forge was last asked about a closed lane's request, by lane
	// id (nextdone.go).
	closedRead map[int64]closedPoll

	// statMu guards poll; apart from mu, which Check holds for a whole pass, so Status
	// never waits on a forge.
	statMu sync.Mutex
	poll   Status
}

// Status is how the watcher's polling last went.
type Status struct {
	// LastPoll is when a pass last finished with no forge error; zero before one has.
	LastPoll time.Time
	// Error is the last pass's first forge error, redacted, on one line; empty when
	// the last pass had none. ErrorAt says when it happened.
	Error   string
	ErrorAt time.Time
}

// Status reports how polling last went. It does not block on a pass in progress.
func (w *Watcher) Status() Status {
	w.statMu.Lock()
	defer w.statMu.Unlock()
	return w.poll
}

// passed records a finished pass: its first error, or none.
func (w *Watcher) passed(err error) {
	w.statMu.Lock()
	defer w.statMu.Unlock()
	now := w.now()
	if err == nil {
		w.poll.LastPoll, w.poll.Error, w.poll.ErrorAt = now, "", time.Time{}
		return
	}
	w.poll.Error = strings.Join(strings.Fields(redact.String(err.Error())), " ")
	w.poll.ErrorAt = now
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

// Check looks at every open lane, and every followed repo's releases, once.
func (w *Watcher) Check(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.skipped == nil {
		w.skipped = map[int64]bool{}
	}
	var first error // the pass's first forge or store error, for Status
	note := func(err error) {
		if first == nil {
			first = err
		}
	}
	defer func() { w.passed(first) }()
	wss, err := w.Store.Workspaces(ctx)
	if err != nil {
		w.Log.Error("watch: workspaces", "err", err)
		note(err)
		return
	}
	w.expireProposals(ctx)
	closedByRepo, linked := w.closedItemLanes(ctx) // once per pass, not per repo
	w.pruneClosedRead(linked)
	for _, ws := range wss {
		repos, err := w.Store.Repos(ctx, ws.ID)
		if err != nil {
			continue
		}
		for i := range repos {
			repos[i] = w.refreshRemote(ctx, ws, repos[i])
			repo := repos[i]
			lanes, err := w.Store.Lanes(ctx, repo.ID)
			closed := closedByRepo[repo.ID]
			if err != nil || (len(lanes) == 0 && len(closed) == 0) {
				continue
			}
			f, err := w.forgeFor(repo)
			if err != nil {
				if !w.skipped[repo.ID] {
					w.skipped[repo.ID] = true
					w.Log.Info("watch: not watching repo", "repo", repo.Name, "why", err)
				}
				w.autoCloseLanes(ctx, lanes)
				continue
			}
			host := hostOf(repo)
			wc := w.conf().Profile(repo.Name).Watch
			for _, lane := range lanes {
				if lane.State != store.LaneOpen {
					continue
				}
				if w.skipHost(host) {
					note(fmt.Errorf("forge host %s is unreachable; skipped while backing off", host))
					break
				}
				err := w.checkLane(ctx, f, lane, wc)
				if err != nil {
					note(err)
				}
				if !w.result(host, err) {
					break
				}
			}
			w.settleClosed(ctx, f, host, repo.Path, closed, note)
			w.autoCloseLanes(ctx, lanes)
		}
		w.checkFollows(ctx, repos)
	}
}

// autoCloseLanes closes the lanes that are finished and idle (autoClose), whether or not
// their forge can be read: a lane with no merge request has nothing to poll.
func (w *Watcher) autoCloseLanes(ctx context.Context, lanes []store.Lane) {
	for _, l := range lanes {
		if cur, err := w.Store.Lane(ctx, l.ID); err != nil || cur.State != store.LaneOpen {
			continue
		}
		w.autoClose(ctx, l)
	}
}

// refreshRemote re-reads a repo's origin URL and saves it when it changed, so a repo
// whose origin was repointed after it was registered is polled at its current host. If
// git cannot say, the saved value stands. It returns the repo as it now is.
func (w *Watcher) refreshRemote(ctx context.Context, ws store.Workspace, repo store.Repo) store.Repo {
	now, err := workspace.Remote(ctx, repo.Path)
	if err != nil || now == "" || now == repo.Remote {
		return repo
	}
	old := repo.Remote
	repo.Remote = now
	if _, err := w.Store.SaveWorkspace(ctx, ws, []store.Repo{repo}); err != nil {
		w.Log.Error("watch: save remote", "repo", repo.Name, "err", err)
		repo.Remote = old
		return repo
	}
	delete(w.skipped, repo.ID)
	w.Log.Info("watch: repo origin changed", "repo", repo.Name, "from", redact.String(old), "to", redact.String(now))
	return repo
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

// checkLane reads a lane's merge request and acts on what changed. It returns the
// forge's error, for the backoff; one from a host that answered is logged here.
func (w *Watcher) checkLane(ctx context.Context, f forge.Forge, lane store.Lane, wc config.Watch) error {
	mr, err := f.MRForBranch(ctx, lane.Branch)
	if err != nil {
		if !forge.Unreachable(err) {
			w.Log.Error("watch: merge request", "lane", lane.Name, "err", err)
		}
		return err
	}
	if mr == nil {
		return nil
	}
	prev, err := w.Store.LaneForge(ctx, lane.ID)
	if err != nil {
		return nil
	}
	next := prev
	next.MR, next.MRState, next.MRURL = mr.IID, mr.State, mr.URL
	next.MRCreated, next.MRUpdated = mr.Created.UTC(), mr.Updated.UTC()
	if mr.IID != prev.MR {
		next.FixTries, next.Pipeline, next.PipelineStatus, next.ThreadsSeen = 0, 0, "", ""
		w.feed(ctx, store.FeedMR, lane.ID, "!%d %s for lane %s: %s", mr.IID, mr.State, lane.Name, mr.URL)
	}

	if p := mr.Pipeline; p != nil && mr.State == "opened" && (p.ID != prev.Pipeline || p.Status != prev.PipelineStatus) {
		next.Pipeline, next.PipelineStatus = p.ID, p.Status
		switch p.Status {
		case "success":
			w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d passed for !%d (lane %s)", p.ID, mr.IID, lane.Name)
		case "failed":
			next.FixTries = w.pipelineFailed(ctx, f, lane, mr, p, next.FixTries, wc.Tries())
		case "canceled":
			w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d was canceled for !%d (lane %s)", p.ID, mr.IID, lane.Name)
		}
	}

	// The pipeline times are the recorded pipeline's own: the forge's head pipeline when it
	// is the one stored, else none, so they never belong to another pipeline.
	next.PipelineStarted, next.PipelineFinished = time.Time{}, time.Time{}
	if p := mr.Pipeline; p != nil && p.ID == next.Pipeline {
		next.PipelineStarted, next.PipelineFinished = p.Started.UTC(), p.Finished.UTC()
	}

	w.approval(ctx, f, lane, mr, prev, &next)
	w.reviewThreads(ctx, f, lane, mr, &next, wc)

	// Act only on a merge seen happening. A request already merged the first time
	// Shepherd looks (an imported lane, a branch reused after its merge) proves nothing
	// about the work in the lane now, so it is reported, never acted on.
	firstSight := prev.MR == 0 || mr.IID != prev.MR
	switch {
	case mr.State == "merged" && firstSight:
		next.MergeSHA = mr.MergeSHA
		w.feed(ctx, store.FeedMR, lane.ID, "!%d for lane %s was already merged when Shepherd first looked; the lane stays open (close it with shepherd lane close when its work is done)", mr.IID, lane.Name)
	case mr.State == "merged" && prev.MRState != "merged":
		next.MergeSHA = mr.MergeSHA
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
	return nil
}

// merged closes the lane on the forge's proof, or says why it cannot.
func (w *Watcher) merged(ctx context.Context, lane store.Lane, mr *forge.MR) {
	if mr.MergeSHA == "" {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d is merged but the forge gives no merge commit yet; lane %s stays open until it does", mr.IID, lane.Name)
		return
	}
	for _, p := range w.Fold.VerifyTags(ctx, lane, mr.MergeSHA) {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d merged, but %s", mr.IID, p)
	}
	res, err := w.Fold.CloseMerged(ctx, lane.ID, mr.MergeSHA, mr.SHA)
	if err != nil {
		w.feed(ctx, store.FeedMR, lane.ID, "!%d merged at %s, but lane %s cannot close: %s", mr.IID, short(mr.MergeSHA), lane.Name, firstLine(err.Error()))
		return
	}
	w.Log.Info("watch: lane closed on merge", "lane", lane.Name, "mr", mr.IID, "merge", mr.MergeSHA)
	if err := w.Fold.WriteView(ctx, lane.RepoID); err != nil {
		w.Log.Error("watch: coordination view", "err", err)
	}
	note := ""
	if res.BranchDeleted {
		note = ", local branch deleted"
	}
	w.feed(ctx, store.FeedLaneClosed, lane.ID, "!%d merged at %s; lane %s closed%s", mr.IID, short(mr.MergeSHA), lane.Name, note)
}

// pipelineFailed hands a failed pipeline to the lane's agent while tries remain, and to
// the person after. It returns the tries used.
func (w *Watcher) pipelineFailed(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, p *forge.Pipeline, tries, max int) int {
	jobs, err := f.FailedJobs(ctx, p.ID)
	if err != nil {
		w.Log.Error("watch: failed jobs", "lane", lane.Name, "err", err)
	}
	var names []string
	var logs strings.Builder
	waits := false
	for _, j := range jobs {
		if j.Name == forge.RequireApprovalJob {
			// Needs a human reviewer; no agent can fix it.
			waits = true
			continue
		}
		names = append(names, j.Name)
		tail, err := f.JobLog(ctx, j.ID, 60)
		if err != nil {
			tail = "(could not read the log: " + err.Error() + ")"
		}
		fmt.Fprintf(&logs, "== job [%s](%s) ==\n%s\n\n", j.Name, j.URL, tail)
	}
	if waits && len(names) == 0 {
		w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d (%s) for !%d (%s) (lane %s) is held by %s: it needs a reviewer, so it is yours", p.ID, p.URL, mr.IID, mr.URL, lane.Name, forge.RequireApprovalJob)
		return tries
	}
	what := strings.Join(names, ", ")
	if what == "" {
		what = "no failed job listed"
	}
	w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline %d (%s) failed for !%d (%s) (lane %s): %s", p.ID, p.URL, mr.IID, mr.URL, lane.Name, what)

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
	case tries >= max:
		w.feed(ctx, store.FeedPipeline, lane.ID, "pipeline failed again after %d fixes by %s; lane %s is yours now", tries, last.Agent, lane.Name)
		return tries
	}
	prompt := fmt.Sprintf("[Shepherd] The pipeline for your merge request [!%d](%s) failed ([pipeline %d](%s)).\n\n%s"+
		"Find the cause and fix it within your scope, run the gate, and commit. Do not push: the person pushes the fix. "+
		"When you report, name the merge request, pipeline and jobs by the clickable links given here.",
		mr.IID, mr.URL, p.ID, p.URL, logs.String())
	// Count the try before starting it, so a fix that ends fast is checked against it.
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
		w.feed(ctx, store.FeedPipeline, lane.ID, "could not hand the failure to %s in lane %s: %s", last.Agent, lane.Name, firstLine(err.Error()))
		return tries
	}
	tries++
	w.feed(ctx, store.FeedPipeline, lane.ID, "asked %s to fix it (run %d, try %d of %d); push its fix when it is done", last.Agent, run.ID, tries, max)
	return tries
}

// conf is the live configuration: the runner's or the fold's, else the defaults.
func (w *Watcher) conf() config.Config {
	switch {
	case w.Runner != nil:
		return w.Runner.Conf()
	case w.Fold != nil:
		return w.Fold.Conf()
	}
	return config.Default()
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
