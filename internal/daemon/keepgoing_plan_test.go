package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestPlanVerdict(t *testing.T) {
	lane := store.Lane{Scope: []string{"internal/x/**", "docs/y.md"}}
	item := &store.NextItem{}
	good := store.Decision{
		Question:       "Plan for the cache fix\n\nI will change `internal/x/cache.go` and `docs/y.md`, with tests in `internal/x/cache_test.go`.",
		Recommendation: "Proceed",
	}
	for name, tc := range map[string]struct {
		mut  func(d *store.Decision)
		item *store.NextItem
		ok   bool
	}{
		"good":          {func(d *store.Decision) {}, item, true},
		"approve":       {func(d *store.Decision) { d.Recommendation = "Approve: it is small" }, item, true},
		"not a plan":    {func(d *store.Decision) { d.Question = "Which name?\n`internal/x/a.go`" }, item, false},
		"outside scope": {func(d *store.Decision) { d.Question += " Also `internal/other/z.go`." }, item, false},
		"bare outside":  {func(d *store.Decision) { d.Question += " Also edit cmd/shepherd/main.go." }, item, false},
		"traversal":     {func(d *store.Decision) { d.Question += " And `../secret.go`." }, item, false},
		"no files":      {func(d *store.Decision) { d.Question = "Plan: tidy things up" }, item, false},
		"no item":       {func(d *store.Decision) {}, nil, false},
		"no rec":        {func(d *store.Decision) { d.Recommendation = "" }, item, false},
		"rec says wait": {func(d *store.Decision) { d.Recommendation = "Wait for the person" }, item, false},
		"rec rejects":   {func(d *store.Decision) { d.Recommendation = "Proceed? No, do not" }, item, false},
		"departs":       {func(d *store.Decision) { d.Why = "This deviates from the brief." }, item, false},
		"merge":         {func(d *store.Decision) { d.Question += " Then merge it." }, item, false},
		"push":          {func(d *store.Decision) { d.Question += " Then push the branch." }, item, false},
		"tag":           {func(d *store.Decision) { d.Question += " Then tag v1." }, item, false},
		"release":       {func(d *store.Decision) { d.Options = []string{"cut a release"} }, item, false},
		"money":         {func(d *store.Decision) { d.Question += " It costs $20 a month." }, item, false},
		"widen":         {func(d *store.Decision) { d.Question += " I need to widen the lane." }, item, false},
		"beyond scope":  {func(d *store.Decision) { d.Why = "work beyond the scope" }, item, false},
	} {
		d := good
		tc.mut(&d)
		ok, why := planVerdict(d, lane, tc.item)
		if ok != tc.ok {
			t.Errorf("%s: ok=%v (%s), want %v", name, ok, why, tc.ok)
		}
	}
}

// planFixture is a lane with a scope, serving an approved item, with a run asking.
func planFixture(t *testing.T) (s *Server, ws store.Workspace, lane store.Lane, answered *[]string) {
	t.Helper()
	ctx := context.Background()
	s, _, _, ws, base := keepSetup(t)
	lane, err := s.Store.CreateLane(ctx, store.Lane{RepoID: base.RepoID, Name: "planlane", Branch: "planlane", Base: "main",
		Worktree: filepath.Join(t.TempDir(), "w"), Scope: []string{"internal/x/**"}, State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Item", Repo: "app", State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	id := lane.ID
	if _, err := s.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id}); err != nil {
		t.Fatal(err)
	}
	var got []string
	s.KeepGoing().planAnswer = func(ctx context.Context, did int64, text string) error {
		got = append(got, text)
		_, err := s.Store.AnswerDecision(ctx, did, text)
		return err
	}
	return s, ws, lane, &got
}

func askPlan(t *testing.T, s *Server, lane store.Lane, q, rec string) store.Decision {
	t.Helper()
	run, err := s.Store.CreateRun(context.Background(), store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Store.CreateDecision(context.Background(), store.Decision{RunID: run.ID, Question: q, Recommendation: rec})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const goodPlan = "Plan\nChange `internal/x/a.go` and its test `internal/x/a_test.go`."

func TestPlanAutoApprovedUnderKeepGoing(t *testing.T) {
	s, ws, lane, got := planFixture(t)
	d := askPlan(t, s, lane, goodPlan, "Proceed")
	keepOn(t, s, ws, api.KeepGoingSet{})
	s.KeepGoing().Drive().StartRun = nil
	s.KeepGoing().autoApprovePlans(context.Background(), ws.ID)
	if len(*got) != 1 || !strings.Contains((*got)[0], "keep-going") || !strings.Contains((*got)[0], PlanRule) || !strings.Contains((*got)[0], "not by the person") {
		t.Fatalf("answer: %v", *got)
	}
	after, _ := s.Store.Decision(context.Background(), d.ID)
	if after.State != store.DecisionAnswered {
		t.Errorf("state %s", after.State)
	}
	feed := keepFeed(t, s)
	if len(feed) == 0 || !strings.Contains(feed[len(feed)-1], "auto-approved the plan") || !strings.Contains(feed[len(feed)-1], PlanRule) {
		t.Errorf("feed: %v", feed)
	}
}

func TestPlanStaysForThePerson(t *testing.T) {
	s, ws, lane, got := planFixture(t)
	askPlan(t, s, lane, goodPlan+" Then push it.", "Proceed")
	askPlan(t, s, lane, "Plan\nChange `internal/elsewhere/a.go`.", "Proceed")
	askPlan(t, s, lane, goodPlan, "Wait")
	keepOn(t, s, ws, api.KeepGoingSet{})
	s.KeepGoing().autoApprovePlans(context.Background(), ws.ID)
	if len(*got) != 0 {
		t.Fatalf("answered: %v", *got)
	}
}

func TestPlanNeverWhenKeepGoingOffOrSwitchOff(t *testing.T) {
	s, ws, lane, got := planFixture(t)
	askPlan(t, s, lane, goodPlan, "Proceed")
	// Mode off: EvaluateAll does not sweep.
	s.KeepGoing().EvaluateAll(context.Background())
	if len(*got) != 0 {
		t.Fatalf("answered while off: %v", *got)
	}
	// Mode on, switch off.
	no := false
	cfg := config.Default()
	cfg.KeepGoing.PlanApprove = &no
	s.Config = cfg
	keepOn(t, s, ws, api.KeepGoingSet{})
	s.KeepGoing().autoApprovePlans(context.Background(), ws.ID)
	if len(*got) != 0 {
		t.Fatalf("answered with the switch off: %v", *got)
	}
}

func TestPlanCountsAndRespectsLaneStarts(t *testing.T) {
	s, ws, lane, got := planFixture(t)
	keepOn(t, s, ws, api.KeepGoingSet{})
	day := s.KeepGoing().day(s.KeepGoing().Now())
	n := config.DefaultKeepLaneStarts
	for i := 0; i < n; i++ {
		askPlan(t, s, lane, goodPlan, "Proceed")
	}
	askPlan(t, s, lane, goodPlan, "Proceed")
	s.KeepGoing().autoApprovePlans(context.Background(), ws.ID)
	if len(*got) != n {
		t.Fatalf("answered %d, want the lane's %d starts", len(*got), n)
	}
	if c, _ := s.Store.LaneStarts(context.Background(), lane.ID, day); c != n {
		t.Errorf("starts counted %d", c)
	}
}
