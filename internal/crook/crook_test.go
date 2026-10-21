package crook

import (
	"context"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

func TestAutonomyValuesMatchConfig(t *testing.T) {
	if AutonomyShepherd != config.Shepherd || AutonomyAgent != config.Agent {
		t.Fatalf("crook spells autonomy %q/%q, config %q/%q", AutonomyShepherd, AutonomyAgent, config.Shepherd, config.Agent)
	}
}

func TestStageOrderFloorBeatsPolicy(t *testing.T) {
	// A push that policy would let through (agent autonomy, origin) still fails when
	// the scope checks found problems.
	v := Decide(context.Background(), Request{Action: ActionPush, Actor: ActorWorker, Facts: Facts{
		Push: AutonomyAgent, ToOrigin: true, Problems: []string{"outside scope"},
	}})
	if v.Kind != Refuse || v.Rule != RuleScopePush || v.Reason != "outside scope" {
		t.Fatalf("floor should win over policy: %+v", v)
	}
	if v := Decide(context.Background(), Request{Action: ActionPush, Actor: ActorWorker, Facts: Facts{Push: AutonomyAgent, ToOrigin: true}}); v.Kind != Proceed || v.Rule != RulePushAutonomy {
		t.Fatalf("policy alone: %+v", v)
	}
}

func TestStageOrderLimitsBeatPolicy(t *testing.T) {
	// Merge policy would proceed, but a run cap is reached.
	v := Decide(context.Background(), Request{Action: ActionMergeArm, Actor: ActorDaemon, Facts: Facts{
		Merge: AutonomyAgent, GitLab: true, Runs: &RunCounts{Running: 4, Max: 4},
	}})
	if v.Kind != Hold || v.Rule != RuleRunCaps {
		t.Fatalf("limits should win over policy: %+v", v)
	}
}

func TestFloorBeatsLimits(t *testing.T) {
	v := Decide(context.Background(), Request{Action: ActionDecisionAnswer, Actor: ActorDesk, Facts: Facts{Budget: "over"}})
	if v.Kind != Refuse || v.Rule != RulePersonAnswers {
		t.Fatalf("floor should win over limits: %+v", v)
	}
}

func TestPersonOnly(t *testing.T) {
	for a := range personOnly {
		for _, actor := range []Actor{ActorDesk, ActorWorker, ActorDaemon} {
			v := Decide(context.Background(), Request{Action: a, Actor: actor})
			if v.Kind != Refuse || v.Rule != RulePersonAnswers || v.Reason != PersonOnlyReason(a) {
				t.Errorf("%s by %s: %+v", a, actor, v)
			}
		}
		if v := Decide(context.Background(), Request{Action: a, Actor: ActorPerson}); v.Kind != Proceed {
			t.Errorf("%s by the person: %+v", a, v)
		}
	}
}

func TestKeepGoingOnlyHoldsTheDesk(t *testing.T) {
	calls := 0
	f := Facts{KeepGoing: func(context.Context) (bool, string) { calls++; return false, "keep-going is off: x" }}
	v := Decide(context.Background(), Request{Action: KeepGoingAction("continue"), Actor: ActorDesk, Facts: f})
	if v.Kind != Refuse || v.Rule != RuleKeepGoingLimits || v.Reason != "keep-going is off: x" || calls != 1 {
		t.Fatalf("desk: %+v calls %d", v, calls)
	}
	v = Decide(context.Background(), Request{Action: KeepGoingAction("continue"), Actor: ActorPerson, Facts: f})
	if v.Kind != Proceed || calls != 1 {
		t.Fatalf("person: %+v calls %d", v, calls)
	}
	ok := Facts{KeepGoing: func(context.Context) (bool, string) { return true, "" }}
	if v := Decide(context.Background(), Request{Action: KeepGoingAction("continue"), Actor: ActorDesk, Facts: ok}); v.Kind != Proceed || v.Rule != RuleKeepGoingLimits {
		t.Fatalf("allowed: %+v", v)
	}
}

func TestBudgetAndRunCaps(t *testing.T) {
	ctx := context.Background()
	v := Decide(ctx, Request{Action: ActionRunStart, Actor: ActorDaemon, Facts: Facts{Budget: "spent", BudgetDaily: true, Runs: &RunCounts{Max: 4}}})
	if v.Kind != Hold || v.Rule != RuleDailyBudget || v.Reason != "spent" {
		t.Fatalf("daily: %+v", v)
	}
	if v := Decide(ctx, Request{Action: ActionRunStart, Actor: ActorDaemon, Facts: Facts{Budget: "spent"}}); v.Rule != RuleBudget {
		t.Fatalf("other period: %+v", v)
	}
	v = Decide(ctx, Request{Action: ActionRunStart, Facts: Facts{Runs: &RunCounts{Running: 2, Max: 2}}})
	if v.Kind != Hold || v.Reason != "2 agents are already running, the limit on this machine (daemon.max_runs)" {
		t.Fatalf("machine cap: %+v", v)
	}
	v = Decide(ctx, Request{Action: ActionRunStart, Facts: Facts{Runs: &RunCounts{Running: 1, Max: 4, Repo: "r", InRepo: 1, RepoMax: 1}}})
	if v.Kind != Hold || v.Reason != "1 agents are already running in r, its limit (max_runs)" {
		t.Fatalf("repo cap: %+v", v)
	}
	if v := Decide(ctx, Request{Action: ActionRunStart, Facts: Facts{Runs: &RunCounts{Running: 1, Max: 4, Repo: "r", InRepo: 1}}}); v.Kind != Proceed || v.Rule != RuleRunCaps {
		t.Fatalf("under caps: %+v", v)
	}
}

func TestAutonomyPolicy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		req  Request
		kind Kind
		rule string
		has  string
	}{
		{"agent push, human repo", Request{Action: ActionPush, Facts: Facts{Push: "human", Run: 3, Agent: "claude", Lane: "l"}}, Refuse, RulePushAutonomy, "agents Shepherd starts never push"},
		{"agent push elsewhere", Request{Action: ActionPush, Facts: Facts{Push: "agent", Remote: "fork"}}, Refuse, RulePushAutonomy, `pushes to "fork"`},
		{"ship, agent repo", Request{Action: ActionShip, Facts: Facts{Push: "agent", Repo: "r", Lane: "l"}}, Refuse, RulePushAutonomy, `r has not opted in to Shepherd pushing (its profile's autonomy.push is "agent", not "shepherd"): push lane l yourself`},
		{"ship ok", Request{Action: ActionShip, Facts: Facts{Push: "shepherd"}}, Proceed, RulePushAutonomy, ""},
		{"tag push needs all three", Request{Action: ActionTagPush, Facts: Facts{Push: "shepherd", Tag: "agent"}}, Refuse, RulePushAutonomy, "reserved tags"},
		{"tag push ok", Request{Action: ActionTagPush, Facts: Facts{Push: "shepherd", Tag: "agent", TagsReserved: true}}, Proceed, RulePushAutonomy, ""},
		{"merge human", Request{Action: ActionMergeArm, Facts: Facts{Merge: "human", GitLab: true}}, Refuse, RuleMergeAutonomy, "autonomy.merge"},
		{"merge not gitlab", Request{Action: ActionMergeArm, Facts: Facts{Merge: "agent"}}, Refuse, RuleMergeAutonomy, "GitLab"},
		{"merge ok", Request{Action: ActionMergeArm, Facts: Facts{Merge: "agent", GitLab: true}}, Proceed, RuleMergeAutonomy, ""},
	}
	for _, c := range cases {
		v := Decide(ctx, c.req)
		if v.Kind != c.kind || v.Rule != c.rule || !strings.Contains(v.Reason, c.has) {
			t.Errorf("%s: %+v", c.name, v)
		}
	}
}

