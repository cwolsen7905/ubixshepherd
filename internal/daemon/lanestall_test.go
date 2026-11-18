package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func stallFeed(t *testing.T, s *Server) []string {
	t.Helper()
	items, err := s.Store.Feed(context.Background(), 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		if it.Kind == FeedLaneStall {
			out = append(out, it.Text)
		}
	}
	return out
}

func newLaneStall(t *testing.T) (*Server, *LaneStall, *fakeClock, store.Lane, *int) {
	t.Helper()
	s, _ := newServer(t)
	_, lane := seedLane(t, s, "stuck")
	clk := &fakeClock{t: time.Now().UTC()}
	n := new(int)
	ls := NewLaneStall(s, LaneStallOptions{Now: clk.Now, Unpushed: func(context.Context, store.Lane) int { return *n }})
	return s, ls, clk, lane, n
}

func TestLaneStallUnpushedAlertsOnceAfterThreshold(t *testing.T) {
	s, ls, clk, _, n := newLaneStall(t)
	ctx := context.Background()
	*n = 2
	ls.Check(ctx)
	clk.Add(3 * time.Hour)
	ls.Check(ctx)
	if got := stallFeed(t, s); len(got) != 0 {
		t.Fatalf("alerted inside the threshold: %v", got)
	}
	clk.Add(2 * time.Hour)
	ls.Check(ctx)
	ls.Check(ctx)
	clk.Add(time.Hour)
	ls.Check(ctx)
	got := stallFeed(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "2 commit(s) are not pushed") || !strings.Contains(got[0], "stuck") {
		t.Fatalf("want one alert naming the lane and the commits: %v", got)
	}
}

func TestLaneStallFailedPipeline(t *testing.T) {
	s, ls, clk, lane, _ := newLaneStall(t)
	ctx := context.Background()
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: lane.ID, MR: 7, MRState: "opened", PipelineStatus: "failed"}); err != nil {
		t.Fatal(err)
	}
	clk.Add(5 * time.Hour)
	ls.Check(ctx)
	got := stallFeed(t, s)
	if len(got) != 1 || !strings.Contains(got[0], "pipeline for MR !7 failed") {
		t.Fatalf("got %v", got)
	}
}

func TestLaneStallNothingHoldsIt(t *testing.T) {
	s, ls, clk, lane, _ := newLaneStall(t)
	ctx := context.Background()
	// A passing pipeline and no unpushed work is not a stall.
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: lane.ID, MR: 7, PipelineStatus: "success"}); err != nil {
		t.Fatal(err)
	}
	clk.Add(10 * time.Hour)
	ls.Check(ctx)
	if got := stallFeed(t, s); len(got) != 0 {
		t.Fatalf("alerted with nothing held: %v", got)
	}
}

func TestLaneStallClearsWhenConditionGoesAndRearms(t *testing.T) {
	s, ls, clk, _, n := newLaneStall(t)
	ctx := context.Background()
	*n = 1
	clk.Add(5 * time.Hour)
	ls.Check(ctx)
	*n = 0 // pushed
	ls.Check(ctx)
	*n = 1 // new unpushed work, still no run
	ls.Check(ctx)
	if got := stallFeed(t, s); len(got) != 2 {
		t.Fatalf("a new stall after the old one cleared must alert again: %v", got)
	}
}

func TestLaneStallRunClearsAndRestartsClock(t *testing.T) {
	s, ls, clk, lane, n := newLaneStall(t)
	ctx := context.Background()
	*n = 1
	clk.Add(5 * time.Hour)
	ls.Check(ctx)
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "x", State: store.RunRunning, Log: filepath.Join(t.TempDir(), "l"), Started: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	clk.Add(10 * time.Hour)
	ls.Check(ctx) // running: never stalled
	end := clk.Now()
	run.State, run.Ended = store.RunSucceeded, &end
	if err := s.Store.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	clk.Add(time.Hour)
	ls.Check(ctx) // the quiet time counts from the run's end
	if got := stallFeed(t, s); len(got) != 1 {
		t.Fatalf("only the first stall alerts yet: %v", got)
	}
	clk.Add(4 * time.Hour)
	ls.Check(ctx)
	if got := stallFeed(t, s); len(got) != 2 {
		t.Fatalf("the stall after the run alerts again: %v", got)
	}
}

func TestLaneStallIgnoresClosedLanesAndWiresInWithOneCall(t *testing.T) {
	s, _ := newServer(t)
	_, lane := seedLane(t, s, "gone")
	clk := &fakeClock{t: time.Now().UTC()}
	s.SetLaneStallOptions(LaneStallOptions{Now: clk.Now, Unpushed: func(context.Context, store.Lane) int { return 3 }})
	ctx := context.Background()
	if err := s.Store.SetLaneState(ctx, lane.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	clk.Add(24 * time.Hour)
	s.CheckLaneStalls(ctx)
	if got := stallFeed(t, s); len(got) != 0 {
		t.Fatalf("closed lane alerted: %v", got)
	}
}

func TestLaneStallSetOptionsKeepsWhatWasAnnounced(t *testing.T) {
	s, _ := newServer(t)
	seedLane(t, s, "kept")
	clk := &fakeClock{t: time.Now().UTC()}
	opts := LaneStallOptions{After: time.Hour, Now: clk.Now, Unpushed: func(context.Context, store.Lane) int { return 1 }}
	s.SetLaneStallOptions(opts)
	ctx := context.Background()
	clk.Add(2 * time.Hour)
	s.CheckLaneStalls(ctx)
	s.SetLaneStallOptions(opts)
	s.CheckLaneStalls(ctx)
	if got := stallFeed(t, s); len(got) != 1 {
		t.Fatalf("one stall, one alert, even across an options change: %v", got)
	}
}

func TestLaneStallFailedPipelineOfClosedOrMergedMRIsNotAStall(t *testing.T) {
	for _, lf := range []store.LaneForge{
		{MR: 7, MRState: "closed", PipelineStatus: "failed"},
		{MR: 7, MRState: "merged", PipelineStatus: "failed"},
		{MR: 7, MRState: "opened", PipelineStatus: "failed", MergeSHA: "abc"},
	} {
		s, ls, clk, lane, _ := newLaneStall(t)
		lf.LaneID = lane.ID
		if err := s.Store.PutLaneForge(context.Background(), lf); err != nil {
			t.Fatal(err)
		}
		clk.Add(5 * time.Hour)
		ls.Check(context.Background())
		if got := stallFeed(t, s); len(got) != 0 {
			t.Fatalf("%+v alerted: %v", lf, got)
		}
	}
}
