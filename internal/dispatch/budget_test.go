package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestOutputReaders(t *testing.T) {
	o := claudeOutput(`{"type":"assistant","message":{"content":[{"type":"text","text":"Done."},{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}}`)
	if o.Show != "Done.\n→ Edit {\"file_path\":\"a.go\"}" {
		t.Errorf("assistant line = %q", o.Show)
	}
	if o := claudeOutput(`{"type":"result","subtype":"success","total_cost_usd":0.1234}`); o.USD != 0.1234 || o.Show != "" {
		t.Errorf("result = %+v", o)
	}
	if o := claudeOutput(`{"type":"user","message":{"content":[{"type":"tool_result"}]}}`); o.Show != "" {
		t.Errorf("tool result shown: %+v", o)
	}
	if o := claudeOutput("plain text"); o.Show != "plain text" {
		t.Errorf("non-JSON line = %+v", o)
	}
	if o := copilotOutput("AI Credits 0.36 (39s)"); o.Credits != 0.36 || o.Show == "" {
		t.Errorf("copilot credits = %+v", o)
	}
}

func TestBudgetHoldsAutomaticRuns(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	cfg, _ := config.Parse([]byte("daemon:\n  budget: 1\n  credit_usd: 0.5\n"))
	f.runner.Config = cfg

	// 0.6 dollars plus one credit at 0.5 is 1.10: past 80% after the first, past 100% after the second.
	f.runner.Spend(ctx, store.Spend{Source: "claude", USD: 0.85})
	f.runner.Spend(ctx, store.Spend{Source: "copilot", Credits: 0.5})
	f.runner.Spend(ctx, store.Spend{Source: "desk", USD: 0.01})
	spent, by, _ := f.runner.Spent(ctx)
	if spent < 1.10 || spent > 1.11 || by["copilot"].Credits != 0.5 {
		t.Errorf("spent = %v, by = %+v", spent, by)
	}
	items, _ := f.st.Feed(ctx, 0, 50)
	var warn, reached int
	for _, it := range items {
		if strings.Contains(it.Text, "80% of today's budget") {
			warn++
		}
		if strings.Contains(it.Text, "Daily budget reached") {
			reached++
		}
	}
	if warn != 1 || reached != 1 {
		t.Errorf("warnings: 80%% x%d, reached x%d", warn, reached)
	}

	if _, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", Auto: true}); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "daily budget") {
		t.Errorf("automatic run over budget: %v", err)
	}
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatalf("a run the person starts should still go: %v", err)
	}
	f.wait(t, run.ID)

	zero, _ := config.Parse([]byte("daemon:\n  budget: 0\n"))
	f.runner.Config = zero
	if why := f.runner.overBudget(ctx); why != "" {
		t.Errorf("budget 0 should mean no cap: %s", why)
	}
}
