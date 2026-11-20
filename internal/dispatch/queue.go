package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// whyQueuedPrefix starts the Why of a held start that is a follow-up queued behind a run
// in its lane, as against one held for a restart.
const whyQueuedPrefix = "queued behind "

// queueFollowUp writes down a start the person asked for in a lane whose agent is going,
// and returns the error that says so: it wraps ErrQueued (and ErrHeld). The row lives in
// the store's held starts, so the queue survives a daemon restart, and RunHeld starts it,
// oldest first, when a run ends. behind is the run it waits for (0: earlier follow-ups).
func (r *Runner) queueFollowUp(ctx context.Context, asked StartRequest, lane store.Lane, behind int64) error {
	asked.LaneID = lane.ID
	b, err := json.Marshal(asked)
	if err != nil {
		return err
	}
	why := "queued behind earlier follow-ups"
	msg := fmt.Sprintf("queued behind earlier follow-ups in lane %s; it starts when they are done", lane.Name)
	if behind != 0 {
		why = fmt.Sprintf("queued behind run %d", behind)
		msg = fmt.Sprintf("queued behind run %d in lane %s; it continues the session when that run ends", behind, lane.Name)
	}
	if _, err := r.Store.AddHeldStart(ctx, store.HeldStart{Request: string(b), Why: why}); err != nil {
		return err
	}
	// The run may have ended after start looked and before the row was written: its
	// RunHeld pass then found nothing. Look again, and kick the queue if the lane is
	// free. start holds the runner's lock here, so the pass goes on its own.
	if going, err := r.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err == nil && len(going) == 0 {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.RunHeld(context.Background())
		}()
	}
	r.feed(ctx, store.FeedRunStarted, behind, "follow-up queued in lane %s (%s): %s", lane.Name, why, clip(asked.Prompt, 120))
	return refusal{msg: msg, held: true, queued: true}
}

// queuedIn counts the follow-ups waiting in a lane.
func (r *Runner) queuedIn(ctx context.Context, laneID int64) (int, error) {
	held, err := r.Store.HeldStarts(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range held {
		if isFollowUp(h) && heldLane(h) == laneID {
			n++
		}
	}
	return n, nil
}

func isFollowUp(h store.HeldStart) bool { return strings.HasPrefix(h.Why, whyQueuedPrefix) }

func heldLane(h store.HeldStart) int64 {
	var req StartRequest
	if json.Unmarshal([]byte(h.Request), &req) != nil {
		return 0
	}
	return req.LaneID
}

// heldLaneOf is the lane a held start would run in: its LaneID, or for a continue
// (a start held for a restart) the lane of the run it continues. 0 when unknown.
func (r *Runner) heldLaneOf(ctx context.Context, req StartRequest) int64 {
	if req.LaneID != 0 || req.Continue == 0 {
		return req.LaneID
	}
	run, err := r.Store.Run(ctx, req.Continue)
	if err != nil {
		return 0
	}
	return run.LaneID
}

// DropQueued removes the follow-ups waiting in a lane, for a lane that is closing, and
// says so in the feed. It returns how many it dropped. RunHeld also drops a queued
// follow-up whose lane is no longer open, so a lane closed without calling this loses
// its queue at the next run's end.
func (r *Runner) DropQueued(ctx context.Context, laneID int64, why string) int {
	r.heldMu.Lock()
	defer r.heldMu.Unlock()
	held, err := r.Store.HeldStarts(ctx)
	if err != nil {
		r.Log.Error("load held starts", "err", err)
		return 0
	}
	n := 0
	for _, h := range held {
		if !isFollowUp(h) || heldLane(h) != laneID {
			continue
		}
		if err := r.Store.DeleteHeldStart(ctx, h.ID); err != nil {
			r.Log.Error("remove queued follow-up", "id", h.ID, "err", err)
			continue
		}
		n++
	}
	if n > 0 {
		r.feed(ctx, store.FeedRunInterrupted, 0, "%d queued follow-up(s) dropped: %s", n, why)
	}
	return n
}

// modelAtLimit says whether a run on model would pass the model's cap in
// daemon.model_limits, given the runs now going: it returns the cap and whether the model
// is full. A model with no cap, and a run with no resolved model, are never full.
func modelAtLimit(cfg config.Config, model string, running []store.Run) (limit int, full bool) {
	limit = cfg.Daemon.ModelLimits[model]
	if model == "" || limit < 1 {
		return 0, false
	}
	n := 0
	for _, run := range running {
		if run.Model == model {
			n++
		}
	}
	return limit, n >= limit
}

// modelWhyPrefix starts the Why of a start queued on model. It begins with
// whyQueuedPrefix on purpose: such a start is a queued follow-up for isFollowUp, so it
// blocks later starts in its lane (RunHeld's stuck), is dropped when its lane closes
// (DropQueued) and counts in queuedIn.
func modelWhyPrefix(model string) string {
	return whyQueuedPrefix + "model " + model + " ("
}

// queuedOnModel says whether a start is already queued waiting for model's slot.
func (r *Runner) queuedOnModel(ctx context.Context, model string) (bool, error) {
	held, err := r.Store.HeldStarts(ctx)
	if err != nil {
		return false, err
	}
	for _, h := range held {
		if strings.HasPrefix(h.Why, modelWhyPrefix(model)) {
			return true, nil
		}
	}
	return false, nil
}

// queueForModel writes down a start the person asked for on a model that is at its cap.
// Like a follow-up it lives in the held starts, so it survives a restart and RunHeld
// starts it, oldest first, when a run ends. start holds the runner's lock here.
func (r *Runner) queueForModel(ctx context.Context, asked StartRequest, lane store.Lane, model string, limit int) error {
	asked.LaneID = lane.ID
	b, err := json.Marshal(asked)
	if err != nil {
		return err
	}
	why := modelWhyPrefix(model) + fmt.Sprintf("at its limit of %d)", limit)
	if _, err := r.Store.AddHeldStart(ctx, store.HeldStart{Request: string(b), Why: why}); err != nil {
		return err
	}
	// A run on the model may have ended after start counted and before the row was
	// written; kick the queue so that is not missed.
	if running, err := r.Store.Runs(ctx, 0, store.RunRunning, 1000); err == nil {
		if _, full := modelAtLimit(r.Conf(), model, running); !full {
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.RunHeld(context.Background())
			}()
		}
	}
	r.feed(ctx, store.FeedRunStarted, 0, "run queued in lane %s: model %s is at its limit of %d: %s", lane.Name, model, limit, clip(asked.Prompt, 120))
	return refusal{msg: fmt.Sprintf("queued: model %s is at its limit of %d; it starts when a run on that model ends", model, limit), held: true, queued: true}
}
