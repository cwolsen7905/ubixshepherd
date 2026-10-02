package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// MaxGateTries is how many gate failures Shepherd hands back to the agent before a push,
// before leaving the lane to the person.
const MaxGateTries = 2

// GateTimeout bounds one gate run.
const GateTimeout = 20 * time.Minute

// ship pushes a lane and opens its merge request after an agent's run, for repos whose
// profile says Shepherd pushes. Shepherd runs the gate itself first, in the lane; the
// agent's word that it passed is not enough. It never merges.
func (r *Runner) ship(ctx context.Context, run store.Run, lane store.Lane) {
	if run.State != store.RunSucceeded || run.Commits == 0 {
		return
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return
	}
	prof := r.Config.Profile(repo.Name)
	if prof.Autonomy.Push != config.Shepherd {
		return
	}
	if len(run.Outside) > 0 {
		r.feed(ctx, store.FeedPipeline, lane.ID, "not pushing lane %s: run %d changed files outside its scope (%s)", lane.Name, run.ID, strings.Join(run.Outside, ", "))
		return
	}
	if why := r.unfinished(ctx, run, lane); why != "" {
		r.Log.Info("not shipping yet", "lane", lane.Name, "run", run.ID, "why", why)
		return
	}
	lf, err := r.Store.LaneForge(ctx, lane.ID)
	if err != nil {
		return
	}

	ok, tail, logPath := r.gate(ctx, run, lane, prof.Gate)
	if !ok {
		r.gateFailed(ctx, run, lane, prof.Gate, tail, logPath, &lf)
		return
	}
	lf.GateTries = 0
	r.Store.PutLaneForge(ctx, lf)

	if _, err := git.Run(ctx, lane.Worktree, "push", "--quiet", "-u", "origin", lane.Branch); err != nil {
		r.feed(ctx, store.FeedPipeline, lane.ID, "gate passed in lane %s, but the push failed: %s", lane.Name, clip(err.Error(), 300))
		return
	}
	f, err := r.forgeFor(repo.Remote)
	if err != nil {
		r.feed(ctx, store.FeedPipeline, lane.ID, "pushed lane %s (gate passed); open the merge request yourself: %v", lane.Name, err)
		return
	}
	mr, err := f.MRForBranch(ctx, lane.Branch)
	if err != nil {
		r.feed(ctx, store.FeedPipeline, lane.ID, "pushed lane %s (gate passed); could not look up its merge request: %s", lane.Name, clip(err.Error(), 200))
		return
	}
	if mr != nil && mr.State == "opened" {
		r.feed(ctx, store.FeedMR, lane.ID, "pushed lane %s (gate `%s` passed); !%d updated: %s", lane.Name, prof.Gate, mr.IID, mr.URL)
		return
	}
	title, body := r.mrText(ctx, run, lane, prof.Gate)
	mr, err = f.CreateMR(ctx, lane.Branch, lane.Base, title, body)
	if err != nil {
		r.feed(ctx, store.FeedPipeline, lane.ID, "pushed lane %s (gate passed), but opening the merge request failed: %s", lane.Name, clip(err.Error(), 300))
		return
	}
	r.Log.Info("lane shipped", "lane", lane.Name, "mr", mr.IID)
	r.feed(ctx, store.FeedMR, lane.ID, "pushed lane %s (gate `%s` passed) and opened !%d for you to review: %s", lane.Name, prof.Gate, mr.IID, mr.URL)
}

// unfinished says why a run's work is not ready to push, or "": it is waiting on the
// person or another lane, it said it was blocked, or the lane has moved on to another run.
func (r *Runner) unfinished(ctx context.Context, run store.Run, lane store.Lane) string {
	if busy, _ := r.Store.Runs(ctx, lane.ID, store.RunRunning, 1); len(busy) > 0 {
		return "another run is going in the lane"
	}
	if ds, err := r.Store.Decisions(ctx, store.DecisionOpen); err == nil {
		for _, d := range ds {
			if d.RunID == run.ID {
				return "it is waiting on decision " + fmt.Sprint(d.ID)
			}
		}
	}
	if qs, err := r.Store.Requests(ctx, store.RequestPending, store.RequestNeedsRouting, store.RequestRouted, store.RequestReplyReady); err == nil {
		for _, q := range qs {
			if q.FromRun == run.ID {
				return "it is waiting on request " + fmt.Sprint(q.ID)
			}
		}
	}
	if events, err := r.Store.Events(ctx, run.ID); err == nil {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Kind == EventReport && events[i].Status != "progress" {
				if events[i].Status == "blocked" {
					return "it reported blocked"
				}
				break
			}
		}
	}
	return ""
}