func TestRouteRoles(t *testing.T) {
	ctx := context.Background()
	for d, want := range map[Denial]string{
		DeniedDesk:     "the front desk's token cannot do this; it is the person's",
		DeniedWeb:      "a browser session cannot do this; use the shepherd command",
		DeniedWorker:   "an agent's token cannot do this; ask the person (ask_human) or another lane (ask_shepherd)",
		DeniedOtherRun: "an agent's token is for run 7 only",
	} {
		v := Decide(ctx, Request{Action: RouteAction("GET /v1/x"), Facts: Facts{Route: &RouteFacts{Denied: d, Run: 7}}})
		if v.Kind != Refuse || v.Rule != RuleRouteRoles || v.Reason != want {
			t.Errorf("%s: %+v", d, v)
		}
	}
	if v := Decide(ctx, Request{Action: RouteAction("GET /v1/x"), Facts: Facts{Route: &RouteFacts{}}}); v.Kind != Proceed || v.Rule != RuleDefault {
		t.Fatalf("allowed: %+v", v)
	}
}

func TestEveryVerdictHasARule(t *testing.T) {
	if v := Decide(context.Background(), Request{}); v.Rule == "" {
		t.Fatalf("%+v", v)
	}
}

func TestChecksActionsArePersonOnly(t *testing.T) {
	for _, a := range []Action{ActionChecksApprove, ActionChecksRelease} {
		if PersonOnlyReason(a) == "" {
			t.Errorf("%s is not person-only", a)
		}
		for _, actor := range []Actor{ActorWorker, ActorDesk, ActorDaemon} {
			v := Decide(context.Background(), Request{Action: a, Actor: actor})
			if v.Kind != Refuse || v.Rule != RulePersonAnswers {
				t.Errorf("%s by %s: %+v", a, actor, v)
			}
		}
		if v := Decide(context.Background(), Request{Action: a, Actor: ActorPerson}); !v.OK() {
			t.Errorf("%s by the person: %+v", a, v)
		}
	}
}
