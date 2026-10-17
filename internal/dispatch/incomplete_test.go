package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// laneRuns is the lane's runs, oldest first.
func laneRuns(t *testing.T, f *fixture) []store.Run {
	t.Helper()
	runs, _ := f.st.Runs(context.Background(), 0, "", 100)
	var out []store.Run
	for i := len(runs) - 1; i >= 0; i-- {
		out = append(out, runs[i])
	}
	return out
}

// waitRuns waits until n runs exist and none is running.
func waitRuns(t *testing.T, f *fixture, n int) []store.Run {
	t.Helper()
	for i := 0; i < 400; i++ {
		runs := laneRuns(t, f)
		busy := false
		for _, r := range runs {
			busy = busy || r.State == store.RunRunning
		}
		if len(runs) >= n && !busy {
			return runs
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("never reached %d finished run(s): %+v", n, laneRuns(t, f))
	return nil
}

// RUN-C-01: exit 0 with uncommitted changes and no question is incomplete, and is
// continued once in the same session, told to commit.
func TestIncompleteRunIsContinuedOnce(t *testing.T) {
	f := newFixture(t, "dirtythen")
	first, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	runs := waitRuns(t, f, 2)
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want the run and one continuation", len(runs))
	}
	a, b := runs[0], runs[1]
	if a.ID != first.ID || a.State == store.RunSucceeded {
		t.Errorf("the unfinished run = %s, want not succeeded", a.State)
	}
	if b.Parent != a.ID || b.Session != a.Session {
		t.Errorf("continuation = %+v, want the same session as run %d", b, a.ID)
	}
	if !strings.Contains(stdinOf(t, b), "Commit your work (or explain why not), push, and stop.") {
		t.Errorf("continuation prompt: %s", stdinOf(t, b))
	}
	if b.State != store.RunSucceeded || b.Commits != 1 {
		t.Errorf("continuation = %s with %d commit(s), error %q", b.State, b.Commits, b.Error)
	}
}

// RUN-C-02, and the continuation never loops: it ends incomplete again, so it fails.
func TestIncompleteTwiceFailsAndNeverLoops(t *testing.T) {
	f := newFixture(t, "dirty")
	if _, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"}); err != nil {
		t.Fatal(err)
	}
	runs := waitRuns(t, f, 2)
	f.runner.Wait()
	if runs = laneRuns(t, f); len(runs) != 2 {
		t.Fatalf("runs = %d, want exactly one continuation", len(runs))
	}
	last := runs[1]
	if last.State != store.RunFailed || last.Error != "ended twice without finishing" {
		t.Errorf("last run = %s, error %q", last.State, last.Error)
	}
	if k := feedKind(t, f, "ended twice without finishing"); k != store.FeedRunFailed {
		t.Errorf("feed kind = %q", k)
	}
}

// RUN-C-03: a run that asked is waiting on the decision: not incomplete, not continued.
func TestAskedRunIsWaitingNotIncomplete(t *testing.T) {
	f := newFixture(t, "dirtyslow")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Ask(context.Background(), store.Decision{RunID: run.ID, Question: "Which table?"}); err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	f.runner.Wait()
	if done.State != store.RunSucceeded {
		t.Errorf("run = %s, error %q", done.State, done.Error)
	}
	if n := len(laneRuns(t, f)); n != 1 {
		t.Errorf("%d runs, want nothing continued", n)
	}
}

// RUN-C-04: a report that no change is needed, with its reason, is a success.
func TestReportedNoChangeIsSucceeded(t *testing.T) {
	f := openCodeFixture(t, "wait")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Record(context.Background(), store.Event{RunID: run.ID, Kind: EventReport, Status: "done", Text: "nothing to change: the test already passes on dev"}); err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	f.runner.Wait()
	if done.State != store.RunSucceeded || done.Error != "" || len(laneRuns(t, f)) != 1 {
		t.Errorf("run = %s, error %q, %d run(s)", done.State, done.Error, len(laneRuns(t, f)))
	}
}
