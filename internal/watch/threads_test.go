package watch

import (
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

func (f *fixture) withSession(t *testing.T) {
	t.Helper()
	if _, err := f.run.Start(f.ctx, dispatch.StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "build it"}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	f.f.mr = &forge.MR{IID: 7, State: "opened", URL: "https://gl/mr/7", Pipeline: &forge.Pipeline{ID: 100, Status: "running"}}
}

func TestReviewThreadsGoBackOnce(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	f.f.threads = []forge.Thread{{ID: "d1abcdef0123", NoteID: 55, URL: "https://gl/mr/7#note_55", File: "a.go", Line: 4, Body: "bot: handle nil"}}
	f.w.Check(f.ctx)
	f.settle(t)
	runs, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10)
	if len(runs) != 2 || !strings.Contains(runs[0].Prompt, "[handle nil](https://gl/mr/7#note_55) [d1abcdef] (a.go:4)") ||
		!strings.Contains(runs[0].Prompt, "[!7](https://gl/mr/7)") || !strings.Contains(runs[0].Prompt, "Do not resolve") {
		t.Fatalf("runs = %+v", runs)
	}
	// Seen again: nothing, even from a fresh watcher (the ids are stored).
	w2 := &Watcher{Store: f.w.Store, Fold: f.w.Fold, Runner: f.w.Runner, Log: f.w.Log, Interval: f.w.Interval, ForgeFor: f.w.ForgeFor}
	w2.Check(f.ctx)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 2 {
		t.Fatalf("handed the same thread twice: %d runs", len(n))
	}
	// A new thread goes; the first resolves and drops out of the record.
	f.f.threads = []forge.Thread{{ID: "d2", Body: "bot: rename"}}
	f.w.Check(f.ctx)
	f.settle(t)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 3 {
		t.Fatalf("runs = %d, want 3", len(n))
	}
	lf, _ := f.st.LaneForge(f.ctx, f.lane.ID)
	if lf.ThreadsSeen != "d2" || lf.FixTries != 2 {
		t.Errorf("forge = %+v", lf)
	}
	// Budget spent: a third thread is left to the person, once.
	f.f.threads = []forge.Thread{{ID: "d2"}, {ID: "d3", Body: "bot: more"}}
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 3 {
		t.Errorf("runs = %d after the cap", len(n))
	}
	if got := f.feed(t); strings.Count(got, "are spent") != 1 {
		t.Errorf("feed = %s", got)
	}
}

func TestReviewThreadsCanBeSwitchedOffAndCapped(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	f.f.threads = []forge.Thread{{ID: "d1", Body: "x"}}
	cfg := config.Default()
	off := false
	cfg.Defaults.Watch.ReviewThreads = &off
	f.run.SetConfig(cfg)
	f.w.Check(f.ctx)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 1 {
		t.Fatalf("switched off, but %d runs", len(n))
	}
	// A repo override turns it on; a cap of 0 still keeps the agent out.
	zero := 0
	on := true
	cfg.Repos = map[string]config.Profile{"app": {Watch: config.Watch{ReviewThreads: &on, FixTries: &zero}}}
	f.run.SetConfig(cfg)
	f.w.Check(f.ctx)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 1 {
		t.Fatalf("cap 0, but %d runs", len(n))
	}
}

func TestRequireApprovalFailureIsNotHandedToTheAgent(t *testing.T) {
	f := newFixture(t)
	f.withSession(t)
	f.w.Check(f.ctx)
	f.f.mr.Pipeline = &forge.Pipeline{ID: 100, Status: "failed"}
	f.f.jobs = []forge.Job{{ID: 9, Name: forge.RequireApprovalJob}}
	f.w.Check(f.ctx)
	if n, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10); len(n) != 1 {
		t.Fatalf("require-approval handed to the agent: %d runs", len(n))
	}
	if got := f.feed(t); !strings.Contains(got, "needs a reviewer") {
		t.Errorf("feed = %s", got)
	}
	if lf, _ := f.st.LaneForge(f.ctx, f.lane.ID); lf.FixTries != 0 {
		t.Errorf("fix tries = %d", lf.FixTries)
	}
	// With a real failure beside it, only the real one goes, and its log is all it gets.
	f.f.mr.Pipeline = &forge.Pipeline{ID: 101, Status: "failed"}
	f.f.jobs = []forge.Job{{ID: 9, Name: forge.RequireApprovalJob}, {ID: 1, Name: "go-check"}}
	f.w.Check(f.ctx)
	f.settle(t)
	runs, _ := f.st.Runs(f.ctx, f.lane.ID, "", 10)
	if len(runs) != 2 || strings.Contains(runs[0].Prompt, forge.RequireApprovalJob) || !strings.Contains(runs[0].Prompt, "go-check") {
		t.Errorf("runs = %+v", runs)
	}
}
