package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// fakeClock is a clock tests move by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// keepSetup is a server with a workspace, a lane, and a clock at 09:00 UTC on a day.
func keepSetup(t *testing.T) (*Server, *httptest.Server, *fakeClock, store.Workspace, store.Lane) {
	t.Helper()
	s, ts := newServer(t)
	ws, lane := seedLane(t, s, "kg")
	clk := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s.KeepGoing().Now = clk.Now
	return s, ts, clk, ws, lane
}

func keepOn(t *testing.T, s *Server, ws store.Workspace, set api.KeepGoingSet) {
	t.Helper()
	if _, err := s.KeepGoing().Enable(context.Background(), ws.ID, store.KeepByCLI, set); err != nil {
		t.Fatal(err)
	}
}

func keepRow(t *testing.T, s *Server, ws store.Workspace) store.KeepGoing {
	t.Helper()
	row, err := s.Store.KeepGoing(context.Background(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func addSpend(t *testing.T, s *Server, day string, usd float64) {
	t.Helper()
	if err := s.Store.AddSpend(context.Background(), store.Spend{Day: day, Source: "run", USD: usd}); err != nil {
		t.Fatal(err)
	}
}

func keepFeed(t *testing.T, s *Server) []string {
	t.Helper()
	items, err := s.Store.Feed(context.Background(), 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		if it.Kind == FeedKeepGoing {
			out = append(out, it.Text)
		}
	}
	return out
}

func TestKeepGoingRefusesWhenOff(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	d := s.KeepGoing().Allow(context.Background(), ws.ID, KeepStartItem, lane.ID)
	if d.OK || !strings.Contains(d.Reason, "off") {
		t.Fatalf("off must refuse: %+v", d)
	}
	if d := s.KeepGoing().Allow(context.Background(), ws.ID, "bogus", 0); d.OK {
		t.Fatalf("an unknown action must be refused: %+v", d)
	}
}

func TestKeepGoingSpendCapTurnsItOff(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	addSpend(t, s, "2026-10-11", 10) // before it was turned on: not the mode's
	keepOn(t, s, ws, api.KeepGoingSet{Cap: 2})
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepStartItem, lane.ID); !d.OK {
		t.Fatalf("under the cap: %+v", d)
	}
	addSpend(t, s, "2026-10-11", 1.5)
	clk.Add(time.Minute)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("$1.50 of $2 must leave it on")
	}
	st, _ := s.KeepGoing().Status(ctx, ws.ID)
	if st.SpentUSD != 1.5 || st.CapUSD != 2 {
		t.Errorf("status: spent %v of %v", st.SpentUSD, st.CapUSD)
	}
	addSpend(t, s, "2026-10-11", 0.5)
	d := s.KeepGoing().Allow(ctx, ws.ID, KeepContinue, lane.ID)
	row := keepRow(t, s, ws)
	if d.OK || row.On || row.ReasonOff != store.KeepOffCap || row.By != store.KeepByLimit {
		t.Fatalf("at the cap: %+v, row %+v", d, row)
	}
	if f := keepFeed(t, s); len(f) != 3 || !strings.Contains(f[2], "spend cap") {
		t.Errorf("feed: %q", f)
	}
}

func TestKeepGoingBudgetTurnsItOff(t *testing.T) {
	s, _, _, ws, _ := keepSetup(t)
	ctx := context.Background()
	cfg := s.LiveConfig()
	b := 20.0
	cfg.Daemon.Budget = &b
	s.applyConfig(cfg)
	keepOn(t, s, ws, api.KeepGoingSet{Cap: 100})
	addSpend(t, s, "2026-10-11", 19)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("under the budget")
	}
	addSpend(t, s, "2026-10-11", 1)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); row.On || row.ReasonOff != store.KeepOffBudget {
		t.Fatalf("crossing the budget: %+v", row)
	}
	// A soft budget only warns.
	cfg.Daemon.BudgetCap = "soft"
	s.applyConfig(cfg)
	keepOn(t, s, ws, api.KeepGoingSet{Cap: 100})
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatalf("a soft budget must not turn it off: %+v", row)
	}
}

func TestKeepGoingIdleAndDayEnd(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{})
	clk.Add(3 * time.Hour)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("3h idle is under 4h")
	}
	// An autonomous action restarts the idle time.
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepStartItem, lane.ID); !d.OK {
		t.Fatalf("%+v", d)
	}
	clk.Add(3 * time.Hour) // 15:00, 3h after the action
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("the action must restart idle time")
	}
	// A message from the person does too.
	clk.Add(2 * time.Hour)
	s.KeepGoing().Touch(ctx, ws.ID)
	clk.Add(3*time.Hour + 30*time.Minute) // 20:30
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("the person's message must restart idle time")
	}
	clk.Add(4 * time.Hour) // 00:30 next day: idle and past midnight; the day's end is named first
	row, _ := s.KeepGoing().Evaluate(ctx, ws.ID)
	if row.On || row.ReasonOff != store.KeepOffDayEnd {
		t.Fatalf("past midnight: %+v", row)
	}
	// Idle alone.
	clk.Add(-4 * time.Hour)
	keepOn(t, s, ws, api.KeepGoingSet{IdleHours: 1})
	clk.Add(61 * time.Minute)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); row.On || row.ReasonOff != store.KeepOffIdle {
		t.Fatalf("idle: %+v", row)
	}
}

