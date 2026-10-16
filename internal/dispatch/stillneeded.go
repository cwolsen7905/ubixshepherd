package dispatch

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// AlreadyDone starts the summary of a done report from an agent that found its work
// already on the base branch. It is the whole protocol: the agent states a fact, and the
// person decides what it means for the item.
const AlreadyDone = "ALREADY DONE:"

// StillNeeded is the part of the brief for a lane opened for a next-up item: check the
// base first, because planned work goes stale. It returns "" for a lane with no item.
func StillNeeded(it *store.NextItem, base string) string {
	if it == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This lane was opened for next-up item %d: %q.", it.ID, redact.String(it.Title))
	if it.Plan != "" {
		fmt.Fprintf(&b, " Its plan is %s.", it.Plan)
	}
	fmt.Fprintf(&b, "\nBefore you change anything, fetch and check whether this work has already landed on %s (look at the files, the recent commits and the tests). ", base)
	fmt.Fprintf(&b, "If it has, make no commits and report done with a summary that starts with %q and names the evidence (commit ids and paths). Shepherd will ask the person whether to mark the item done: you do not. Otherwise carry on with the task.", AlreadyDone)
	return b.String()
}

// itemOfLane is the open next-up item linked to a lane, or nil.
func (r *Runner) itemOfLane(ctx context.Context, laneID int64) *store.NextItem {
	items, err := r.Store.NextItems(ctx)
	if err != nil {
		return nil
	}
	for i := range items {
		if items[i].LaneID == laneID && items[i].State != store.NextDone && items[i].State != store.NextDropped {
			return &items[i]
		}
	}
	return nil
}

// claimedDone tells the person, on the feed, that an agent found its item's work already
// landed. It changes nothing: marking done is the person's call.
func (r *Runner) claimedDone(ctx context.Context, e store.Event) {
	if e.Status != "done" || !strings.HasPrefix(strings.TrimSpace(e.Text), AlreadyDone) {
		return
	}
	run, err := r.Store.Run(ctx, e.RunID)
	if err != nil {
		return
	}
	it := r.itemOfLane(ctx, run.LaneID)
	if it == nil {
		return
	}
	if n := r.commitsOn(ctx, run.LaneID); n != 0 {
		// An agent that committed work did not find it already done.
		r.Log.Info("already-done report ignored: the lane has commits", "run", e.RunID, "commits", n)
		return
	}
	r.feed(ctx, store.FeedPlan, it.ID, "next-up item %d may already be done: %s. Mark it done? (shepherd next done %d)",
		it.ID, clip(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(e.Text), AlreadyDone)), 200), it.ID)
}

// commitsOn counts the lane's commits beyond its base; -1 when git cannot say, which
// counts as some, so a claim that cannot be checked is not offered.
func (r *Runner) commitsOn(ctx context.Context, laneID int64) int {
	lane, err := r.Store.Lane(ctx, laneID)
	if err != nil {
		return -1
	}
	base := lane.Base
	if repo, err := r.Store.Repo(ctx, lane.RepoID); err == nil && git.RefExists(ctx, repo.Path, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	}
	out, err := git.Run(ctx, lane.Worktree, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return n
}
