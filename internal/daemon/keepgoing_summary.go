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
	st := k.s.Store
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
	titleOf := map[int64]string{} // lane id to the item it works on
	for _, it := range items {
		if it.LaneID != 0 {
			titleOf[it.LaneID] = fmt.Sprintf("item %d %q", it.ID, it.Title)
		}
	}
	runs, err := st.Runs(ctx, 0, "", 1000)
	if err != nil {
		return "", err
	}
	var started, finished, failed, mrs []string
	seenLane, seenMR := map[int64]bool{}, map[int64]bool{}
	for i := len(runs) - 1; i >= 0; i-- { // oldest first
		r := runs[i]
		if r.Started.Before(row.Since) {
			continue
		}
		lane, err := st.Lane(ctx, r.LaneID)
		if err != nil || !inRepos(repos, lane.RepoID) {
			continue
		}
		what := titleOf[lane.ID]
		if what == "" {
			what = "lane " + lane.Name
		}
		if !seenLane[lane.ID] {
			seenLane[lane.ID] = true
			started = append(started, what+" on lane "+lane.Name)
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
		if lf, err := st.LaneForge(ctx, lane.ID); err == nil && lf.MR != 0 && !seenMR[lane.ID] {
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
	if row.On {
		b.WriteString("  it is still on\n")
	} else if row.ReasonOff != "" {
		fmt.Fprintf(&b, "  it went off at %s: %s\n", row.OffAt.Local().Format("Mon 15:04"), offText(row.ReasonOff))
	}
	return redact.String(b.String()), nil
}

func inRepos(repos []store.Repo, id int64) bool {
	for _, r := range repos {
		if r.ID == id {
			return true
		}
	}
	return false
}