func TestKeepGoingConfiguredDayEnd(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	cfg := s.LiveConfig()
	cfg.KeepGoing.DayEnd = "17:00"
	cfg.KeepGoing.IdleHours = 24
	s.applyConfig(cfg)
	keepOn(t, s, ws, api.KeepGoingSet{})
	clk.Add(7 * time.Hour) // 16:00
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); !row.On {
		t.Fatal("before day_end")
	}
	clk.Add(time.Hour)
	if row, _ := s.KeepGoing().Evaluate(ctx, ws.ID); row.On || row.ReasonOff != store.KeepOffDayEnd {
		t.Fatalf("at day_end: %+v", row)
	}
}

func TestKeepGoingConcurrentRuns(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 1})
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepStartItem, 0); !d.OK {
		t.Fatalf("no runs yet: %+v", d)
	}
	if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunRunning, Started: s.KeepGoing().Now()}); err != nil {
		t.Fatal(err)
	}
	d := s.KeepGoing().Allow(ctx, ws.ID, KeepRerun, 0)
	if d.OK || !strings.Contains(d.Reason, "1 of 1 runs") {
		t.Fatalf("at the run limit: %+v", d)
	}
	// Refused, not off: and a scope change starts no run.
	if !keepRow(t, s, ws).On {
		t.Error("a refused start must not turn it off")
	}
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepScope, lane.ID); !d.OK {
		t.Errorf("scope widening starts no run: %+v", d)
	}
}

func TestKeepGoingLoopGuard(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{})
	for i := 1; i <= 3; i++ {
		if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID); !d.OK {
			t.Fatalf("start %d: %+v", i, d)
		}
	}
	for i := 0; i < 2; i++ {
		d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID)
		if d.OK || !strings.Contains(d.Reason, "handed back") {
			t.Fatalf("fourth start: %+v", d)
		}
	}
	var told int
	for _, f := range keepFeed(t, s) {
		if strings.Contains(f, "handed lane") {
			told++
		}
	}
	if told != 1 {
		t.Errorf("the person is told once, got %d", told)
	}
	// Another lane is not affected, and the next day starts again.
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID+1000); !d.OK {
		t.Errorf("another lane: %+v", d)
	}
	cfg := s.LiveConfig()
	cfg.KeepGoing.DayEnd = "23:59"
	cfg.KeepGoing.IdleHours = 48
	s.applyConfig(cfg)
	clk.Add(20 * time.Hour) // 05:00 the next day: the session lasts until day_end, which passed
	keepOn(t, s, ws, api.KeepGoingSet{})
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID); !d.OK {
		t.Errorf("a new day: %+v", d)
	}
}

func TestKeepGoingSurvivesRestart(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{})
	// A new daemon on the same store: still on, and its first look re-checks the limits.
	s2, err := NewServer(s.Store, s.LiveConfig(), "/cfg", s.Log)
	if err != nil {
		t.Fatal(err)
	}
	clk.Add(30 * time.Minute)
	s2.KeepGoing().Now = clk.Now
	s2.KeepGoing().EvaluateAll(ctx)
	if !keepRow(t, s, ws).On {
		t.Fatal("a restart must keep it on")
	}
	// And one that comes back after the day ended turns it off at once.
	clk.Add(20 * time.Hour)
	s3, _ := NewServer(s.Store, s.LiveConfig(), "/cfg", s.Log)
	s3.KeepGoing().Now = clk.Now
	s3.KeepGoing().EvaluateAll(ctx)
	if row := keepRow(t, s, ws); row.On || row.ReasonOff != store.KeepOffDayEnd {
		t.Fatalf("after a restart past the day's end: %+v", row)
	}
	if f := keepFeed(t, s); len(f) < 2 || !strings.Contains(f[len(f)-1], "day ended") {
		t.Errorf("feed: %q", f)
	}
}

func TestKeepGoingRunStartedWhileDown(t *testing.T) {
	// Handler start runs the check before the desk's first turn.
	s, _, clk, ws, _ := keepSetup(t)
	keepOn(t, s, ws, api.KeepGoingSet{})
	clk.Add(30 * time.Hour)
	s2, err := NewServer(s.Store, s.LiveConfig(), "/cfg", s.Log)
	if err != nil {
		t.Fatal(err)
	}
	s2.KeepGoing().Now = clk.Now
	s2.DeskAgent = nopDesk{}
	_ = s2.Handler()
	defer s2.Close()
	if row := keepRow(t, s, ws); row.On {
		t.Fatalf("starting the daemon must re-check: %+v", row)
	}
}

