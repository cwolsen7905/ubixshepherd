package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestEscalationTarget(t *testing.T) {
	cfg := config.Default()
	cfg.Defaults.Agent.Model = map[string]string{"claude": "opus"}
	for _, c := range []struct {
		name  string
		last  store.Run
		model string
		up    bool
	}{
		{"another agent", store.Run{Agent: "opencode", Model: "x/y"}, "opus", true},
		{"cursor", store.Run{Agent: "cursor"}, "opus", true},
		{"smaller claude", store.Run{Agent: "claude", Model: "haiku"}, "opus", true},
		{"claude on its default", store.Run{Agent: "claude", Model: "opus"}, "opus", false},
	} {
		agent, model, up := EscalationTarget(cfg, "app", c.last)
		if up != c.up || (up && (agent != "claude" || model != c.model)) {
			t.Errorf("%s: got %s %q %v", c.name, agent, model, up)
		}
	}
	// With no default named, Claude on any model is already where escalation goes.
	if _, _, up := EscalationTarget(config.Default(), "app", store.Run{Agent: "claude", Model: "haiku"}); up {
		t.Error("claude with no repo default escalated")
	}
	if _, m, up := EscalationTarget(config.Default(), "app", store.Run{Agent: "copilot"}); !up || m != "" {
		t.Errorf("copilot with no default: %q %v", m, up)
	}
}

func TestEscalatedFollowUpRunsOnClaude(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	cheap, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "cursor", Prompt: "first task"})
	if err != nil {
		t.Fatal(err)
	}
	cheap = f.wait(t, cheap.ID)

	// Not asked to escalate: the round stays with the same agent.
	same, err := f.runner.Start(ctx, StartRequest{Continue: cheap.ID, Prompt: "threads", Auto: true})
	if err != nil {
		t.Fatal(err)
	}
	if same = f.wait(t, same.ID); same.Agent != "cursor" {
		t.Errorf("plain continue ran on %s", same.Agent)
	}

	up, err := f.runner.Start(ctx, StartRequest{Continue: cheap.ID, Prompt: "threads", Auto: true, Escalate: true})
	if err != nil {
		t.Fatal(err)
	}
	up = f.wait(t, up.ID)
	if up.Agent != "claude" || up.Parent != 0 {
		t.Errorf("escalated run = %+v", up)
	}
	if in := stdinOf(t, up); !strings.Contains(in, "did not close what it was sent to close") || !strings.Contains(in, "threads") || !strings.Contains(in, "cursor") {
		t.Errorf("escalated prompt: %s", in)
	}

	// The next escalation resumes that Claude session, and a Claude run on its default
	// has nowhere to go.
	again, err := f.runner.Start(ctx, StartRequest{Continue: up.ID, Prompt: "more", Auto: true, Escalate: true})
	if err != nil {
		t.Fatal(err)
	}
	if again = f.wait(t, again.ID); again.Agent != "claude" || again.Session != up.Session || strings.Contains(stdinOf(t, again), "did not close") {
		t.Errorf("claude continue = %+v", again)
	}
}

func TestEscalationResumesTheLanesClaudeSession(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	first, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "first"})
	first = f.wait(t, first.ID)
	cheap, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "cursor", Prompt: "second"})
	cheap = f.wait(t, cheap.ID)
	up, err := f.runner.Start(ctx, StartRequest{Continue: cheap.ID, Prompt: "threads", Auto: true, Escalate: true})
	if err != nil {
		t.Fatal(err)
	}
	if up = f.wait(t, up.ID); up.Agent != "claude" || up.Session != first.Session || up.Parent != first.ID {
		t.Errorf("escalated run = %+v, want the lane's claude session %s of run %d", up, first.Session, first.ID)
	}
}

func TestEscalationStaysPutWhenItCannot(t *testing.T) {
	ctx := context.Background()

	off := newFixture(t, "quick")
	cfg := off.runner.Conf()
	no := false
	cfg.Defaults.Watch.Escalate = &no
	off.runner.SetConfig(cfg)
	cheap, _ := off.runner.Start(ctx, StartRequest{LaneID: off.lane.ID, Agent: "cursor", Prompt: "x"})
	cheap = off.wait(t, cheap.ID)
	next, err := off.runner.Start(ctx, StartRequest{Continue: cheap.ID, Prompt: "y", Auto: true, Escalate: true})
	if err != nil {
		t.Fatal(err)
	}
	if next = off.wait(t, next.ID); next.Agent != "cursor" {
		t.Errorf("watch.escalate off still moved to %s", next.Agent)
	}

	held := newFixture(t, "quick")
	cheap, _ = held.runner.Start(ctx, StartRequest{LaneID: held.lane.ID, Agent: "cursor", Prompt: "x"})
	cheap = held.wait(t, cheap.ID)
	held.runner.mu.Lock()
	held.runner.outOf = map[string]outOfQuota{"claude": {Limit: Limit{Until: time.Now().Add(time.Hour), Text: "limit"}}}
	held.runner.mu.Unlock()
	next, err = held.runner.Start(ctx, StartRequest{Continue: cheap.ID, Prompt: "y", Auto: true, Escalate: true})
	if err != nil {
		t.Fatal(err)
	}
	if next = held.wait(t, next.ID); next.Agent != "cursor" {
		t.Errorf("a claude quota hold moved the round to %s", next.Agent)
	}
}
