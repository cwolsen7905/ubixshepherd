package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func limitFixture(t *testing.T, limits map[string]int) (*fixture, int64, int64) {
	t.Helper()
	f := newFixture(t, "sleep")
	cfg := config.Default()
	cfg.Daemon.ModelLimits = limits
	f.runner.SetConfig(cfg)
	other := openLane(t, f, "other", "docs/**")
	return f, f.lane.ID, other.ID
}

func TestModelLimitHoldsASecondRunAndStartsItWhenTheFirstEnds(t *testing.T) {
	f, a, b := limitFixture(t, map[string]int{"local-qwen": 1})
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "local-qwen", Prompt: "second"})
	if !errors.Is(err, ErrQueued) || !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), "queued: model local-qwen is at its limit of 1") {
		t.Fatalf("second run: %v", err)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 1 {
		t.Fatalf("held starts = %+v", held)
	}
	if got := runsWithPrompt(t, f, "second"); got != nil {
		t.Fatalf("second started past the limit: %+v", got)
	}
	// A third waits behind the second: first in, first out.
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "third"}); !errors.Is(err, ErrQueued) {
		t.Fatalf("third run: %v", err)
	}

	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
	second := waitRunWithPrompt(t, f, "second")
	if second.Model != "local-qwen" {
		t.Errorf("second model = %q", second.Model)
	}
	if got := runsWithPrompt(t, f, "third"); got != nil {
		t.Errorf("third started while second held the slot: %+v", got)
	}
	f.runner.Stop(ctx, second.ID)
	f.wait(t, second.ID)
	third := waitRunWithPrompt(t, f, "third")
	f.runner.Stop(ctx, third.ID)
	f.wait(t, third.ID)
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts left: %+v", held)
	}
}

func TestModelLimitLeavesOtherModelsAlone(t *testing.T) {
	f, a, b := limitFixture(t, map[string]int{"local-qwen": 1})
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "sonnet", Prompt: "other"})
	if err != nil {
		t.Fatalf("a model with no limit: %v", err)
	}
	f.runner.Stop(ctx, first.ID)
	f.runner.Stop(ctx, other.ID)
	f.wait(t, first.ID)
	f.wait(t, other.ID)
}

func TestNoModelLimitChangesNothing(t *testing.T) {
	f, a, b := limitFixture(t, nil)
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "local-qwen", Prompt: "second"})
	if err != nil {
		t.Fatalf("no limit set: %v", err)
	}
	f.runner.Stop(ctx, first.ID)
	f.runner.Stop(ctx, second.ID)
	f.wait(t, first.ID)
	f.wait(t, second.ID)
}

func TestModelLimitHoldsAnAutomaticRunWithoutQueueing(t *testing.T) {
	f, a, b := limitFixture(t, map[string]int{"local-qwen": 1})
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "local-qwen", Prompt: "auto", Auto: true})
	if !errors.Is(err, ErrHeld) || errors.Is(err, ErrQueued) {
		t.Fatalf("automatic run: %v", err)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("an automatic run was queued: %+v", held)
	}
	f.runner.Stop(ctx, first.ID)
	f.wait(t, first.ID)
}

func TestModelQueuedStartActsAsAQueuedFollowUp(t *testing.T) {
	f, a, b := limitFixture(t, map[string]int{"local-qwen": 1})
	ctx := context.Background()
	first, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.runner.Stop(ctx, first.ID); f.wait(t, first.ID) }()
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "local-qwen", Prompt: "second"}); !errors.Is(err, ErrQueued) {
		t.Fatalf("second: %v", err)
	}
	// It blocks a later start in the same lane, even on a model with no limit.
	_, err = f.runner.Start(ctx, StartRequest{LaneID: b, Agent: "claude", Model: "sonnet", Prompt: "third"})
	if !errors.Is(err, ErrQueued) || !strings.Contains(err.Error(), "queued behind earlier follow-ups") {
		t.Fatalf("later start in the lane: %v", err)
	}
	if n, _ := f.runner.queuedIn(ctx, b); n != 2 {
		t.Fatalf("queuedIn = %d, want 2", n)
	}
	// Closing the lane drops both.
	if n := f.runner.DropQueued(ctx, b, "lane closed"); n != 2 {
		t.Fatalf("dropped %d, want 2", n)
	}
	if held, _ := f.st.HeldStarts(ctx); len(held) != 0 {
		t.Errorf("held starts left: %+v", held)
	}
}

func TestModelLimitKeepsOrderWhenASlotIsFreeButOthersWait(t *testing.T) {
	f, a, b := limitFixture(t, map[string]int{"local-qwen": 1})
	ctx := context.Background()
	// A start waits for the model while its slot is free (the run that held it just
	// ended and RunHeld has not yet started the start).
	queued, _ := json.Marshal(StartRequest{LaneID: b, Agent: "claude", Model: "local-qwen", Prompt: "older"})
	if _, err := f.st.AddHeldStart(ctx, store.HeldStart{Request: string(queued), Why: modelWhyPrefix("local-qwen") + "at its limit of 1)"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.runner.queuedOnModel(ctx, "local-qwen"); err != nil || !ok {
		t.Fatalf("queuedOnModel = %v, %v", ok, err)
	}
	if ok, _ := f.runner.queuedOnModel(ctx, "other"); ok {
		t.Error("another model reported as queued")
	}
	_, err := f.runner.Start(ctx, StartRequest{LaneID: a, Agent: "claude", Model: "local-qwen", Prompt: "newer"})
	if !errors.Is(err, ErrQueued) || !strings.Contains(err.Error(), "queued: model local-qwen") {
		t.Fatalf("a fresh start must go behind the waiting one: %v", err)
	}
	older := waitRunWithPrompt(t, f, "older")
	f.runner.Stop(ctx, older.ID)
	f.wait(t, older.ID)
	newer := waitRunWithPrompt(t, f, "newer")
	f.runner.Stop(ctx, newer.ID)
	f.wait(t, newer.ID)
}
