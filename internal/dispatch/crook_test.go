package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// These tests pin the verdicts Crook gives at dispatch's call sites: the rule that
// decided and the text said, which are what the old inline checks said.

func TestCrookBudgetHold(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	cfg, _ := config.Parse([]byte("daemon:\n  budget: 1\n"))
	f.runner.Config = cfg
	f.runner.Spend(ctx, store.Spend{Source: "claude", USD: 1.5})

	_, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", Auto: true})
	if !errors.Is(err, ErrHeld) || ruleOf(err) != crook.RuleDailyBudget {
		t.Fatalf("automatic run over budget: %v (rule %q)", err, ruleOf(err))
	}
	why, daily := f.runner.budgetHold(ctx, cfg, "")
	if !daily || err.Error() != why {
		t.Errorf("the hold says %q, the budget says %q", err, why)
	}
	// The person's own run is not held.
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatalf("a run the person starts: %v", err)
	}
	f.wait(t, run.ID)
}

func TestCrookRunCapRules(t *testing.T) {
	f := newFixture(t, "sleep")
	f.runner.Config.Daemon.MaxRuns = 1
	ctx := context.Background()
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.runner.Stop(ctx, run.ID); f.wait(t, run.ID) }()
	other := openLane(t, f, "other", "docs/**")
	_, err = f.runner.Start(ctx, StartRequest{LaneID: other.ID, Agent: "claude", Prompt: "y"})
	if !errors.Is(err, ErrHeld) || ruleOf(err) != crook.RuleRunCaps ||
		err.Error() != "1 agents are already running, the limit on this machine (daemon.max_runs)" {
		t.Fatalf("machine cap: %v (rule %q)", err, ruleOf(err))
	}
	repo, _ := f.st.Repo(ctx, f.lane.RepoID)
	capped := config.Default()
	capped.Daemon.MaxRuns = 6
	capped.Repos = map[string]config.Profile{repo.Name: {MaxRuns: 1}}
	f.runner.SetConfig(capped)
	_, err = f.runner.Start(ctx, StartRequest{LaneID: other.ID, Agent: "claude", Prompt: "y"})
	if !errors.Is(err, ErrHeld) || ruleOf(err) != crook.RuleRunCaps ||
		err.Error() != "1 agents are already running in "+repo.Name+", its limit (max_runs)" {
		t.Fatalf("repo cap: %v (rule %q)", err, ruleOf(err))
	}
}

func TestCrookShipVerdict(t *testing.T) {
	ctx := context.Background()
	f, _ := shipFixture(t, "ok", "repos:\n  app:\n    gate: \"true\"\n")
	_, err := f.runner.Ship(ctx, f.lane.ID)
	if !errors.Is(err, ErrRefused) || ruleOf(err) != crook.RulePushAutonomy ||
		!strings.HasPrefix(err.Error(), `app has not opted in to Shepherd pushing (its profile's autonomy.push is "human", not "shepherd"): push lane `) {
		t.Fatalf("not opted in: %v (rule %q)", err, ruleOf(err))
	}
	repo, _ := f.st.Repo(ctx, f.lane.RepoID)
	prof := f.runner.Conf().Profile(repo.Name)
	if v := shipVerdict(ctx, repo, f.lane, prof); v.Kind != crook.Refuse {
		t.Errorf("verdict %+v", v)
	}
	prof.Autonomy.Push = config.Shepherd
	if v := shipVerdict(ctx, repo, f.lane, prof); v.Kind != crook.Proceed || v.Rule != crook.RulePushAutonomy {
		t.Errorf("opted in: %+v", v)
	}
}

func TestCrookTagPushVerdict(t *testing.T) {
	f, _, _ := tagCase(t, "repos:\n  app:\n    gate: \"true\"\n", "v1.0.0", own)
	if said := f.pushTagsNow(t); said != nil {
		t.Errorf("a repo that has not opted in said %v", said)
	}
	v := crook.Decide(context.Background(), crook.Request{Action: crook.ActionTagPush, Actor: crook.ActorDaemon, Facts: crook.Facts{
		Push: config.Shepherd, Tag: config.Agent, TagsReserved: true,
	}})
	if v.Kind != crook.Proceed || v.Rule != crook.RulePushAutonomy {
		t.Errorf("opted in: %+v", v)
	}
}

func TestCrookMergeAutonomyPowers(t *testing.T) {
	var prof config.Profile
	prof.Autonomy.Merge = config.Agent
	prof.Autonomy.Push = config.Agent
	if p := powers(prof, "git@gitlab.com:g/r.git"); !p.Merge || !p.GitLab || p.Push != config.Agent {
		t.Errorf("merge: agent on GitLab: %+v", p)
	}
	if p := powers(prof, "git@github.com:g/r.git"); p.Merge || p.GitLab {
		t.Errorf("merge: agent off GitLab: %+v", p)
	}
	prof.Autonomy.Merge = config.Human
	if p := powers(prof, "git@gitlab.com:g/r.git"); p.Merge || !p.GitLab {
		t.Errorf("merge: human: %+v", p)
	}
}
