package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
