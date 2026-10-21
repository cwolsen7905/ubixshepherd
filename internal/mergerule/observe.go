package mergerule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ubixsys/ubixshepherd/internal/crook"
)

// Stages of the rows this package records in Crook's verdict log. A row with either is
// evidence, not an act: counts and lists keep them apart from what Shepherd did.
const (
	// StageShadow is what a shadow rule would have done for a merge request's head.
	StageShadow = "shadow"
	// StageOutcome is what happened to the merge request afterwards.
	StageOutcome = "shadow-outcome"
	// ActionMerge is the action a would-have row records; ActionOutcome the outcome row's.
	ActionMerge   = "merge"
	ActionOutcome = "merge.outcome"
)

// IsShadowStage reports whether a verdict row's stage marks it as shadow evidence.
func IsShadowStage(stage string) bool {
	return stage == StageShadow || stage == StageOutcome
}

// Feed puts a line in the lane's feed.
type Feed func(ctx context.Context, lane, msg string)

// Observer is the shadow pass. The zero value is not usable: set Rec, and Base per call
// in Input. Feed and Log may be nil.
type Observer struct {
	Rec  crook.Recorder
	Feed Feed
	Log  *slog.Logger

	Rules Loader

	mu     sync.Mutex
	last   map[string]string // lane key -> state key of the last row written
	fed    map[string]string // lane key -> head that already had its feed line
	failed map[string]string // repo dir -> base commit whose rules failed to load
}

// Result is what a shadow rule said for one head.
type Result struct {
	Rule    string
	Layer   string
	Verdict crook.Kind
	Reason  string
	// Recorded is false when this repeats the last row for the lane.
	Recorded bool
}

// Observe asks what each shadow merge rule would have done for in and records it. It
// returns nil when the repo has no shadow merge rule. It writes a row only when the
// head, the verdict or the unmet conditions changed since the last row for the lane,
// and a feed line the first time a head would have merged. It makes no forge or git
// change of any kind.
func (o *Observer) Observe(ctx context.Context, in Input) (*Result, error) {
	rules, err := o.Rules.Load(ctx, in.Dir, in.Base)
	if err != nil {
		return nil, o.loadFailed(in, err)
	}
	if len(rules.Set.Errors) > 0 {
		err := o.loadFailed(in, fmt.Errorf("%v", rules.Set.Errors[0]))
		if err != nil {
			o.logf("%v", err)
		}
	}
	facts := Facts(ctx, in)
	out := crook.Evaluate(rules.Set, crook.Query{Action: "merge", Actor: crook.ActorDaemon, Facts: facts})
	var res *Result
	for _, s := range out.Shadow {
		if !o.isMerge(rules.Set, s.Rule) {
			continue
		}
		res = &Result{Rule: s.Rule, Layer: s.Layer, Verdict: s.Verdict, Reason: o.reason(rules.Set, s.Rule, facts)}
		break // one merge rule is observed per request: the first applicable
	}
	if res == nil && out.Stage == "floor" {
		// A change to a rules folder never meets a merge rule: the floor asks. Say so for
		// the first shadow merge rule, so the record shows the request.
		for _, r := range rules.Set.Rules {
			if r.Kind == crook.KindMerge && r.Mode == crook.ModeShadow {
				res = &Result{Rule: r.ID, Layer: r.Layer, Verdict: out.Verdict, Reason: out.Reason}
				break
			}
		}
	}
	if res == nil {
		return nil, nil
	}
	key := fmt.Sprintf("%s|%s|%s|%s", in.Head, res.Rule, res.Verdict, res.Reason)
	lane := in.Repo + "/" + in.Lane
	o.mu.Lock()
	same := o.last[lane] == key
	o.mu.Unlock()
	if same {
		return res, nil
	}
	f, _ := json.Marshal(withCommit(facts, rules.Commit))
	if o.Rec != nil {
		err := o.Rec.Record(context.WithoutCancel(ctx), crook.Entry{
			Action: ActionMerge, Actor: crook.ActorDaemon, Kind: res.Verdict, Rule: res.Rule,
			RuleVersion: crook.RuleVersion, Stage: StageShadow, Reason: res.Reason,
			Repo: in.Repo, Lane: in.Lane, Facts: string(f),
		})
		if err != nil {
			// The key is not kept, so the next poll tries the row again.
			return res, fmt.Errorf("record shadow verdict: %w", err)
		}
	}
	res.Recorded = true
	o.mu.Lock()
	if o.last == nil {
		o.last = map[string]string{}
		o.fed = map[string]string{}
	}
	o.last[lane] = key
	// The feed line is once per head: a head that goes proceed, ask, proceed again does
	// not say it twice.
	feed := res.Verdict == crook.Proceed && o.Feed != nil && o.fed[lane] != in.Head
	if feed {
		o.fed[lane] = in.Head
	}
	o.mu.Unlock()
	if feed {
		o.Feed(ctx, in.Lane, fmt.Sprintf("rule %s would have merged !%d (%s) at %s (shadow mode: not merged)", res.Rule, in.MR, in.MRURL, short(in.Head)))
	}
	return res, nil
}

