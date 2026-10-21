// Package crook is the one place that answers "may Shepherd do this now?". A caller
// describes the action, who is acting and the facts it has gathered, and Decide returns
// a verdict that carries the id of the rule that made it.
//
// This step only moves today's checks behind the call; it changes none of their
// answers. The stages run in a fixed order, in Go: floor, limits, guardrails, classify,
// policy, default. The floor holds what no policy may override and the limits hold what
// is spent or used up; policy holds the built-in rules that reproduce the repo
// autonomy settings. The guardrails and classify stages are named and empty.
//
// Decide does no I/O of its own. Callers read the store, git and the forge, and pass
// what they found as Facts; the one exception is Facts.KeepGoing, a check that also
// counts the start it lets through.
package crook

import (
	"context"
	"fmt"
	"strings"
)

// Kind is what a verdict says to do.
type Kind string

const (
	// Proceed: go ahead.
	Proceed Kind = "proceed"
	// Ask: the person decides. No current call site produces it.
	Ask Kind = "ask"
	// Hold: not now, and the action waits for something to change (a budget period
	// ending, a run finishing).
	Hold Kind = "hold"
	// Refuse: not at all, as asked.
	Refuse Kind = "refuse"
)

// Actor is who is acting.
type Actor string

const (
	// ActorPerson: the person, in the CLI or the web, or the front desk in a turn they
	// started.
	ActorPerson Actor = "person"
	// ActorDesk: the front desk in a turn Shepherd started (an event turn).
	ActorDesk Actor = "desk"
	// ActorDaemon: Shepherd itself, with no one asking.
	ActorDaemon Actor = "daemon"
	// ActorWorker: an agent Shepherd started.
	ActorWorker Actor = "worker"
)

// Action names what is to be done.
type Action string

// KeepGoingAction is the action for something keep-going mode covers: name is the
// keep-going action ("start_item", "continue", ...).
func KeepGoingAction(name string) Action { return Action("keep-going/" + name) }

const (
	// ActionRunStart: start an agent run.
	ActionRunStart Action = "run.start"
	// ActionPush: an agent pushes its lane's branch while its run is going.
	ActionPush Action = "push.agent"
	// ActionShip: Shepherd pushes a lane and opens its merge request.
	ActionShip Action = "lane.ship"
	// ActionTagPush: Shepherd pushes a release tag an agent reserved and created.
	ActionTagPush Action = "tag.push"
	// ActionMergeArm: an agent arms merge when the pipeline succeeds.
	ActionMergeArm Action = "merge.arm"
	// ActionPrePush: the checks on a push from a lane, or of a reserved tag.
	ActionPrePush Action = "push.check"

	// Actions only the person takes.
	ActionDecisionAnswer Action = "decision.answer"
	ActionNextChoose     Action = "next.choose"
	ActionWidenScope     Action = "lane.widen"
	ActionKeepGoingOn    Action = "keep-going.on"
	ActionBriefApprove   Action = "brief.approve"
	ActionChecksApprove  Action = "checks.approve"
	ActionChecksRelease  Action = "checks.release"
)

// RouteAction is the action for calling the route a mux pattern names.
func RouteAction(pattern string) Action { return Action("route " + pattern) }

// Built-in rule ids. They are stable: later layers and logs refer to them.
const (
	RuleKeepGoingLimits = "builtin/keep-going-limits"
	RulePushAutonomy    = "builtin/push-autonomy"
	RuleMergeAutonomy   = "builtin/merge-autonomy"
	RulePersonAnswers   = "builtin/person-answers"
	RuleDailyBudget     = "builtin/daily-budget"
	RuleBudget          = "builtin/budget"
	RuleRunCaps         = "builtin/run-caps"
	RuleScopePush       = "builtin/scope-push"
	RuleRouteRoles      = "builtin/route-roles"
	// RuleDefault is the verdict when no stage had a say: today, proceed.
	RuleDefault = "builtin/default"
)

// Autonomy values, as the repo profile spells them (config.Shepherd and config.Agent).
const (
	AutonomyShepherd = "shepherd"
	AutonomyAgent    = "agent"
)

// Request is a question for Decide.
type Request struct {
	Action Action
	Actor  Actor
	Facts  Facts
}

