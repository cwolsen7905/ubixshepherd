package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// stallGrace is how long a workspace must stay stalled before the person is told, so
// the gap between one run ending and the desk starting the next never alerts.
const stallGrace = 2 * time.Minute

// Caps keep one feed line readable.
const (
	stallItems   = 5
	stallReasons = 3
)

// stallState is what the debounce remembers about one workspace.
type stallState struct {
	since time.Time
	told  bool
}

// checkStall posts one feed item when keep-going is on, nothing runs and approved
// Next-up items wait with nothing able to start them.
// It fires once per stall: the state resets only when the workspace stops stalling.
func (k *KeepGoing) checkStall(ctx context.Context, row store.KeepGoing) {
	ws := row.WorkspaceID
	// The store reads happen outside the lock; it guards only the debounce map.
	var lines []string
	stalled := false
	if row.On {
		n, err := k.running(ctx, ws)
		if err != nil {
			return
		}
		if n == 0 {
			if lines, err = k.stallLines(ctx, ws); err != nil {
				k.s.Log.Warn("keep-going: stall check", "workspace", ws, "err", err)
				return
			}
			stalled = len(lines) > 0
		}
	}
	k.stallMu.Lock()
	if !stalled {
		delete(k.stalls, ws)
		k.stallMu.Unlock()
		return
	}
	st := k.stalls[ws]
	if st == nil {
		st = &stallState{since: k.Now()}
		k.stalls[ws] = st
	}
	post := !st.told && k.Now().Sub(st.since) >= stallGrace
	if post {
		st.told = true
	}
	k.stallMu.Unlock()
	if post {
		k.s.postFeed(ctx, FeedKeepGoing, 0, redact.String("keep-going is stalled: nothing is running and nothing can start. "+strings.Join(lines, " ")))
	}
}

// stallLines is one line per approved item the drive could not start, saying what blocks
// it and what the person must do; empty when there is no such item. A blocker holds only
// the item it belongs to.
func (k *KeepGoing) stallLines(ctx context.Context, ws int64) ([]string, error) {
	items, err := k.assess(ctx, ws)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	var everyone []string // blockers that hold every item
	if k.s.QuotaHolds != nil {
		for _, h := range k.s.QuotaHolds() {
			everyone = append(everyone, fmt.Sprintf("%s is out of quota until %s: wait, or use another agent", h.Agent, h.Until.Local().Format("Jan 2 15:04")))
		}
	}
	var lines []string
	for i, d := range items {
		if i == stallItems {
			lines = append(lines, fmt.Sprintf("(and %d more approved items)", len(items)-stallItems))
			break
		}
		reasons := append(append([]string(nil), everyone...), d.reasons...)
		if why := k.refusedWhy(d.item.ID); why != "" {
			reasons = append(reasons, "keep-going could not start it: "+why)
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "nothing known blocks it: ask the desk to start it")
		}
		if len(reasons) > stallReasons {
			reasons = append(reasons[:stallReasons], fmt.Sprintf("and %d more", len(reasons)-stallReasons))
		}
		lines = append(lines, fmt.Sprintf("Item %d %q: %s.", d.item.ID, d.item.Title, strings.Join(reasons, "; ")))
	}
	return lines, nil
}

// stallReason says what a waiting item blocks and what the person does about it.
func stallReason(it api.WaitingItem) string {
	on := ""
	if it.Lane != "" {
		on = " on lane " + it.Lane
	}
	switch it.Kind {
	case api.WaitingDecision:
		return fmt.Sprintf("decision %d is held%s: answer it", it.DecisionID, on)
	case api.WaitingBlocked:
		return "lane " + it.Lane + " is blocked: open it and unblock it"
	case api.WaitingScope:
		return "a scope widening" + on + " is not applied: look at it"
	case api.WaitingRequest:
		return fmt.Sprintf("request %d needs routing: route it", it.RequestID)
	case api.WaitingMR:
		return fmt.Sprintf("MR !%d%s waits on you: approve or fix it", it.MR, on)
	case api.WaitingShip:
		return "lane " + it.Lane + " has unpushed work: ship it"
	case api.WaitingArtifact:
		return "lane " + it.Lane + " holds files nobody decided on: decide them"
	}
	return it.Title
}
