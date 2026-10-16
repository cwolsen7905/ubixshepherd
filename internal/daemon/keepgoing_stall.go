package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
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
// Next-up items wait with nothing able to start them (docs/design/keep-going-stall-alert.md).
// It fires once per stall: the state resets only when the workspace stops stalling.
func (k *KeepGoing) checkStall(ctx context.Context, row store.KeepGoing) {
	ws := row.WorkspaceID
	k.stallMu.Lock()
	defer k.stallMu.Unlock()
	reset := func() { delete(k.stalls, ws) }
	if !row.On {
		reset()
		return
	}
	n, err := k.running(ctx, ws)
	if err != nil || n > 0 {
		if err == nil {
			reset()
		}
		return
	}
	lines, err := k.stallLines(ctx, ws)
	if err != nil {
		k.s.Log.Warn("keep-going: stall check", "workspace", ws, "err", err)
		return
	}
	if len(lines) == 0 {
		reset()
		return
	}
	st := k.stalls[ws]
	if st == nil {
		st = &stallState{since: k.Now()}
		k.stalls[ws] = st
	}
	if st.told || k.Now().Sub(st.since) < stallGrace {
		return
	}
	st.told = true
	k.s.postFeed(ctx, FeedKeepGoing, 0, "keep-going is stalled: nothing is running and nothing can start. "+strings.Join(lines, " "))
}

// stallLines is one line per approved, unstarted item in the workspace, saying what
// blocks it and what the person must do; empty when there is no such item.
func (k *KeepGoing) stallLines(ctx context.Context, ws int64) ([]string, error) {
	s := k.s
	items, err := s.Store.NextItems(ctx)
	if err != nil {
		return nil, err
	}
	repos, err := s.Store.Repos(ctx, ws)
	if err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	for _, r := range repos {
		mine[r.Name] = true
	}
	// known is every repo in any workspace: an item for one of another workspace's
	// repos is not this workspace's stall, but one for a repo in none is.
	known := map[string]bool{}
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range wss {
		rs, err := s.Store.Repos(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			known[r.Name] = true
		}
	}
	var approved []api.NextItemView
	for _, it := range items {
		v := s.nextView(ctx, it)
		if v.State == store.NextApproved && v.LaneID == 0 && (v.Repo == "" || mine[v.Repo] || !known[v.Repo]) {
			approved = append(approved, v)
		}
	}
	if len(approved) == 0 {
		return nil, nil
	}
	w, err := waiting.Collect(ctx, s.Store, s.LiveConfig(), ws)
	if err != nil {
		return nil, err
	}
	qs, err := s.Store.Questions(ctx, ws, store.QuestionOpen)
	if err != nil {
		return nil, err
	}
	w.AddQuestions(qs)
	var everyone []string // blockers that hold every item
	for _, it := range w.Items {
		if it.Kind == api.WaitingQuestion {
			everyone = append(everyone, fmt.Sprintf("question %d is waiting on you: reply to it", it.QuestionID))
		}
	}
	if s.QuotaHolds != nil {
		for _, h := range s.QuotaHolds() {
			everyone = append(everyone, fmt.Sprintf("%s is out of quota until %s: wait, or use another agent", h.Agent, h.Until.Local().Format("Jan 2 15:04")))
		}
	}
	sort.SliceStable(approved, func(i, j int) bool { return approved[i].Rank < approved[j].Rank })
	var lines []string
	for i, v := range approved {
		if i == stallItems {
			lines = append(lines, fmt.Sprintf("(and %d more approved items)", len(approved)-stallItems))
			break
		}
		reasons := append([]string(nil), everyone...)
		if v.Repo != "" && !mine[v.Repo] {
			reasons = append(reasons, "repo "+v.Repo+" is not registered: register it")
		}
		for _, it := range w.Items {
			if it.Repo == v.Repo && it.Kind != api.WaitingQuestion && it.Kind != api.WaitingNext {
				reasons = append(reasons, stallReason(it))
			}
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "nothing known blocks it: ask the desk to start it")
		}
		if len(reasons) > stallReasons {
			reasons = append(reasons[:stallReasons], fmt.Sprintf("and %d more", len(reasons)-stallReasons))
		}
		lines = append(lines, fmt.Sprintf("Item %d %q: %s.", v.ID, v.Title, strings.Join(reasons, "; ")))
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