// Facts is what the caller found out. A field is read only by the action that needs it.
type Facts struct {
	// KeepGoing checks keep-going's limits for a front desk acting in an event turn and
	// counts the start it allows; ok false refuses with reason.
	KeepGoing func(ctx context.Context) (ok bool, reason string)

	// Budget is why an automatic run must wait, or "". BudgetDaily says it is the
	// workspace's daily ceiling.
	Budget      string
	BudgetDaily bool

	// Runs are the run counts for a start.
	Runs *RunCounts

	// Route is a route's role check.
	Route *RouteFacts

	// Problems are the push checks a lane's push failed.
	Problems []string

	// Repo autonomy, as the profile spells it, for push, ship, tag and merge.
	Push, Tag, Merge string
	TagsReserved     bool
	GitLab           bool
	// ToOrigin: the push goes to the repo's origin.
	ToOrigin bool
	// Repo, Lane, Agent, Remote and Run name what the reasons talk about.
	Repo, Lane, Agent, Remote string
	Run                       int64
}

// RunCounts are the runs going and the limits on them.
type RunCounts struct {
	// Running is runs going on the machine, Max the daemon's limit.
	Running, Max int
	// Repo names the repo; InRepo is runs going in it and RepoMax its limit (0: none).
	Repo            string
	InRepo, RepoMax int
}

// Denial says why a role may not call a route.
type Denial string

const (
	DeniedNone   Denial = ""
	DeniedDesk   Denial = "desk"
	DeniedWeb    Denial = "web"
	DeniedWorker Denial = "worker"
	// DeniedOtherRun: a worker named a run that is not its own (Facts.Run is its own).
	DeniedOtherRun Denial = "other-run"
)

// RouteFacts is the outcome of matching a caller's role against a route's access mask.
type RouteFacts struct {
	Denied Denial
	Run    int64
}

// Verdict is the answer.
type Verdict struct {
	Kind Kind
	// Rule is the id of the rule that decided; every verdict has one.
	Rule string
	// Reason is the text for a person.
	Reason string
	// Stage is the stage that decided: one of Stages.
	Stage string
}

// OK reports whether the action may go ahead.
func (v Verdict) OK() bool { return v.Kind == Proceed }

type stage struct {
	name string
	run  func(context.Context, Request) (Verdict, bool)
}

// stages is the evaluation order. A stage returns false when it has nothing to say.
var stages = []stage{
	{"floor", floor},
	{"limits", limits},
	{"guardrails", guardrails},
	{"classify", classify},
	{"policy", policy},
	{"default", defaults},
}

// Decide answers whether the action may be done now. The first stage with something to
// say decides.
func Decide(ctx context.Context, req Request) Verdict {
	for _, s := range stages {
		if v, ok := s.run(ctx, req); ok {
			v.Stage = s.name
			return v
		}
	}
	return Verdict{Kind: Proceed, Rule: RuleDefault, Stage: "default"}
}

// Stages are the stage names in evaluation order.
func Stages() []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = s.name
	}
	return out
}

func refuse(rule, format string, a ...any) (Verdict, bool) {
	return Verdict{Kind: Refuse, Rule: rule, Reason: fmt.Sprintf(format, a...)}, true
}

func hold(rule, format string, a ...any) (Verdict, bool) {
	return Verdict{Kind: Hold, Rule: rule, Reason: fmt.Sprintf(format, a...)}, true
}

func proceed(rule string) (Verdict, bool) { return Verdict{Kind: Proceed, Rule: rule}, true }

// personOnly is what only the person does, and what is said to anyone else.
var personOnly = map[Action]string{
	ActionDecisionAnswer: "the front desk may record an answer only in a turn the person started; bring the decision to them",
	ActionNextChoose:     "the front desk may approve, defer, drop, finish or reorder an item only in a turn the person started; propose it and bring it to them",
	ActionWidenScope:     "the front desk may widen a lane's scope only in a turn the person started; bring the request to them",
	ActionKeepGoingOn:    "keep-going is turned on by the person: not by an agent, and by the front desk only in a turn the person started; off is always allowed",
	ActionBriefApprove:   "the front desk may approve a brief only in a turn the person started; bring the draft to them",
	ActionChecksApprove:  "approving check files is the person's call: an agent never does, and the front desk only in a turn the person started, citing the decision in which they said so",
	ActionChecksRelease:  "releasing approved check files is the person's call: an agent raises a decision (ask_human) saying which check is wrong and why; the front desk releases only in a turn the person started, citing the decision in which they said so",
}

// PersonOnlyReason is the text said when anyone but the person takes a person-only
// action, or "" for an action that is not one.
func PersonOnlyReason(a Action) string { return personOnly[a] }

