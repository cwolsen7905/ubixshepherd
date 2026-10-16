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
	approvedItem(t, s, "Ship the thing")
	if _, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: "which db?", State: store.QuestionOpen}); err != nil {
		t.Fatal(err)
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
	approvedItem(t, s, "App item")
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
