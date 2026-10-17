package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// napAgent is a fake agent that works for $NAP seconds, then exits cleanly.
const napAgent = `#!/bin/sh
if [ "$1" = create-chat ]; then echo "11111111-2222-4333-8444-555555555555"; exit 0; fi
prompt=$(cat)
echo "ARGS: $(printf '%s ' "$@" | tr '\n' ' ')"
echo "STDIN: $(printf '%s' "$prompt" | tr '\n' ' ')"
sleep ${NAP:-0}
exit 0
`

// napFixture is a fixture whose agent is napAgent.
func napFixture(t *testing.T, nap string) *fixture {
	t.Helper()
	f := newFixture(t, "ok")
	t.Setenv("NAP", nap)
	agent := filepath.Join(t.TempDir(), "nap-agent")
	if err := os.WriteFile(agent, []byte(napAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	f.runner.lookPath = func(string) (string, error) { return agent, nil }
	return f
}

// restarted is the runner of a daemon that started again on the same store.
func (f *fixture) restarted(t *testing.T) *Runner {
	t.Helper()
	r := &Runner{Store: f.st, Config: f.runner.Config, Dir: f.runner.Dir,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), lookPath: f.runner.lookPath}
	t.Cleanup(r.Wait)
	return r
}

func (f *fixture) drained(t *testing.T) *Drain {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if d := f.runner.Draining(); d != nil && d.Drained {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the drain did not finish")
	return nil
}

func runsOf(t *testing.T, st store.Store) []store.Run {
	t.Helper()
	runs, err := st.Runs(context.Background(), 0, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestDrainWithNothingRunningIsAtOnce(t *testing.T) {
	f := napFixture(t, "0")
	start := time.Now()
	f.runner.Drain(10 * time.Minute)
	if d := f.drained(t); d.Runs != 0 || time.Since(start) > 2*time.Second {
		t.Errorf("drain = %+v after %s", d, time.Since(start))
	}
}

func TestDrainHoldsAStartAndItRunsAfterTheRestart(t *testing.T) {
	f := napFixture(t, "1")
	ctx := context.Background()
	going, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	f.runner.Drain(10 * time.Minute)
	if d := f.runner.Draining(); d == nil || d.Runs != 1 || d.Drained {
		t.Fatalf("draining with a run going = %+v", d)
	}
	other := openLane(t, f, "other", "docs/**")
	_, err = f.runner.Start(ctx, StartRequest{LaneID: other.ID, Agent: "claude", Prompt: "second"})
	if !errors.Is(err, ErrQueued) || !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), DrainReason) {
		t.Fatalf("a start while draining: %v", err)
	}
	if n := len(runsOf(t, f.st)); n != 1 {
		t.Fatalf("%d runs: the held start must not have begun", n)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 1 {
		t.Fatalf("held starts = %+v", held)
	}
	waitFeed(t, f, "start held in lane other")

	// The run ends on its own, so it is not interrupted; the drain finishes.
	f.drained(t)
	if got, _ := f.st.Run(ctx, going.ID); got.State != store.RunSucceeded || got.Reason != "" {
		t.Errorf("the run that finished during the drain = %+v", got)
	}

	// The daemon restarts: the held start goes.
	next := f.restarted(t)
	if err := next.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := next.ResumeRestarted(ctx); err != nil {
		t.Fatal(err)
	}
	var second *store.Run
	for _, r := range runsOf(t, f.st) {
		if r.LaneID == other.ID {
			second = &r
		}
	}
	if second == nil || second.Prompt != "second" {
		t.Fatalf("after the restart the held start did not run: %+v", runsOf(t, f.st))
	}
	f.wait(t, second.ID)
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts left: %+v", held)
	}
	waitFeed(t, f, "started after the restart")
}

func TestDrainTimeoutInterruptsForRestartAndKeepsTheSession(t *testing.T) {
	f := napFixture(t, "30")
	ctx := context.Background()
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "long job"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Session == "" {
		t.Fatal("the run has no session")
	}
	f.runner.Drain(300 * time.Millisecond)
	f.drained(t)
	got, _ := f.st.Run(ctx, run.ID)
	if got.State != store.RunInterrupted || got.Reason != store.ReasonRestart || got.Session != run.Session {
		t.Errorf("run after the drain timed out = %+v", got)
	}
	if f.runner.Active() != 0 {
		t.Error("an agent is still going")
	}
}

func TestRestartInterruptedRunIsContinuedOnceInItsSession(t *testing.T) {
	f := napFixture(t, "30")
	ctx := context.Background()
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "long job"})
	if err != nil {
		t.Fatal(err)
	}
	f.runner.Drain(200 * time.Millisecond)
	f.drained(t)

	t.Setenv("NAP", "0")
	next := f.restarted(t)
	if err := next.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := next.ResumeRestarted(ctx); err != nil {
		t.Fatal(err)
	}
	runs := runsOf(t, f.st)
	if len(runs) != 2 {
		t.Fatalf("runs after the restart = %+v", runs)
	}
	child := runs[0]
	if child.Parent != run.ID || child.Session != run.Session || child.Prompt != ResumePrompt {
		t.Errorf("the continuation = %+v", child)
	}
	f.wait(t, child.ID)
	if b, _ := os.ReadFile(child.Log); !strings.Contains(string(b), "check `git status` first") {
		t.Errorf("the agent was not told: %s", b)
	}
	waitFeed(t, f, "will be continued")

	// A second restart does not continue it again.
	again := f.restarted(t)
	if err := again.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := again.ResumeRestarted(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(runsOf(t, f.st)); n != 2 {
		t.Errorf("%d runs after a second restart, want 2", n)
	}
}

func TestCrashInterruptedRunIsNotResumed(t *testing.T) {
	f := napFixture(t, "0")
	ctx := context.Background()
	stale, _ := f.st.CreateRun(ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", Session: "s-1", State: store.RunRunning, Log: "l"})
	next := f.restarted(t)
	if err := next.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := next.ResumeRestarted(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.Run(ctx, stale.ID)
	if got.State != store.RunInterrupted || got.Reason != "" {
		t.Errorf("crashed run = %+v", got)
	}
	if n := len(runsOf(t, f.st)); n != 1 {
		t.Errorf("%d runs: a crash must not be resumed", n)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts = %+v", held)
	}
}

func TestResumeGoesThroughTheRunCaps(t *testing.T) {
	f := napFixture(t, "30")
	ctx := context.Background()
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "long job"})
	f.runner.Drain(100 * time.Millisecond)
	f.drained(t)

	next := f.restarted(t)
	next.Config.Daemon.MaxRuns = 1
	other := openLane(t, f, "other", "docs/**")
	busy, err := next.Start(ctx, StartRequest{LaneID: other.ID, Agent: "claude", Prompt: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := next.ResumeRestarted(ctx); err != nil {
		t.Fatal(err)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 1 {
		t.Fatalf("the cap did not hold the continuation: %+v", held)
	}
	// When the busy run ends, the held continuation goes.
	t.Setenv("NAP", "0")
	next.Stop(ctx, busy.ID)
	f.wait(t, busy.ID)
	waitHeldEmpty(t, f)
	if got := runsOf(t, f.st); got[0].Parent != run.ID {
		t.Errorf("newest run = %+v", got[0])
	}
}

func waitHeldEmpty(t *testing.T, f *fixture) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if h, _ := f.st.HeldStarts(context.Background()); len(h) == 0 {
			return
		}
	}
	t.Fatal("the held start never went")
}

// failingDelete is a store whose held starts cannot be removed.
type failingDelete struct{ store.Store }

func (failingDelete) DeleteHeldStart(context.Context, int64) error { return errors.New("disk full") }

func TestHeldStartIsNotRunTwiceWhenItsRowWillNotGo(t *testing.T) {
	f := napFixture(t, "0")
	ctx := context.Background()
	b, _ := json.Marshal(StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "once"})
	f.st.AddHeldStart(ctx, store.HeldStart{Request: string(b), Why: "x"})
	f.runner.Store = failingDelete{f.st}
	f.runner.RunHeld(ctx)
	f.runner.Wait()
	f.runner.RunHeld(ctx)
	f.runner.Wait()
	if n := len(runsOf(t, f.st)); n != 1 {
		t.Errorf("%d runs for one held start", n)
	}
}

func TestDrainIsNotDrainedWhileARunIsStillBeingRecorded(t *testing.T) {
	f := napFixture(t, "0")
	f.runner.wg.Add(1) // a run's end, still being recorded
	f.runner.Drain(time.Minute)
	time.Sleep(300 * time.Millisecond)
	if d := f.runner.Draining(); d == nil || d.Drained {
		t.Fatalf("drained with a run still ending: %+v", d)
	}
	f.runner.wg.Done()
	f.drained(t)
}