func TestKeepGoingEndpointOriginRule(t *testing.T) {
	s, ts, _, ws, lane := keepSetup(t)
	human, _ := s.MintDesk(true, "sess")
	system, _ := s.MintDesk(false, "sess")
	worker, _ := s.MintWorker(99)
	on := api.KeepGoingSet{WorkspaceID: ws.ID, On: true}
	off := api.KeepGoingSet{WorkspaceID: ws.ID}
	var st api.KeepGoingStatus

	for name, tok := range map[string]string{"system desk": system, "worker": worker} {
		if code := call(t, ts, tok, "POST", api.PathKeepGoing, on, nil); code != http.StatusForbidden {
			t.Errorf("%s turning it on: %d, want 403", name, code)
		}
	}
	if keepRow(t, s, ws).On {
		t.Fatal("a refused turn-on must change nothing")
	}
	if code := call(t, ts, human, "POST", api.PathKeepGoing, on, &st); code != http.StatusOK || !st.On || st.By != store.KeepByDesk {
		t.Fatalf("desk in a person's turn: %d %+v", code, st)
	}
	// Off is always allowed, even for a system turn; an agent's token has no route at all.
	if code := call(t, ts, system, "POST", api.PathKeepGoing, off, &st); code != http.StatusOK || st.On || st.ReasonOff != store.KeepOffPerson {
		t.Fatalf("system desk turning it off: %d %+v", code, st)
	}
	if code := call(t, ts, worker, "GET", api.PathKeepGoing+"?workspace_id=1", nil, nil); code != http.StatusForbidden {
		t.Errorf("worker read: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathKeepGoing, api.KeepGoingSet{WorkspaceID: ws.ID, On: true, Cap: 3, MaxRuns: 1, IdleHours: 2}, &st); code != http.StatusOK || !st.On || st.By != store.KeepByCLI || st.CapUSD != 3 || st.MaxRuns != 1 || st.IdleHours != 2 {
		t.Fatalf("operator: %d %+v", code, st)
	}
	if code := call(t, ts, system, "GET", api.PathKeepGoing, nil, &st); code != http.StatusOK || !st.On {
		t.Errorf("desk reads: %d %+v", code, st)
	}
	if code := call(t, ts, s.Token, "POST", api.PathKeepGoing, api.KeepGoingSet{WorkspaceID: ws.ID, On: true, Cap: -1}, nil); code != http.StatusBadRequest {
		t.Errorf("negative cap: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathKeepGoing, api.KeepGoingSet{WorkspaceID: 4242}, nil); code != http.StatusNotFound {
		t.Errorf("unknown workspace: %d", code)
	}
	_ = lane
}

func TestKeepGoingStatusCounters(t *testing.T) {
	s, ts, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{})
	if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunRunning, Started: clk.Now()}); err != nil {
		t.Fatal(err)
	}
	var st api.KeepGoingStatus
	call(t, ts, s.Token, "GET", api.PathKeepGoing, nil, &st)
	if !st.On || st.RunsNow != 1 || st.MaxRuns != 2 || st.CapUSD != 5 || st.IdleHours != 4 || st.LaneStarts != 3 {
		t.Fatalf("status: %+v", st)
	}
	if want := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC); !st.DayEnd.Equal(want) {
		t.Errorf("day end %v, want %v", st.DayEnd, want)
	}
}

// A reload (SIGHUP, or the config editor's save) changes the limits Allow reads at once,
// and one that does not validate keeps the old ones.
func TestKeepGoingConfigReload(t *testing.T) {
	s, path := reloadServer(t)
	ctx := context.Background()
	ws, lane := seedLane(t, s, "kg")
	clk := &fakeClock{t: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s.KeepGoing().Now = clk.Now
	keepOn(t, s, ws, api.KeepGoingSet{})
	if err := os.WriteFile(path, []byte("keep_going:\n  lane_starts: 1\n  cap: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadConfig(ctx); err != nil {
		t.Fatal(err)
	}
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID); !d.OK {
		t.Fatalf("first start: %+v", d)
	}
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID); d.OK {
		t.Fatalf("lane_starts: 1 must refuse a second: %+v", d)
	}
	if st, _ := s.KeepGoing().Status(ctx, ws.ID); st.CapUSD != 9 {
		t.Errorf("cap after reload: %v", st.CapUSD)
	}
	os.WriteFile(path, []byte("keep_going:\n  cap: -3\n"), 0o600)
	if err := s.ReloadConfig(ctx); err == nil {
		t.Fatal("a negative cap reloaded")
	}
	if st, _ := s.KeepGoing().Status(ctx, ws.ID); st.CapUSD != 9 {
		t.Errorf("a refused reload changed the cap: %v", st.CapUSD)
	}
}

func TestKeepGoingActionsReachFeedAndStatus(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	ctx := context.Background()
	keepOn(t, s, ws, api.KeepGoingSet{})
	if d := s.KeepGoing().Allow(ctx, ws.ID, KeepFix, lane.ID); !d.OK {
		t.Fatalf("allow: %+v", d)
	}
	f := keepFeed(t, s)
	if len(f) != 2 || !strings.Contains(f[1], "followed up a failed pipeline") {
		t.Fatalf("feed: %q", f)
	}
	st, err := s.KeepGoing().Status(ctx, ws.ID)
	if err != nil || len(st.Recent) != 2 || st.Recent[1].Lane != lane.ID {
		t.Fatalf("recent: %+v, %v", st.Recent, err)
	}
}