// Merged records that the person merged a request a shadow rule had an opinion on, so a
// later reading can compare what the rule said with what happened. It writes nothing
// when the rule never observed the lane. mergeSHA is the commit that landed it.
func (o *Observer) Merged(ctx context.Context, in Input, mergeSHA string) error {
	lane := in.Repo + "/" + in.Lane
	o.mu.Lock()
	key, ok := o.last[lane]
	delete(o.last, lane)
	delete(o.fed, lane)
	o.mu.Unlock()
	if !ok || o.Rec == nil {
		return nil
	}
	parts := strings.SplitN(key, "|", 4)
	if len(parts) < 4 {
		return nil
	}
	return o.Rec.Record(context.WithoutCancel(ctx), crook.Entry{
		Action: ActionOutcome, Actor: crook.ActorDaemon, Kind: crook.Ask, Rule: parts[1],
		RuleVersion: crook.RuleVersion, Stage: StageOutcome,
		Reason: fmt.Sprintf("the person merged !%d at %s; the rule had said %s for %s", in.MR, short(mergeSHA), parts[2], short(parts[0])),
		Repo:   in.Repo, Lane: in.Lane,
	})
}

func (o *Observer) loadFailed(in Input, err error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failed == nil {
		o.failed = map[string]string{}
	}
	if o.failed[in.Dir] == err.Error() {
		return nil // already said once
	}
	o.failed[in.Dir] = err.Error()
	return fmt.Errorf("merge rules for %s: %w", in.Repo, err)
}

func (o *Observer) isMerge(set crook.RuleSet, id string) bool {
	for _, r := range set.Rules {
		if r.ID == id {
			return r.Kind == crook.KindMerge && r.Mode == crook.ModeShadow
		}
	}
	return false
}

// reason says which conditions are unmet, in the words of the rule file, by asking the
// evaluator again as if the rule were live.
func (o *Observer) reason(set crook.RuleSet, id string, facts map[string]any) string {
	for _, r := range set.Rules {
		if r.ID != id {
			continue
		}
		live := *r
		live.Mode = crook.ModeLive
		out := crook.Evaluate(crook.RuleSet{Rules: []*crook.FileRule{&live}}, crook.Query{Action: "merge", Actor: crook.ActorDaemon, Facts: facts})
		return out.Reason
	}
	return ""
}

// withCommit is the facts plus the commit the rules came from, for the log.
func withCommit(f map[string]any, rulesCommit string) map[string]any {
	out := make(map[string]any, len(f)+1)
	for k, v := range f {
		out[k] = v
	}
	out["rules_commit"] = rulesCommit
	return out
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (o *Observer) logf(format string, a ...any) {
	if o.Log != nil {
		o.Log.Warn(fmt.Sprintf(format, a...))
	}
}
