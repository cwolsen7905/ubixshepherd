package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// RuleRestartDrain names the hold a drain puts on starts, in the style of Crook's rule ids.
const RuleRestartDrain = "builtin/restart-drain"

// DrainReason is what a start held for a restart is told.
const DrainReason = "Shepherd is restarting"

// ResumePrompt is what a run a restart interrupted is told when it is continued.
const ResumePrompt = "Shepherd restarted while you were working. Carry on where you left off; check `git status` first."

// DrainGrace is how long a drained daemon waits to be stopped before it gives up on the
// restart and lets starts go again (the command that asked for it died, say).
var DrainGrace = 2 * time.Minute

// drainStopWait is how long, after the runs are told to stop, before a drain says it is
// still waiting for them to end (it does not give up: the daemon is not drained, and so
// not stopped, while a run is being recorded).
const drainStopWait = 30 * time.Second

type drainState struct {
	until time.Time
	// drained: no run is going and none can start.
	drained bool
}

// Drain is a restart's wait as Draining reports it.
type Drain struct {
	// Runs are the agents still going; Until when the drain stops them.
	Runs  int
	Until time.Time
	// Drained: nothing is going, and the daemon can be stopped.
	Drained bool
}

// Drain begins draining for a restart: from now no run starts (a start asked for is
// kept, and goes after the restart), and the agents going are given until the timeout to
// finish. At the timeout those still going are stopped; each ends interrupted with the
// reason restart, its session kept, and the next start continues it. Drain returns at
// once; Draining says how it stands. A drain already under way is left as it is.
func (r *Runner) Drain(timeout time.Duration) {
	r.mu.Lock()
	if r.drain != nil {
		r.mu.Unlock()
		return
	}
	d := &drainState{until: time.Now().Add(timeout)}
	r.drain = d
	r.mu.Unlock()
	r.Log.Info("draining for a restart", "runs", r.Active(), "timeout", timeout)
	go r.runDrain(d, timeout)
}

func (r *Runner) runDrain(d *drainState, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
wait:
	for r.Active() > 0 {
		select {
		case <-timer.C:
			r.stopForRestart()
			break wait
		case <-tick.C:
		}
	}
	// The runs' outcomes, and what each one's end sets off, are recorded before the
	// daemon may be stopped.
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(drainStopWait):
		r.Log.Warn("runs are still ending; the drain waits for them", "runs", r.Active())
		<-done
	}
	r.mu.Lock()
	if r.drain == d {
		d.drained = true
	}
	r.mu.Unlock()
	r.Log.Info("drained; waiting to be stopped")
	time.Sleep(DrainGrace)
	r.mu.Lock()
	still := r.drain == d && !r.closing
	r.mu.Unlock()
	if still {
		r.Log.Warn("the restart did not come; starting runs again")
		r.CancelDrain(context.Background())
	}
}

// stopForRestart asks every running agent to end, as a restart's, not as the person's
// stop or a crash.
func (r *Runner) stopForRestart() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, p := range r.procs {
		p.restarting = true
		r.Log.Info("stopping run for a restart", "run", id)
		r.terminateThenKill(id, p)
	}
}

// Draining reports a drain under way, nil when there is none.
func (r *Runner) Draining() *Drain {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drain == nil {
		return nil
	}
	return &Drain{Runs: len(r.procs), Until: r.drain.until, Drained: r.drain.drained}
}

// CancelDrain lets starts go again, and starts what was held.
func (r *Runner) CancelDrain(ctx context.Context) {
	r.mu.Lock()
	r.drain = nil
	r.mu.Unlock()
	r.RunHeld(ctx)
}

// holdForRestart is the answer to a start made while draining. A start the person
// asked for is written down and goes after the restart; one Shepherd began on its own is
// only held, since whatever began it asks again.
func (r *Runner) holdForRestart(ctx context.Context, asked StartRequest, lane store.Lane, fromQueue bool) error {
	hold := refusal{msg: DrainReason, held: true, rule: RuleRestartDrain}
	if fromQueue || asked.Auto {
		return hold
	}
	b, err := json.Marshal(asked)
	if err != nil {
		return err
	}
	if _, err := r.Store.AddHeldStart(ctx, store.HeldStart{Request: string(b), Why: DrainReason}); err != nil {
		return err
	}
	r.feed(ctx, store.FeedRunStarted, 0, "start held in lane %s: %s; it starts after the restart: %s", lane.Name, DrainReason, clip(asked.Prompt, 120))
	hold.msg = DrainReason + "; the run starts after the restart"
	hold.queued = true
	return hold
}

