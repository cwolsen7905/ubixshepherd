// Package waiting collects what needs the person's action into one list: open decisions,
// lane merge requests held on their approval, lanes with work to ship, and Next-up items
// proposed to them. It reads the store and the lanes' git state; nothing is stored for
// it, so the list is as fresh as what it reads.
package waiting

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// heldJob lists the require-approval job statuses that hold a merge: not started, waiting
// to be played, blocked, or failed for want of a sign-off.
var heldJob = map[string]bool{"pending": true, "manual": true, "blocked": true, "failed": true, "created": true, "scheduled": true}

// Collect reads the list. workspaceID 0 means every workspace; Next-up items belong to no
// workspace and are always included. It stops at the first store error.
func Collect(ctx context.Context, st store.Store, conf config.Config, workspaceID int64) (api.Waiting, error) {
	out := api.Waiting{Items: []api.WaitingItem{}}
	repos, err := allRepos(ctx, st, workspaceID)
	if err != nil {
		return out, err
	}
	laneOf := map[int64]store.Lane{}
	repoOf := map[int64]store.Repo{}
	for _, rp := range repos {
		lanes, err := st.Lanes(ctx, rp.ID)
		if err != nil {
			return out, err
		}
		for _, l := range lanes {
			laneOf[l.ID] = l
			repoOf[l.ID] = rp
		}
	}

	// Decisions: ours through the run's lane. One whose run or lane cannot be found is
	// kept when no workspace was asked for.
	ds, err := st.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		return out, err
	}
	for _, d := range ds {
		it := api.WaitingItem{
			Kind: api.WaitingDecision, Action: "answer", DecisionID: d.ID,
			Title: oneLine(d.Question), Since: d.Created,
		}
		run, err := st.Run(ctx, d.RunID)
		if err == nil {
			if l, ok := laneOf[run.LaneID]; ok {
				it.Lane, it.LaneID, it.Repo = l.Name, l.ID, repoOf[l.ID].Name
			} else if workspaceID != 0 {
				continue
			}
		} else if workspaceID != 0 {
			continue
		}
		it.Detail = fmt.Sprintf("decision %d: %s", d.ID, it.Title)
		out.Items = append(out.Items, it)
		out.Counts.Decisions++
	}

	laneIDs := make([]int64, 0, len(laneOf))
	for id := range laneOf {
		laneIDs = append(laneIDs, id)
	}
	sort.Slice(laneIDs, func(i, j int) bool { return laneIDs[i] < laneIDs[j] })
	for _, id := range laneIDs {
		l, rp := laneOf[id], repoOf[id]
		if l.State != store.LaneOpen {
			continue
		}
		running := false
		if busy, err := st.Runs(ctx, l.ID, store.RunRunning, 1); err == nil && len(busy) > 0 {
			running = true
		}
		lf, _ := st.LaneForge(ctx, l.ID)
		if it, ok := mrItem(l, rp, lf, running); ok {
			out.Items = append(out.Items, it)
			out.Counts.MRs++
		}
		if running {
			continue
		}
		if it, ok := shipItem(ctx, conf, l, rp); ok {
			out.Items = append(out.Items, it)
			out.Counts.Ship++
		}
	}

	items, err := st.NextItems(ctx)
	if err != nil {
		return out, err
	}
	for _, it := range items {
		if it.Source == store.NextByPerson || it.State != store.NextPlanned {
			continue
		}
		// A linked lane that is open makes the item started: nothing waits on it.
		if it.LaneID != 0 {
			if l, err := st.Lane(ctx, it.LaneID); err == nil && (l.State == store.LaneOpen || l.State == store.LaneOpening) {
				continue
			}
		}
		out.Items = append(out.Items, api.WaitingItem{
			Kind: api.WaitingNext, Action: "review", Repo: it.Repo, NextID: it.ID,
			Title: oneLine(it.Title), Detail: fmt.Sprintf("proposed by the %s: %s", it.Source, oneLine(it.Title)),
			Since: it.Created,
		})
		out.Counts.Next++
	}

	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Since.Before(out.Items[j].Since) })
	out.Count = len(out.Items)
	return out, nil
}

// mrItem is a lane's open merge request that only the person can move: its pipeline is
// held (or failed) at the require-approval job with no approval, or it failed elsewhere
// and no run is working on it. The first is approved from the web page; the second is
// only shown.
func mrItem(l store.Lane, rp store.Repo, lf store.LaneForge, running bool) (api.WaitingItem, bool) {
	if lf.MR == 0 || lf.MRState == "merged" || lf.MRState == "closed" {
		return api.WaitingItem{}, false
	}
	it := api.WaitingItem{Kind: api.WaitingMR, Repo: rp.Name, Lane: l.Name, LaneID: l.ID, MR: lf.MR}
	switch {
	case !lf.Approved && heldJob[lf.RequireApproval] && (lf.PipelineStatus != "failed" || lf.RequireApproval == "failed"):
		it.Action = "approve"
		it.Title = fmt.Sprintf("approve MR !%d, then retry its approval job", lf.MR)
		it.Detail = "approve it, then retry the require-approval job"
		it.Since = firstTime(lf.PipelineFinished, lf.PipelineStarted, lf.MRUpdated, l.Created)
	case lf.PipelineStatus == "failed" && !running:
		it.Action = "open"
		it.Title = fmt.Sprintf("MR !%d: pipeline failed", lf.MR)
		it.Detail = "pipeline failed"
		it.Since = firstTime(lf.PipelineFinished, lf.PipelineStarted, lf.MRUpdated, l.Created)
	default:
		return api.WaitingItem{}, false
	}
	return it, true
}

// shipItem is an open lane with commits not on its branch at origin, in a repo whose
// push is not the agent's. With push: shepherd the person's word ships it; with
// push: human they push it themselves, and the item only says so.
func shipItem(ctx context.Context, conf config.Config, l store.Lane, rp store.Repo) (api.WaitingItem, bool) {
	push := conf.Profile(rp.Name).Autonomy.Push
	if push != config.Shepherd && push != config.Human {
		return api.WaitingItem{}, false
	}
	n := dispatch.Unpushed(ctx, l)
	if n == 0 {
		return api.WaitingItem{}, false
	}
	it := api.WaitingItem{
		Kind: api.WaitingShip, Repo: rp.Name, Lane: l.Name, LaneID: l.ID,
		Since: l.Created,
	}
	plural := "s"
	if n == 1 {
		plural = ""
	}
	if push == config.Shepherd {
		it.Action = "ship"
		it.Title = fmt.Sprintf("ship %d commit%s", n, plural)
		it.Detail = fmt.Sprintf("%d commit%s not pushed; ship it", n, plural)
	} else {
		it.Action = "open"
		it.Title = fmt.Sprintf("push %d commit%s", n, plural)
		it.Detail = fmt.Sprintf("%d commit%s not pushed; push it yourself", n, plural)
	}
	return it, true
}

func allRepos(ctx context.Context, st store.Store, workspaceID int64) ([]store.Repo, error) {
	var wss []store.Workspace
	if workspaceID != 0 {
		wss = []store.Workspace{{ID: workspaceID}}
	} else {
		var err error
		if wss, err = st.Workspaces(ctx); err != nil {
			return nil, err
		}
	}
	var out []store.Repo
	for _, w := range wss {
		rs, err := st.Repos(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:159]) + "…"
	}
	return s
}
