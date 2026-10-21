package crook

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

// Evaluate answers a Query from the effective rule files, in the order crook/1 gives:
// floor, limits, guardrails, classify, policy, default. It is not what Decide runs:
// Decide keeps today's built-in answers (its default is proceed, the format's is ask)
// until a later step moves them into the built-in layer.

// Limits are the machine limits a Query carries, already read by the caller.
type Limits struct {
	// RunsFree is the number of run slots free; nil means not checked.
	RunsFree *int `json:"runs_free,omitempty"`
}

// Query is a question for Evaluate. Action is a crook/1 action name.
type Query struct {
	Action string
	Actor  Actor
	// Facts are the named values of the format (paths, gate, pipeline, ...).
	Facts  map[string]any
	Limits Limits
	// Now is the local time the time matcher reads; zero means now.
	Now time.Time
	// RulesGlobs are extra path globs the floor treats as a rules folder, beyond
	// .shepherd/**: the paths of a workspace or project layer's rules folder.
	RulesGlobs []string
}

// ShadowResult is what a shadow rule would have said.
type ShadowResult struct {
	Rule    string `json:"rule"`
	Layer   string `json:"layer"`
	Verdict Kind   `json:"verdict"`
}

// Outcome is Evaluate's answer.
type Outcome struct {
	Verdict Kind
	// Rule is the deciding rule's id: a file rule's, builtin/<name> or floor/<name>.
	Rule string
	// Layer is the deciding file rule's layer, or "" for a built-in or the floor.
	Layer  string
	Reason string
	Stage  string
	// Classes are the classes the classifiers added.
	Classes []string
	// Answer is what a matching standing answer or policy would answer a decision with.
	Answer string
	// Shadow lists the shadow rules that applied.
	Shadow []ShadowResult
}

// Floor and built-in ids Evaluate cites.
const (
	FloorRulesFolder = "floor/rules-folder"
	RulePerson       = "builtin/person"
)

type candidate struct {
	rule    *FileRule
	verdict Kind
	reason  string
}

// better reports whether c should win over d: more restrictive, and on a tie the later
// layer.
func (c candidate) better(d candidate) bool {
	if rank(c.verdict) != rank(d.verdict) {
		return rank(c.verdict) > rank(d.verdict)
	}
	return layerIndex(c.rule.Layer) > layerIndex(d.rule.Layer)
}

func pick(cs []candidate) (candidate, bool) {
	if len(cs) == 0 {
		return candidate{}, false
	}
	best := cs[0]
	for _, c := range cs[1:] {
		if c.better(best) {
			best = c
		}
	}
	return best, true
}

// applies reports whether a rule covers the action and actor.
func (r *FileRule) applies(q Query) bool {
	if len(r.Actions) > 0 && !slices.Contains(r.Actions, q.Action) {
		return false
	}
	return slices.Contains(r.Actors, q.Actor)
}

// Evaluate decides q under the rules in set.
func Evaluate(set RuleSet, q Query) Outcome {
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	paths, _ := factStrings(q.Facts, "paths")
	// Paths are cleaned and folded to lower case: on a case-insensitive disk
	// .SHEPHERD/ is the same folder.
	globs := []string{".shepherd/**"}
	for _, g := range q.RulesGlobs {
		globs = append(globs, strings.ToLower(g))
	}
	rulesFolder := slices.ContainsFunc(paths, func(p string) bool {
		return globMatch(globs, strings.ToLower(path.Clean(strings.ReplaceAll(p, "\\", "/"))))
	})

	out := evaluate(set, q, rulesFolder)
	// A change to a rules folder is at least ask; where the rules would have said ask or
	// less, the floor is what is cited.
	if rulesFolder && rank(out.Verdict) <= rank(Ask) {
		out.Verdict, out.Rule, out.Layer, out.Stage = Ask, FloorRulesFolder, "", "floor"
		out.Reason = "a change to a rules folder waits for the person"
		out.Answer = ""
	}
	return out
}

func evaluate(set RuleSet, q Query, rulesFolder bool) Outcome {
	if q.Actor == ActorPerson {
		return Outcome{Verdict: Proceed, Rule: RulePerson, Stage: "person", Reason: "the person is the authority"}
	}
	var out Outcome

	if q.Action == "start_run" && q.Limits.RunsFree != nil && *q.Limits.RunsFree <= 0 {
		out = Outcome{Verdict: Hold, Rule: RuleRunCaps, Stage: "limits", Reason: "no run slot is free; the start waits for one"}
		out.Shadow = shadowRules(set, q, nil, rulesFolder)
		return out
	}

	var guards []candidate
	for _, r := range set.Rules {
		if r.Kind == KindGuardrail && r.Mode == ModeLive && r.applies(q) && r.When.matches(q.Facts, nil, q.Now) {
			guards = append(guards, candidate{r, r.Then, r.Statement})
		}
	}
	if c, ok := pick(guards); ok {
		out = finish(c, "guardrails")
		out.Shadow = shadowRules(set, q, nil, rulesFolder)
		return out
	}

	var classes []string
	for _, r := range set.Rules {
		if r.Kind != KindClassifier || r.Mode != ModeLive || !r.applies(q) {
			continue
		}
		if (r.AppliesTo == "decision") != (q.Action == "answer_decision") {
			continue
		}
		// Classifiers see the facts only, never each other's classes, so the result does
		// not depend on the order they are read in (a class matcher in one is a load error).
		if r.When.matches(q.Facts, nil, q.Now) && !slices.Contains(classes, r.Class) {
			classes = append(classes, r.Class)
		}
	}

	var cands []candidate
	for _, r := range set.Rules {
		if c, ok := policyCandidate(r, q, classes, rulesFolder); ok && r.Mode == ModeLive {
			cands = append(cands, c)
		}
	}
	if c, ok := pick(cands); ok {
		out = finish(c, "policy")
	} else {
		out = Outcome{Verdict: Ask, Rule: RuleDefault, Stage: "default",
			Reason: "no rule covers this, so the person decides"}
	}
	out.Classes = classes
	out.Shadow = shadowRules(set, q, classes, rulesFolder)
	return out
}

