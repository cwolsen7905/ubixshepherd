package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func stallAlerts(t *testing.T, s *Server) []string {
	t.Helper()
	var out []string
	for _, l := range keepFeed(t, s) {
		if strings.Contains(l, "is stalled") {
			out = append(out, l)
		}
	}
	return out
}

func approvedItem(t *testing.T, s *Server, title string) store.NextItem {
	t.Helper()
	it, err := s.Store.AddNextItem(context.Background(), store.NextItem{Title: title, Repo: "app", State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestStallAlertFiresOncePerStall(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	kg := s.KeepGoing()
	keepOn(t, s, ws, api.KeepGoingSet{})
	// Nothing approved: no stall.
	kg.EvaluateAll(ctx)
	clk.Add(5 * time.Minute)
	kg.EvaluateAll(ctx)
	if got := stallAlerts(t, s); len(got) != 0 {
		t.Fatalf("no approved item, yet alerted: %v", got)
	}
	ship := approvedItem(t, s, "Ship the thing")
	q, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: "which db?", State: store.QuestionOpen})
	if err != nil {
		t.Fatal(err)
	}
	// Only a question linked to the item holds it.
	kg.Drive().LinkOf = func(x store.Question) QuestionLink {
		if x.ID == q.ID {
			return QuestionLink{ItemID: ship.ID}
		}
		return QuestionLink{}
	}
	kg.EvaluateAll(ctx) // first sight: inside the grace period
	if got := stallAlerts(t, s); len(got) != 0 {
		t.Fatalf("alerted inside the grace: %v", got)
	}
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	got := stallAlerts(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "Ship the thing") || !strings.Contains(got[0], "question") {
		t.Fatalf("want one alert naming the item and the question: %v", got)
	}
	for i := 0; i < 3; i++ {
		clk.Add(time.Minute)
		kg.EvaluateAll(ctx)
	}
	if got := stallAlerts(t, s); len(got) != 1 {
		t.Fatalf("debounce: fired again within one stall: %v", got)
	}
	// A run starts: the stall ends. When it ends and nothing moves, a new stall alerts.
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunRunning, Started: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	kg.EvaluateAll(ctx)
	run.State = store.RunSucceeded
	if err := s.Store.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	kg.EvaluateAll(ctx)
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	if got := stallAlerts(t, s); len(got) != 2 {
		t.Fatalf("a new stall should alert once more: %v", got)
	}
}

func TestStallAlertSaysNothingKnownAndSilentWhenOff(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	kg := s.KeepGoing()
	approvedItem(t, s, "Quiet item")
	kg.EvaluateAll(ctx)
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	if got := stallAlerts(t, s); len(got) != 0 {
		t.Fatalf("keep-going is off: %v", got)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	kg.EvaluateAll(ctx)
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	got := stallAlerts(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "nothing known blocks it") {
		t.Fatalf("want the nothing-known line: %v", got)
	}
}

func TestStallAlertNamesAnUnregisteredRepo(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	kg := s.KeepGoing()
	if _, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Orphan", Repo: "nowhere", State: store.NextApproved, Source: store.NextByPerson}); err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	kg.EvaluateAll(ctx)
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	got := stallAlerts(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "repo nowhere is not registered") {
		t.Fatalf("want the register-it reason: %v", got)
	}
}

func TestStallAlertKeepsToItsWorkspaceAndGroupsBlockersPerRepo(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	kg := s.KeepGoing()
	// A second workspace with its own repo: its approved item is not this stall's.
	root := t.TempDir()
	if _, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "other", Path: root},
		[]store.Repo{{Name: "elsewhere", Path: filepath.Join(root, "elsewhere")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Other workspace item", Repo: "elsewhere", State: store.NextApproved, Source: store.NextByPerson}); err != nil {
		t.Fatal(err)
	}
	app := approvedItem(t, s, "App item")
	// The item's lane is the one the decision is open on, so the decision holds it.
	if _, err := s.Store.UpdateNextItem(ctx, app.ID, store.NextPatch{LaneID: &lane.ID}); err != nil {
		t.Fatal(err)
	}
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunSucceeded, Started: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "which db?", State: store.DecisionOpen})
	if err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	kg.EvaluateAll(ctx)
	clk.Add(stallGrace)
	kg.EvaluateAll(ctx)
	got := stallAlerts(t, s)
	if len(got) != 1 {
		t.Fatalf("want one alert: %v", got)
	}
	if strings.Contains(got[0], "Other workspace item") {
		t.Errorf("another workspace's item is in this stall: %s", got[0])
	}
	if want := fmt.Sprintf("decision %d is held on lane %s", d.ID, lane.Name); !strings.Contains(got[0], "App item") || !strings.Contains(got[0], want) {
		t.Errorf("want the app item with %q: %s", want, got[0])
	}
}

func TestStallReasonSaysWhatEachKindNeeds(t *testing.T) {
	for _, c := range []struct {
		it   api.WaitingItem
		want string
	}{
		{api.WaitingItem{Kind: api.WaitingDecision, DecisionID: 7, Lane: "a"}, "decision 7 is held on lane a: answer it"},
		{api.WaitingItem{Kind: api.WaitingBlocked, Lane: "a"}, "lane a is blocked: open it and unblock it"},
		{api.WaitingItem{Kind: api.WaitingScope, Lane: "a"}, "a scope widening on lane a is not applied: look at it"},
		{api.WaitingItem{Kind: api.WaitingRequest, RequestID: 3}, "request 3 needs routing: route it"},
		{api.WaitingItem{Kind: api.WaitingMR, MR: 12, Lane: "a"}, "MR !12 on lane a waits on you: approve or fix it"},
		{api.WaitingItem{Kind: api.WaitingShip, Lane: "a"}, "lane a has unpushed work: ship it"},
		{api.WaitingItem{Kind: api.WaitingArtifact, Lane: "a"}, "lane a holds files nobody decided on: decide them"},
		{api.WaitingItem{Kind: "other", Title: "something"}, "something"},
	} {
		if got := stallReason(c.it); got != c.want {
			t.Errorf("%s: got %q, want %q", c.it.Kind, got, c.want)
		}
	}
}