// ResumeRestarted queues the continuation of each run a restart interrupted that has
// not been continued yet, then starts what is held. Call it at start, after Recover. A
// run that a crash interrupted is not touched.
func (r *Runner) ResumeRestarted(ctx context.Context) error {
	runs, err := r.Store.Runs(ctx, 0, store.RunInterrupted, 1000)
	if err != nil {
		return err
	}
	held, err := r.Store.HeldStarts(ctx)
	if err != nil {
		return err
	}
	queued := map[int64]bool{}
	for _, h := range held {
		var req StartRequest
		if json.Unmarshal([]byte(h.Request), &req) == nil && req.Continue != 0 {
			queued[req.Continue] = true
		}
	}
	for _, run := range runs {
		if run.Reason != store.ReasonRestart || queued[run.ID] {
			continue
		}
		followed, err := r.continued(ctx, run)
		if err != nil {
			return err
		}
		if followed {
			continue
		}
		b, _ := json.Marshal(StartRequest{Continue: run.ID, Prompt: ResumePrompt})
		if _, err := r.Store.AddHeldStart(ctx, store.HeldStart{Request: string(b), Why: "continue after a restart", Auto: true}); err != nil {
			return err
		}
		r.feed(ctx, store.FeedRunStarted, run.ID, "run %d: Shepherd restarted while it was working; it will be continued in the same session", run.ID)
	}
	r.RunHeld(ctx)
	return nil
}

// continued says another run in the lane already carries on from run.
func (r *Runner) continued(ctx context.Context, run store.Run) (bool, error) {
	later, err := r.Store.Runs(ctx, run.LaneID, "", 200)
	if err != nil {
		return false, err
	}
	for _, l := range later {
		if l.Parent == run.ID {
			return true, nil
		}
	}
	return false, nil
}

// dropHeld removes a held start's row. A row that cannot be removed is remembered as
// done, so the start is not made a second time; the row goes at the next try or the next
// start of the daemon.
func (r *Runner) dropHeld(ctx context.Context, id int64) {
	if err := r.Store.DeleteHeldStart(ctx, id); err != nil {
		r.Log.Error("remove held start", "id", id, "err", err)
		if r.consumed == nil {
			r.consumed = map[int64]bool{}
		}
		r.consumed[id] = true
		return
	}
	delete(r.consumed, id)
}

// RunHeld starts what was held, in the order it was asked for. A start that is held
// again (a run cap, the daily budget) stays for the next try; one refused for good is
// dropped and said so. It does nothing while draining.
func (r *Runner) RunHeld(ctx context.Context) {
	r.heldMu.Lock()
	defer r.heldMu.Unlock()
	r.mu.Lock()
	idle := r.drain == nil && !r.closing
	r.mu.Unlock()
	if !idle {
		return
	}
	held, err := r.Store.HeldStarts(ctx)
	if err != nil {
		r.Log.Error("load held starts", "err", err)
		return
	}
	for _, h := range held {
		var req StartRequest
		if err := json.Unmarshal([]byte(h.Request), &req); err != nil {
			r.Log.Error("drop unreadable held start", "id", h.ID, "err", err)
			r.Store.DeleteHeldStart(ctx, h.ID)
			continue
		}
		if r.consumed[h.ID] {
			// Started already; only its row would not go (see below).
			r.dropHeld(ctx, h.ID)
			continue
		}
		req.Auto = h.Auto
		run, err := r.start(ctx, req, true)
		switch {
		case err == nil:
			r.dropHeld(ctx, h.ID)
			r.feed(ctx, store.FeedRunStarted, run.ID, "run %d: started after the restart (%s)", run.ID, h.Why)
		case errors.Is(err, ErrHeld):
			r.Log.Info("held start waits", "id", h.ID, "why", err)
		case errors.Is(err, ErrRefused):
			r.Store.DeleteHeldStart(ctx, h.ID)
			if req.Continue != 0 {
				r.Store.SetRunReason(ctx, req.Continue, store.ReasonRestartDropped)
			}
			r.feed(ctx, store.FeedRunInterrupted, req.Continue, "a start held for the restart was dropped: %s", firstLine(fmt.Sprint(err)))
		default:
			r.Log.Error("start held run", "id", h.ID, "err", err)
		}
	}
}