func finish(c candidate, stage string) Outcome {
	o := Outcome{Verdict: c.verdict, Rule: c.rule.ID, Layer: c.rule.Layer, Stage: stage, Reason: c.reason}
	if c.rule.Answer != "" {
		o.Answer = c.rule.Answer
	}
	return o
}

// policyCandidate is what a policy, standing-answer or merge rule says, if it applies.
// A rules-folder change never meets a merge rule.
func policyCandidate(r *FileRule, q Query, classes []string, rulesFolder bool) (candidate, bool) {
	if !r.applies(q) || !r.When.matches(q.Facts, classes, q.Now) {
		return candidate{}, false
	}
	switch r.Kind {
	case KindMerge:
		if q.Action != "merge" || rulesFolder {
			return candidate{}, false
		}
		v, why := mergeVerdict(r, q.Facts)
		return candidate{r, v, why}, true
	case KindPolicy:
		if r.Class != "" && !slices.Contains(classes, r.Class) {
			return candidate{}, false
		}
		return candidate{r, r.Then, r.Statement}, true
	case KindStandingAnswer:
		return candidate{r, Proceed, r.Statement}, true
	}
	return candidate{}, false
}

// shadowRules reports what each applicable shadow rule would have said. Classes is nil
// for the stages reached before classify.
func shadowRules(set RuleSet, q Query, classes []string, rulesFolder bool) []ShadowResult {
	var out []ShadowResult
	for _, r := range set.Rules {
		if r.Mode != ModeShadow {
			continue
		}
		switch r.Kind {
		case KindGuardrail:
			if r.applies(q) && r.When.matches(q.Facts, nil, q.Now) {
				out = append(out, ShadowResult{r.ID, r.Layer, r.Then})
			}
		case KindMerge, KindPolicy, KindStandingAnswer:
			if c, ok := policyCandidate(r, q, classes, rulesFolder); ok {
				out = append(out, ShadowResult{r.ID, r.Layer, c.verdict})
			}
		}
	}
	return out
}

// mergeVerdict applies a merge rule's conditions, size and path limits. When they all
// hold the rule gives its own verdict. When something is short it gives hold if only
// things that change by themselves are short (a running pipeline, a base that moved),
// and ask otherwise.
func mergeVerdict(r *FileRule, f map[string]any) (Kind, string) {
	var failed []string
	changes := true // every shortfall so far can clear without anyone acting
	fail := func(what string, clears bool) {
		failed = append(failed, what)
		changes = changes && clears
	}
	str := func(k string) string { s, _ := factString(f, k); return s }
	flag := func(k string) (bool, bool) { return factBool(f, k) }
	for _, c := range r.Require {
		switch c {
		case "linked_requirement":
			ids, _ := factStrings(f, "requirement")
			if linked, _ := flag("item_linked"); !linked || len(ids) == 0 {
				fail(c, false)
			}
		case "approved_checks_pass":
			if str("approved_checks") != "pass" {
				fail(c, false)
			}
		case "no_test_edits":
			t, tok := flag("tests_edited")
			cf, cok := flag("check_files_edited")
			if !tok || !cok || t || cf {
				fail(c, false)
			}
		case "gate_green":
			if str("gate") != "green" {
				fail(c, false)
			}
		case "pipeline_green":
			if p := str("pipeline"); p != "green" {
				fail(c, p == "running")
			}
		case "ai_review_not_blocking":
			if str("ai_review") != "ok" {
				fail(c, false)
			}
		case "up_to_date":
			if ok, present := flag("up_to_date"); !ok {
				fail(c, present)
			}
		case "no_open_threads":
			if n, ok := factNumber(f, "open_threads"); !ok || n != 0 {
				fail(c, false)
			}
		}
	}
	if r.MaxChangedLines != nil {
		if n, ok := factNumber(f, "changed_lines"); !ok || n > float64(*r.MaxChangedLines) {
			fail(fmt.Sprintf("max_changed_lines %d", *r.MaxChangedLines), false)
		}
	}
	if len(r.PathsNot) > 0 {
		ps, ok := factStrings(f, "paths")
		if !ok || slices.ContainsFunc(ps, func(p string) bool { return globMatch(r.PathsNot, p) }) {
			fail("paths_not", false)
		}
	}
	if len(failed) == 0 {
		return r.Then, r.Statement
	}
	why := fmt.Sprintf("%s Not yet met: %s.", r.Statement, strings.Join(failed, ", "))
	if changes {
		return Hold, why
	}
	return Ask, why
}
