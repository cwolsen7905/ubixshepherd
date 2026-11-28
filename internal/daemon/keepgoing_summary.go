package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// SinceYouLeft is the one summary for the person coming back: what keep-going did since
// the mode was last turned on, in the workspace. It lists the items started, the runs
// finished and failed, the merge requests opened or merged, and why and when the mode
// stopped (or that it is still on). It is empty when the mode was never turned on.
func (k *KeepGoing) SinceYouLeft(ctx context.Context, ws int64) (string, error) {
	st := k.s.db(ctx)
	row, err := st.KeepGoing(ctx, ws)
	if err != nil {
		return "", err
	}
	if row.Since.IsZero() {
		return "", nil
	}
	repos, err := st.Repos(ctx, ws)
	if err != nil {
		return "", err
	}
	items, err := st.NextItems(ctx)
	if err != nil {
		return "", err
	}
	byLane := map[int64][]store.NextItem{} // the items that claimed each lane
	for _, it := range items {
		if it.LaneID != 0 {
			byLane[it.LaneID] = append(byLane[it.LaneID], it)
		}
	}
	// Runs are newest first across every workspace: widen the window until it reaches
	// back past the time keep-going was turned on, so this workspace's runs are not cut
	// off by another's. At the cap the summary says it is partial.
	var runs []store.Run
	truncated := false
	for limit := runWindow; ; limit *= 2 {
		if runs, err = st.Runs(ctx, 0, "", limit); err != nil {
			return "", err
		}
		if len(runs) < limit || runs[len(runs)-1].Started.Before(row.Since) {
			break
		}
		if limit >= runWindowMax {
			truncated = true
			break
		}
	}
	forge := map[int64]*store.LaneForge{} // read once per lane, nil for none
	var started, finished, failed, mrs []string
	seenLane, seenMR := map[int64]bool{}, map[int64]bool{}
	// Lanes are read once per distinct lane in the window, by id: Lanes by repo leaves
	// out closed lanes, and a lane closed by its merge still did the work.
	lanes := map[int64]store.Lane{}
	skip := map[int64]bool{}
	var mine []store.Run
	for _, r := range runs {
		if r.Started.Before(row.Since) {
			break // older runs are from before keep-going was turned on
		}
		if skip[r.LaneID] {
			continue
		}
		if _, ok := lanes[r.LaneID]; !ok {
			l, err := st.Lane(ctx, r.LaneID)
			if err != nil || !inRepos(repos, l.RepoID) {
				skip[r.LaneID] = true
				continue
			}
			lanes[r.LaneID] = l
		}
		mine = append(mine, r)
	}
	for i := len(mine) - 1; i >= 0; i-- { // oldest first
		r := mine[i]
		lane := lanes[r.LaneID]
		what := runWhat(lane, itemFor(byLane[lane.ID], r), r)
		if !seenLane[lane.ID] {
			seenLane[lane.ID] = true
			started = append(started, what)
		}
		switch r.State {
		case store.RunSucceeded:
			finished = append(finished, fmt.Sprintf("%s (run %d)", what, r.ID))
		case store.RunFailed, store.RunInterrupted:
			why := r.Error
			if why == "" {
				why = r.State
			}
			failed = append(failed, fmt.Sprintf("%s: run %d %s (%s)", what, r.ID, r.State, firstLine(why)))
		}
		lf, ok := forge[lane.ID]
		if !ok {
			forge[lane.ID] = nil
			if f, err := st.LaneForge(ctx, lane.ID); err == nil {
				forge[lane.ID] = &f
			}
			lf = forge[lane.ID]
		}
		if lf != nil && lf.MR != 0 && !seenMR[lane.ID] {
			seenMR[lane.ID] = true
			mrs = append(mrs, fmt.Sprintf("!%d %s (%s)", lf.MR, what, lf.MRState))
		}
	}
	var b strings.Builder
	when := row.Since.Local().Format("Mon 15:04")
	if row.On {
		fmt.Fprintf(&b, "Since keep-going was turned on at %s (still on):\n", when)
	} else {
		fmt.Fprintf(&b, "Since keep-going was turned on at %s:\n", when)
	}
	list := func(label string, xs []string) {
		fmt.Fprintf(&b, "  %s: %d\n", label, len(xs))
		for _, x := range xs {
			fmt.Fprintf(&b, "    - %s\n", x)
		}
	}
	list("started", started)
	list("finished", finished)
	list("failed", failed)
	list("merge requests opened or merged", mrs)
	if truncated {
		fmt.Fprintf(&b, "  only the newest %d runs in all workspaces were read: earlier ones are not listed\n", runWindowMax)
	}
	if row.On {
		b.WriteString("  it is still on\n")
	} else if row.ReasonOff != "" {
		fmt.Fprintf(&b, "  it went off at %s: %s\n", row.OffAt.Local().Format("Mon 15:04"), offText(row.ReasonOff))
	}
	return redact.String(b.String()), nil
}

// itemFor picks the Next-up item a run was for out of those that claimed its lane: one
// whose life (created to done) holds the run's start, the latest created if several, else
// the latest created that existed by then, else none (a run from before every item).
func itemFor(items []store.NextItem, r store.Run) *store.NextItem {
	var live, early *store.NextItem
	for i := range items {
		it := &items[i]
		if it.Created.After(r.Started) {
			continue
		}
		if early == nil || it.Created.After(early.Created) {
			early = it
		}
		if (it.DoneAt == nil || !it.DoneAt.Before(r.Started)) && (live == nil || it.Created.After(live.Created)) {
			live = it
		}
	}
	if live != nil {
		return live
	}
	if early != nil {
		return early
	}
	// Every item came after the run: the run was not for any of them.
	return nil
}

// runWhat names a run by its item (if any), its lane and the agent that ran it.
func runWhat(l store.Lane, it *store.NextItem, r store.Run) string {
	s := "lane " + l.Name
	if it != nil {
		s = fmt.Sprintf("item %d %q on lane %s", it.ID, it.Title, l.Name)
	}
	if r.Agent != "" {
		s += " by " + r.Agent
	}
	return s
}

// runWindow is how many runs the summary reads first, runWindowMax the most it reads.
const (
	runWindow    = 1000
	runWindowMax = 16000
)

func inRepos(repos []store.Repo, id int64) bool {
	for _, r := range repos {
		if r.ID == id {
			return true
		}
	}
	return false
}