func (r *Runner) forgeFor(remote string) (forge.Forge, error) {
	if r.ForgeFor != nil {
		return r.ForgeFor(remote)
	}
	return forge.For(remote)
}

// gate runs the repo's gate in the lane's worktree. It reports whether it passed, the
// end of its output, and where the whole output is.
func (r *Runner) gate(ctx context.Context, run store.Run, lane store.Lane, gate string) (bool, string, string) {
	ctx, cancel := context.WithTimeout(ctx, GateTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", gate)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", gate)
	}
	cmd.Dir = lane.Worktree
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	r.feed(ctx, store.FeedPipeline, lane.ID, "running the gate `%s` in lane %s before pushing", gate, lane.Name)
	err := cmd.Run()
	text := redact.String(out.String())
	logPath := filepath.Join(r.Dir, fmt.Sprintf("%d.gate.log", run.ID))
	os.WriteFile(logPath, []byte(text), 0o600)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	tail := strings.Join(lines, "\n")
	if ctx.Err() != nil {
		return false, tail + "\n(the gate timed out after " + GateTimeout.String() + ")", logPath
	}
	return err == nil, tail, logPath
}

// gateFailed hands the gate's failure back to the agent while tries remain.
func (r *Runner) gateFailed(ctx context.Context, run store.Run, lane store.Lane, gate, tail, logPath string, lf *store.LaneForge) {
	if lf.GateTries >= MaxGateTries {
		r.feed(ctx, store.FeedPipeline, lane.ID, "the gate `%s` failed again in lane %s after %d fixes; not pushing, it is yours (%s)", gate, lane.Name, lf.GateTries, logPath)
		return
	}
	if run.Session == "" {
		r.feed(ctx, store.FeedPipeline, lane.ID, "the gate `%s` failed in lane %s; not pushing (%s)", gate, lane.Name, logPath)
		return
	}
	prompt := fmt.Sprintf("[Shepherd] Before pushing your work, Shepherd ran the repo's gate, `%s`, in your lane, and it failed:\n\n%s\n\n"+
		"Fix the cause within your scope, run the gate yourself until it passes, and commit. Shepherd runs it again when you finish.", gate, tail)
	next, err := r.Start(ctx, StartRequest{Continue: run.ID, Prompt: prompt})
	if err != nil {
		r.feed(ctx, store.FeedPipeline, lane.ID, "the gate failed in lane %s, and handing it back failed: %s", lane.Name, clip(err.Error(), 200))
		return
	}
	lf.GateTries++
	r.Store.PutLaneForge(ctx, *lf)
	r.feed(ctx, store.FeedPipeline, lane.ID, "the gate `%s` failed in lane %s; asked %s to fix it (run %d, try %d of %d)", gate, lane.Name, run.Agent, next.ID, lf.GateTries, MaxGateTries)
}

// mrText is the merge request's title and description, from the lane's commits.
func (r *Runner) mrText(ctx context.Context, run store.Run, lane store.Lane, gate string) (string, string) {
	base := lane.Base
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	}
	log, _ := git.Run(ctx, lane.Worktree, "log", "--reverse", "--format=%s", base+"..HEAD")
	subjects := strings.Split(strings.TrimSpace(log), "\n")
	title := lane.Name
	if len(subjects) > 0 && subjects[0] != "" {
		title = subjects[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Opened by uBixShepherd from lane `%s`, after %s's run %d. The gate, `%s`, passed in the lane before the push.\n\n", lane.Name, run.Agent, run.ID, gate)
	b.WriteString("Commits:\n")
	for _, s := range subjects {
		if s != "" {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	fmt.Fprintf(&b, "\nScope: `%s`. Shepherd does not merge; that is yours.\n", strings.Join(lane.Scope, "`, `"))
	return title, b.String()
}
