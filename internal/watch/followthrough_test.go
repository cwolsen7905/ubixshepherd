package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// external makes the fixture's repo followed through by an outside owner, with a clock
// the test moves.
func (f *fixture) external(t *testing.T) *time.Time {
	t.Helper()
	cfg := config.Default()
	cfg.Defaults.Watch.FollowThrough = config.FollowThrough{Owner: config.OwnerExternal, Name: "reviewbot", Patience: "30m", Author: []string{"review-bot"}}
	f.run.SetConfig(cfg)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	f.w.Clock = func() time.Time { return now }
	return &now
}

func (f *fixture) runs(t *testing.T) int {
	t.Helper()
	r, _ := f.st.Runs(f.ctx, f.lane.ID, "", 20)
	return len(r)
}

func TestExternalOwnerHoldsAFailedPipelineThenShepherdTakesOver(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	now := f.external(t)
	f.f.jobs = []forge.Job{{ID: 1, Name: "test", URL: "https://gl/j/1"}}
	f.f.mr.Pipeline = &forge.Pipeline{ID: 100, Status: "failed", URL: "https://gl/p/100"}
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 1 {
		t.Fatalf("Shepherd handed the failure back while the owner had it: %d runs", n)
	}
	if got := f.feed(t); strings.Count(got, "reviewbot follows it through") != 1 {
		t.Errorf("feed = %s", got)
	}
	// Still inside the patience, even from a fresh watcher.
	*now = now.Add(29 * time.Minute)
	f.w.Check(f.ctx)
	if n := f.runs(t); n != 1 {
		t.Fatalf("took over early: %d runs", n)
	}
	*now = now.Add(2 * time.Minute)
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("runs = %d after the patience, want 2", n)
	}
	// Taking over is once per pipeline.
	*now = now.Add(time.Hour)
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("took over twice: %d runs", n)
	}
	lf, _ := f.st.LaneForge(f.ctx, f.lane.ID)
	if got := f.feed(t); lf.FixTries != 1 || !strings.Contains(got, "reviewbot did not act on pipeline 100 within 30m") {
		t.Errorf("tries = %d, feed = %s", lf.FixTries, got)
	}
}

func TestExternalOwnerPushingRestartsTheWait(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	now := f.external(t)
	f.f.mr.Pipeline = &forge.Pipeline{ID: 100, Status: "failed"}
	f.w.Check(f.ctx)
	*now = now.Add(25 * time.Minute)
	f.f.mr.Pipeline = &forge.Pipeline{ID: 101, Status: "running"}
	f.w.Check(f.ctx)
	*now = now.Add(10 * time.Minute)
	f.f.mr.Pipeline = &forge.Pipeline{ID: 102, Status: "failed"}
	f.w.Check(f.ctx)
	*now = now.Add(25 * time.Minute)
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 1 {
		t.Fatalf("took over a new failure early: %d runs", n)
	}
}

func TestExternalOwnerThreadsWaitAndPeopleThreadsDoNot(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	now := f.external(t)
	f.f.threads = []forge.Thread{
		{ID: "d1", Author: "review-bot", Notes: 1, Body: "review-bot: handle nil"},
		{ID: "d2", Author: "sam", Notes: 1, Body: "sam: rename this"},
	}
	f.w.Check(f.ctx)
	f.settle(t)
	runs, _ := f.st.Runs(f.ctx, f.lane.ID, "", 20)
	if len(runs) != 2 || !strings.Contains(runs[0].Prompt, "rename this") || strings.Contains(runs[0].Prompt, "handle nil") {
		t.Fatalf("runs = %+v", runs)
	}
	// The agent answered the person's thread; the owner replies: its wait restarts.
	*now = now.Add(25 * time.Minute)
	f.f.threads[1].Notes = 2
	f.f.threads[0].Notes = 2
	f.w.Check(f.ctx)
	*now = now.Add(25 * time.Minute)
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("took the thread over while the owner was working: %d runs", n)
	}
	// Silent for the patience: Shepherd takes it, once.
	*now = now.Add(10 * time.Minute)
	f.w.Check(f.ctx)
	f.settle(t)
	runs, _ = f.st.Runs(f.ctx, f.lane.ID, "", 20)
	if len(runs) != 3 || !strings.Contains(runs[0].Prompt, "handle nil") {
		t.Fatalf("runs = %d", len(runs))
	}
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 3 {
		t.Fatalf("handed the thread twice: %d runs", n)
	}
}

func TestExternalOwnerSayingUnfixedIsTakenAtOnce(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	f.external(t)
	f.f.threads = []forge.Thread{{ID: "d1", Author: "review-bot", Notes: 1, Body: "review-bot: handle nil"}}
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 1 {
		t.Fatalf("runs = %d", n)
	}
	// A person writing the marker does not count; the owner writing it does.
	f.f.threads[0].Notes = 2
	f.f.threads[0].Body += "\nsam: Unfixed: no"
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 1 {
		t.Fatalf("a person's marker took the thread: %d runs", n)
	}
	f.f.threads[0].Notes = 3
	f.f.threads[0].Body += "\nreview-bot: Unfixed: needs a design call"
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("runs = %d, want 2", n)
	}
	if got := f.feed(t); !strings.Contains(got, "reviewbot could not address") {
		t.Errorf("feed = %s", got)
	}
}

func TestExternalOwnerStateSurvivesARestart(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	now := f.external(t)
	f.f.mr.Pipeline = &forge.Pipeline{ID: 100, Status: "failed"}
	f.w.Check(f.ctx)
	*now = now.Add(31 * time.Minute)
	w2 := &Watcher{Store: f.w.Store, Fold: f.w.Fold, Runner: f.w.Runner, Log: f.w.Log, Interval: f.w.Interval, ForgeFor: f.w.ForgeFor, Clock: f.w.Clock}
	w2.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("runs = %d, want 2", n)
	}
}

func TestExternalOwnerUnfixedOnFirstSightIsTakenAtOnce(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	f.external(t)
	f.f.threads = []forge.Thread{{ID: "d1", Author: "review-bot", Notes: 1, Body: "review-bot: Unfixed: needs a design call"}}
	f.w.Check(f.ctx)
	f.settle(t)
	if n := f.runs(t); n != 2 {
		t.Fatalf("runs = %d, want 2", n)
	}
}
