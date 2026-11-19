package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestFollowUpsQueueBehindARunAndGoInOrder(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.runner.Start(ctx, StartRequest{Continue: first.ID, Prompt: "second"})
	if !errors.Is(err, ErrQueued) || !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), fmt.Sprintf("queued behind run %d", first.ID)) {
		t.Fatalf("continue a running run: %v", err)
	}
	// A lane_run style start (no Continue) queues the same way.
	_, err = f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "third"})
	if !errors.Is(err, ErrQueued) {
		t.Fatalf("a start in a busy lane: %v", err)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 2 {
		t.Fatalf("held starts = %+v", held)
	}
	if n := len(runsOf(t, f.st)); n != 1 {
		t.Fatalf("%d runs; queued follow-ups must not have begun", n)
	}

	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	// The second starts when the first ends, and continues its session; the third
	// waits for the second.
	second := waitRunWithPrompt(t, f, "second")
	if second.Parent != first.ID || second.Session != first.Session {
		t.Errorf("second = parent %d session %q, want %d %q", second.Parent, second.Session, first.ID, first.Session)
	}
	if got := runsWithPrompt(t, f, "third"); got != nil {
		t.Errorf("third started while second was going: %+v", got)
	}
	f.runner.Stop(ctx, second.ID)
	f.wait(t, second.ID)
	third := waitRunWithPrompt(t, f, "third")
	if third.Parent != second.ID {
		t.Errorf("third parent = %d, want %d", third.Parent, second.ID)
	}
	f.runner.Stop(ctx, third.ID)
	f.wait(t, third.ID)
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts left: %+v", held)
	}
}

func TestQueuedFollowUpSurvivesARestart(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	if _, err := f.runner.Start(ctx, StartRequest{Continue: first.ID, Prompt: "later"}); !errors.Is(err, ErrQueued) {
		t.Fatal(err)
	}
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	// The row is in the store: a runner that did not see the start finds it.
	next := f.restarted(t)
	held, _ := f.st.HeldStarts(ctx)
	if len(held) == 0 {
		// The first runner already started it; the row's job is done.
		waitRunWithPrompt(t, f, "later")
		return
	}
	next.RunHeld(ctx)
	waitRunWithPrompt(t, f, "later")
}

func TestQueuedFollowUpsDropWhenTheLaneCloses(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	for _, p := range []string{"a", "b"} {
		if _, err := f.runner.Start(ctx, StartRequest{Continue: first.ID, Prompt: p}); !errors.Is(err, ErrQueued) {
			t.Fatal(err)
		}
	}
	if n := f.runner.DropQueued(ctx, f.lane.ID, "lane closed"); n != 2 {
		t.Errorf("dropped %d, want 2", n)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts left: %+v", held)
	}
	waitFeed(t, f, "2 queued follow-up(s) dropped")
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
}

func TestQueuedFollowUpIsDroppedWhenItsLaneClosedMeanwhile(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	if _, err := f.runner.Start(ctx, StartRequest{Continue: first.ID, Prompt: "orphan"}); !errors.Is(err, ErrQueued) {
		t.Fatal(err)
	}
	if err := f.st.SetLaneState(ctx, f.lane.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	waitFeed(t, f, "a queued follow-up was dropped")
	if got := runsWithPrompt(t, f, "orphan"); got != nil {
		t.Errorf("a follow-up ran in a closed lane: %+v", got)
	}
}

func runsWithPrompt(t *testing.T, f *fixture, prompt string) []store.Run {
	t.Helper()
	var out []store.Run
	for _, r := range runsOf(t, f.st) {
		if r.Prompt == prompt {
			out = append(out, r)
		}
	}
	return out
}

func waitRunWithPrompt(t *testing.T, f *fixture, prompt string) store.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if got := runsWithPrompt(t, f, prompt); len(got) > 0 {
			return got[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no run with prompt %q started", prompt)
	return store.Run{}
}

func TestQueuedFollowUpKeepsTheAgentAsked(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	if _, err := f.runner.Start(ctx, StartRequest{Continue: first.ID, Agent: "copilot", Prompt: "other"}); !errors.Is(err, ErrQueued) {
		t.Fatal(err)
	}
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	got := waitRunWithPrompt(t, f, "other")
	if got.Agent != "copilot" || got.Parent == first.ID {
		t.Errorf("run = agent %s parent %d, want copilot in its own session", got.Agent, got.Parent)
	}
	f.runner.Stop(ctx, got.ID)
	f.wait(t, got.ID)
}

func TestHeldFollowUpBlocksLaterOnesInItsLane(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	for _, p := range []string{"a", "b"} {
		if _, err := f.runner.Start(ctx, StartRequest{Continue: first.ID, Prompt: p}); !errors.Is(err, ErrQueued) {
			t.Fatal(err)
		}
	}
	// Shepherd's own start waits for the queue in an idle lane too.
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	a := waitRunWithPrompt(t, f, "a")
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "auto", Auto: true}); !errors.Is(err, ErrHeld) {
		t.Errorf("auto start with a follow-up queued: %v", err)
	}
	if got := runsWithPrompt(t, f, "b"); got != nil {
		t.Errorf("b started while a was going: %+v", got)
	}
	f.runner.Stop(ctx, a.ID)
	f.wait(t, a.ID)
	b := waitRunWithPrompt(t, f, "b")
	f.runner.Stop(ctx, b.ID)
	f.wait(t, b.ID)
}
