package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// finishedRun records a run in the lane that ended an hour ago in state, whose last
// report says status ("" for none).
func (f *fixture) finishedRun(t *testing.T, state, status string) store.Run {
	t.Helper()
	ended := time.Now().Add(-time.Hour)
	run, err := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", Prompt: "do it", State: state, Started: ended.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if state != store.RunRunning {
		run.Ended = &ended
		if err := f.st.UpdateRun(f.ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	if status != "" {
		if _, err := f.st.AddEvent(f.ctx, store.Event{RunID: run.ID, Kind: "report", Status: status, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func (f *fixture) git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// commitInLane commits a file in the lane's worktree.
func (f *fixture) commitInLane(t *testing.T) {
	t.Helper()
	os.WriteFile(filepath.Join(f.lane.Worktree, "work.txt"), []byte("work\n"), 0o644)
	f.git(t, f.lane.Worktree, "add", ".")
	f.git(t, f.lane.Worktree, "commit", "-q", "-m", "work")
}

func (f *fixture) state(t *testing.T) string {
	t.Helper()
	l, err := f.st.Lane(f.ctx, f.lane.ID)
	if err != nil {
		t.Fatal(err)
	}
	return l.State
}

func TestAutoCloseClosesAnIdleFinishedLaneWithNoCommits(t *testing.T) {
	f := newFixture(t)
	f.finishedRun(t, store.RunSucceeded, "done")
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneClosed {
		t.Fatalf("lane not closed; feed:\n%s", f.feed(t))
	}
	if _, err := os.Stat(f.lane.Worktree); !os.IsNotExist(err) {
		t.Error("worktree left behind")
	}
	if !strings.Contains(f.feed(t), "lane_closed") && !strings.Contains(f.feed(t), "lane feat/x closed by Shepherd") {
		t.Errorf("feed = %s", f.feed(t))
	}
}

func TestAutoCloseClosesALaneWhoseCommitsLandedElsewhere(t *testing.T) {
	f := newFixture(t)
	f.commitInLane(t)
	// The commit reaches the base by another route, as when a release lane's change
	// goes in through another branch.
	f.git(t, f.lane.Worktree, "push", "-q", "origin", "HEAD:main")
	f.finishedRun(t, store.RunSucceeded, "done")
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneClosed {
		t.Fatalf("lane not closed; feed:\n%s", f.feed(t))
	}
}

func TestAutoCloseLeavesALaneOpen(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
	}{
		{"no run ever", func(t *testing.T, f *fixture) {}},
		{"a run is going", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			f.finishedRun(t, store.RunRunning, "")
		}},
		{"last run failed", func(t *testing.T, f *fixture) { f.finishedRun(t, store.RunFailed, "done") }},
		{"last run was interrupted by a daemon restart", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			f.finishedRun(t, store.RunInterrupted, "progress")
		}},
		{"last run was stopped", func(t *testing.T, f *fixture) { f.finishedRun(t, store.RunStopped, "done") }},
		{"last run did not report", func(t *testing.T, f *fixture) { f.finishedRun(t, store.RunSucceeded, "") }},
		{"last run reported blocked", func(t *testing.T, f *fixture) { f.finishedRun(t, store.RunSucceeded, "blocked") }},
		{"last run ended a moment ago", func(t *testing.T, f *fixture) {
			run := f.finishedRun(t, store.RunSucceeded, "done")
			now := time.Now()
			run.Ended = &now
			f.st.UpdateRun(f.ctx, run)
		}},
		{"a decision is open", func(t *testing.T, f *fixture) {
			run := f.finishedRun(t, store.RunSucceeded, "done")
			if _, err := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "which?", State: store.DecisionOpen}); err != nil {
				t.Fatal(err)
			}
		}},
		{"an answer is undelivered", func(t *testing.T, f *fixture) {
			run := f.finishedRun(t, store.RunSucceeded, "done")
			d, err := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "which?", State: store.DecisionOpen})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.st.AnswerDecision(f.ctx, d.ID, "this one"); err != nil {
				t.Fatal(err)
			}
		}},
		{"a request from it is open", func(t *testing.T, f *fixture) {
			run := f.finishedRun(t, store.RunSucceeded, "done")
			if _, err := f.st.CreateRequest(f.ctx, store.Request{FromRun: run.ID, Kind: store.RequestQuestion, Message: "?", State: store.RequestNeedsRouting}); err != nil {
				t.Fatal(err)
			}
		}},
		{"a request for it is queued", func(t *testing.T, f *fixture) {
			run := f.finishedRun(t, store.RunSucceeded, "done")
			if _, err := f.st.CreateRequest(f.ctx, store.Request{FromRun: run.ID, Kind: store.RequestQuestion, Lane: "feat/x", Message: "?", State: store.RequestPending}); err != nil {
				t.Fatal(err)
			}
		}},
		{"uncommitted changes", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			os.WriteFile(filepath.Join(f.lane.Worktree, "wip.txt"), []byte("mine"), 0o644)
		}},
		{"a commit not in the base", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			f.commitInLane(t)
		}},
		{"its merge request is open", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			f.f.mr = &forge.MR{IID: 3, State: "opened", SHA: headOf(t, f.lane.Worktree)}
		}},
		{"auto-close is off", func(t *testing.T, f *fixture) {
			f.finishedRun(t, store.RunSucceeded, "done")
			f.w.NoAutoClose = true
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.setup(t, f)
			f.w.Check(f.ctx)
			if f.state(t) != store.LaneOpen {
				t.Fatalf("lane closed; feed:\n%s", f.feed(t))
			}
			if _, err := os.Stat(f.lane.Worktree); err != nil {
				t.Errorf("worktree gone: %v", err)
			}
		})
	}
}
