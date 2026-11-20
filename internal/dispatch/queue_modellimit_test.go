package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
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