// floor holds what nothing may override: the person's own actions, the role masks of
// routes, and the checks a push must pass.
func floor(_ context.Context, req Request) (Verdict, bool) {
	if why, ok := personOnly[req.Action]; ok && req.Actor != ActorPerson {
		return refuse(RulePersonAnswers, "%s", why)
	}
	if r := req.Facts.Route; r != nil {
		switch r.Denied {
		case DeniedDesk:
			return refuse(RuleRouteRoles, "the front desk's token cannot do this; it is the person's")
		case DeniedWeb:
			return refuse(RuleRouteRoles, "a browser session cannot do this; use the shepherd command")
		case DeniedWorker:
			return refuse(RuleRouteRoles, "an agent's token cannot do this; ask the person (ask_human) or another lane (ask_shepherd)")
		case DeniedOtherRun:
			return refuse(RuleRouteRoles, "an agent's token is for run %d only", r.Run)
		}
	}
	if len(req.Facts.Problems) > 0 {
		return refuse(RuleScopePush, "%s", strings.Join(req.Facts.Problems, "\n"))
	}
	return Verdict{}, false
}

// limits holds what is spent or used up, in a fixed order: keep-going's limits for a
// front desk acting unasked, the budgets, then the run caps.
func limits(ctx context.Context, req Request) (Verdict, bool) {
	f := req.Facts
	if f.KeepGoing != nil && req.Actor == ActorDesk {
		if ok, why := f.KeepGoing(ctx); !ok {
			return refuse(RuleKeepGoingLimits, "%s", why)
		}
		return proceed(RuleKeepGoingLimits)
	}
	if f.Budget != "" {
		rule := RuleBudget
		if f.BudgetDaily {
			rule = RuleDailyBudget
		}
		return hold(rule, "%s", f.Budget)
	}
	if c := f.Runs; c != nil {
		if c.Running >= c.Max {
			return hold(RuleRunCaps, "%d agents are already running, the limit on this machine (daemon.max_runs)", c.Max)
		}
		if c.RepoMax > 0 && c.InRepo >= c.RepoMax {
			return hold(RuleRunCaps, "%d agents are already running in %s, its limit (max_runs)", c.InRepo, c.Repo)
		}
		return proceed(RuleRunCaps)
	}
	return Verdict{}, false
}

// guardrails and classify are the stages later steps fill in; they decide nothing yet.
func guardrails(context.Context, Request) (Verdict, bool) { return Verdict{}, false }
func classify(context.Context, Request) (Verdict, bool)   { return Verdict{}, false }

// policy holds the built-in rules that read the repo's autonomy settings.
func policy(_ context.Context, req Request) (Verdict, bool) {
	f := req.Facts
	switch req.Action {
	case ActionPush:
		if f.Push != AutonomyAgent {
			return refuse(RulePushAutonomy, "agent run %d (%s) is going in lane %s; agents Shepherd starts never push. Review the lane's commits when it ends, then push yourself",
				f.Run, f.Agent, f.Lane)
		}
		if !f.ToOrigin {
			return refuse(RulePushAutonomy, "agent run %d (%s) in lane %s may push only to the repo's origin; this pushes to %q",
				f.Run, f.Agent, f.Lane, f.Remote)
		}
		return proceed(RulePushAutonomy)
	case ActionShip:
		if f.Push != AutonomyShepherd {
			return refuse(RulePushAutonomy, "%s has not opted in to Shepherd pushing (its profile's autonomy.push is %q, not %q): push lane %s yourself",
				f.Repo, f.Push, AutonomyShepherd, f.Lane)
		}
		return proceed(RulePushAutonomy)
	case ActionTagPush:
		if f.Push != AutonomyShepherd || f.Tag != AutonomyAgent || !f.TagsReserved {
			return refuse(RulePushAutonomy, "%s lets Shepherd push a reserved tag only with push: shepherd, tag: agent and reserved tags", f.Repo)
		}
		return proceed(RulePushAutonomy)
	case ActionMergeArm:
		if f.Merge != AutonomyAgent {
			return refuse(RuleMergeAutonomy, "%s has not let agents arm a merge (autonomy.merge is %q, not %q)", f.Repo, f.Merge, AutonomyAgent)
		}
		if !f.GitLab {
			return refuse(RuleMergeAutonomy, "arming a merge is a GitLab command, and %s is not on GitLab", f.Repo)
		}
		return proceed(RuleMergeAutonomy)
	}
	return Verdict{}, false
}

// defaults is the last stage. Nothing here yet: an action no earlier stage objected to
// goes ahead, as it does today.
func defaults(context.Context, Request) (Verdict, bool) { return proceed(RuleDefault) }
