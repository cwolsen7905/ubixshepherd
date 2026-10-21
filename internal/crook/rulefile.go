package crook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file reads rule files as the crook/1 rule format defines them: YAML front matter,
// a one-paragraph statement, then an optional "## Why". Reading is strict: a file with
// anything unknown in it is a load error and is not used.

// RuleKind is what a rule file is.
type RuleKind string

const (
	KindGuardrail      RuleKind = "guardrail"
	KindClassifier     RuleKind = "classifier"
	KindPolicy         RuleKind = "policy"
	KindStandingAnswer RuleKind = "standing-answer"
	KindMerge          RuleKind = "merge"
)

// Mode says whether a rule's verdict is used.
type Mode string

const (
	ModeLive   Mode = "live"
	ModeShadow Mode = "shadow"
)

// Actors the format names (the package's own Actor type covers only Shepherd's four).
const (
	ActorPeer   Actor = "peer"
	ActorKelpie Actor = "kelpie"
)

var (
	ruleKinds = []RuleKind{KindGuardrail, KindClassifier, KindPolicy, KindStandingAnswer, KindMerge}
	// formatActions are the actions crook/1 defines.
	formatActions = []string{"start_run", "open_lane", "widen_scope", "push", "open_mr", "merge", "tag",
		"deploy", "answer_decision", "approve_item", "reorder_items", "spend", "close_question", "upkeep_fix"}
	// toolPrefixes are the prefixes a tool may add actions under.
	toolPrefixes   = []string{"kelpie"}
	formatActors   = []Actor{ActorPerson, ActorDesk, ActorDaemon, ActorWorker, ActorPeer, ActorKelpie}
	decisionKinds  = []string{"gate_failure", "pipeline_failure", "review_thread", "scope", "plan", "other"}
	mergeRequires  = []string{"linked_requirement", "approved_checks_pass", "no_test_edits", "gate_green", "pipeline_green", "ai_review_not_blocking", "up_to_date", "no_open_threads"}
	redActions     = []string{"revert_and_ask", "ask"}
	ruleIDPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	classPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	timeWindowExpr = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])-([01][0-9]|2[0-3]):([0-5][0-9])$`)
)

// ValidRuleID reports whether s can be a rule id.
func ValidRuleID(s string) bool { return ruleIDPattern.MatchString(s) }

// strList reads a YAML scalar or list as a list, so `repo: a` and `repo: [a]` agree.
type strList []string

func (l *strList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := n.Decode(&s); err != nil {
			return err
		}
		*l = strList{s}
	case yaml.SequenceNode:
		var out []string
		if err := n.Decode(&out); err != nil {
			return err
		}
		*l = out
	default:
		return fmt.Errorf("line %d: expected a name or a list of names", n.Line)
	}
	return nil
}

// When holds the matchers of crook/1. Every one set must match.
type When struct {
	Repo              strList  `yaml:"repo"`
	Paths             strList  `yaml:"paths"`
	PathsAll          strList  `yaml:"paths_all"`
	Branch            strList  `yaml:"branch"`
	Class             strList  `yaml:"class"`
	DecisionAbout     strList  `yaml:"decision_about"`
	FailingChecksOnly strList  `yaml:"failing_checks_only"`
	ChangedLinesMax   *int     `yaml:"changed_lines_max"`
	CostUSDMax        *float64 `yaml:"cost_usd_max"`
	Time              string   `yaml:"time"`
	ItemLinked        *bool    `yaml:"item_linked"`
	Requirement       strList  `yaml:"requirement"`
}

// FileRule is one rule file after loading.
type FileRule struct {
	// ID is the file name without .md; Layer names the layer it came from.
	ID, Layer string
	Kind      RuleKind
	Mode      Mode
	// ModeSet is true when the file wrote a mode.
	ModeSet  bool
	Disabled bool
	Actions  []string
	Actors   []Actor
	When     When
	Then     Kind
	// Classifier fields.
	AppliesTo, Class string
	// Policy and standing-answer: Answer is "recommendation", an option number or text.
	Answer string
	// Merge fields.
	Require         []string
	MaxChangedLines *int
	PathsNot        []string
	OnRedAfterMerge string
	Statement, Why  string
	SHA             string
	// Warnings are things valid but suspect, such as a loosening rule written live.
	Warnings []string
}

// ruleFront is the front matter as written.
type ruleFront struct {
	Kind            string   `yaml:"kind"`
	Mode            string   `yaml:"mode"`
	Disabled        bool     `yaml:"disabled"`
	Actions         strList  `yaml:"actions"`
	Actor           strList  `yaml:"actor"`
	When            When     `yaml:"when"`
	Then            string   `yaml:"then"`
	AppliesTo       string   `yaml:"applies_to"`
	Class           string   `yaml:"class"`
	Answer          *answer  `yaml:"answer"`
	Require         []string `yaml:"require"`
	MaxChangedLines *int     `yaml:"max_changed_lines"`
	PathsNot        []string `yaml:"paths_not"`
	OnRedAfterMerge string   `yaml:"on_red_after_merge"`
}

// answer reads `answer:` whether it is written as text or a number.
type answer string

func (a *answer) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: answer must be text or an option number", n.Line)
	}
	*a = answer(n.Value)
	return nil
}

// ParseRule reads one rule file. id is the file name without .md; layer names its layer.
func ParseRule(layer, id string, data []byte) (*FileRule, error) {
	if !ValidRuleID(id) {
		return nil, fmt.Errorf("%q is not a rule id: use lowercase words joined by single hyphens", id)
	}
	front, body, err := splitFront(data)
	if err != nil {
		return nil, err
	}
	var f ruleFront
	dec := yaml.NewDecoder(bytes.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("front matter: %w", err)
	}
	sum := sha256.Sum256(data)
	r := &FileRule{ID: id, Layer: layer, Disabled: f.Disabled, SHA: hex.EncodeToString(sum[:])}
	r.Statement, r.Why = splitStatement(body)
	if f.Disabled {
		// A switch-off names only the id it removes; the rest is not read.
		r.Kind = RuleKind(f.Kind)
		return r, nil
	}
	if err := r.fill(f); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *FileRule) fill(f ruleFront) error {
	if f.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	r.Kind = RuleKind(f.Kind)
	if !slices.Contains(ruleKinds, r.Kind) {
		return fmt.Errorf("unknown kind %q", f.Kind)
	}
	r.When = f.When
	if err := r.When.validate(); err != nil {
		return err
	}
	switch Mode(f.Mode) {
	case "":
	case ModeLive, ModeShadow:
		r.Mode, r.ModeSet = Mode(f.Mode), true
	default:
		return fmt.Errorf("unknown mode %q: use live or shadow", f.Mode)
	}
	// The answering rules and merge rules start in shadow unless written live.
	answers := r.Kind == KindStandingAnswer || (r.Kind == KindPolicy && f.Answer != nil)
	if !r.ModeSet {
		r.Mode = ModeLive
		if r.Kind == KindMerge || answers {
			r.Mode = ModeShadow
		}
	} else if r.Mode == ModeLive && (r.Kind == KindMerge || answers) {
		r.Warnings = append(r.Warnings, "this rule loosens what Shepherd may do and is written live from the start; shadow lets you check it first")
	}

	if f.Then != "" {
		r.Then = Kind(f.Then)
		if !slices.Contains([]Kind{Proceed, Ask, Hold, Refuse}, r.Then) {
			return fmt.Errorf("unknown verdict %q in then", f.Then)
		}
	}
	for _, a := range f.Actions {
		if !validFormatAction(a) {
			return fmt.Errorf("unknown action %q", a)
		}
	}
	r.Actions = f.Actions
	for _, a := range f.Actor {
		if !slices.Contains(formatActors, Actor(a)) {
			return fmt.Errorf("unknown actor %q", a)
		}
		r.Actors = append(r.Actors, Actor(a))
	}
	if len(r.Actors) == 0 {
		for _, a := range formatActors {
			if a != ActorPerson {
				r.Actors = append(r.Actors, a)
			}
		}
	}

	var stray []string
	set := func(name string, on bool) {
		if on {
			stray = append(stray, name)
		}
	}
	switch r.Kind {
	case KindClassifier:
		set("then", f.Then != "")
		set("answer", f.Answer != nil)
		set("require", f.Require != nil)
		set("max_changed_lines", f.MaxChangedLines != nil)
		set("paths_not", f.PathsNot != nil)
		set("on_red_after_merge", f.OnRedAfterMerge != "")
		if len(r.When.Class) > 0 {
			return fmt.Errorf("a classifier cannot match on class: classifiers see the facts, not each other's classes")
		}
		if f.AppliesTo != "action" && f.AppliesTo != "decision" {
			return fmt.Errorf("a classifier needs applies_to: action or decision")
		}
		if !classPattern.MatchString(f.Class) {
			return fmt.Errorf("a classifier needs class: one lowercase name")
		}
		r.AppliesTo, r.Class = f.AppliesTo, f.Class
	case KindGuardrail:
		if len(r.When.Class) > 0 {
			return fmt.Errorf("a guardrail cannot match on class: classes are not known until after the guardrail stage")
		}
		set("applies_to", f.AppliesTo != "")
		set("class", f.Class != "")
		set("answer", f.Answer != nil)
		set("require", f.Require != nil)
		set("max_changed_lines", f.MaxChangedLines != nil)
		set("paths_not", f.PathsNot != nil)
		set("on_red_after_merge", f.OnRedAfterMerge != "")
		if r.Then == "" {
			return fmt.Errorf("then is required")
		}
		if r.Then == Proceed {
			return fmt.Errorf("a guardrail cannot give proceed: it exists to hold things back")
		}
	case KindPolicy:
		set("applies_to", f.AppliesTo != "")
		set("require", f.Require != nil)
		set("max_changed_lines", f.MaxChangedLines != nil)
		set("paths_not", f.PathsNot != nil)
		set("on_red_after_merge", f.OnRedAfterMerge != "")
		if r.Then == "" {
			return fmt.Errorf("then is required")
		}
		if err := r.classAndAnswer(f); err != nil {
			return err
		}
		if f.Answer != nil && (len(f.Actions) != 1 || f.Actions[0] != "answer_decision") {
			return fmt.Errorf("answer is only for the answer_decision action")
		}
	case KindStandingAnswer:
		set("class", f.Class != "")
		set("applies_to", f.AppliesTo != "")
		set("then", f.Then != "")
		set("require", f.Require != nil)
		set("max_changed_lines", f.MaxChangedLines != nil)
		set("paths_not", f.PathsNot != nil)
		set("on_red_after_merge", f.OnRedAfterMerge != "")
		if len(r.When.DecisionAbout) == 0 {
			return fmt.Errorf("a standing answer needs decision_about in when")
		}
		if f.Answer == nil || strings.TrimSpace(string(*f.Answer)) == "" {
			return fmt.Errorf("a standing answer needs an answer")
		}
		r.Answer = string(*f.Answer)
		r.Then = Proceed
	case KindMerge:
		set("applies_to", f.AppliesTo != "")
		set("class", f.Class != "")
		set("answer", f.Answer != nil)
		if r.Then == "" {
			return fmt.Errorf("then is required")
		}
		for _, c := range f.Require {
			if !slices.Contains(mergeRequires, c) {
				return fmt.Errorf("unknown merge condition %q", c)
			}
		}
		if f.OnRedAfterMerge != "" && !slices.Contains(redActions, f.OnRedAfterMerge) {
			return fmt.Errorf("unknown on_red_after_merge %q", f.OnRedAfterMerge)
		}
		for _, g := range f.PathsNot {
			if err := checkGlob(g); err != nil {
				return err
			}
		}
		if f.MaxChangedLines != nil && *f.MaxChangedLines < 0 {
			return fmt.Errorf("max_changed_lines cannot be negative")
		}
		r.Require, r.MaxChangedLines, r.PathsNot, r.OnRedAfterMerge = f.Require, f.MaxChangedLines, f.PathsNot, f.OnRedAfterMerge
	}
	if len(stray) > 0 {
		return fmt.Errorf("%s does not belong in a %s rule", strings.Join(stray, ", "), r.Kind)
	}
	if r.Kind == KindMerge && len(r.Actions) == 0 {
		// The shared cases write merge rules without actions: a merge rule is for merge.
		r.Actions = []string{"merge"}
	}
	if r.Kind != KindClassifier && len(r.Actions) == 0 {
		return fmt.Errorf("actions is required")
	}
	return nil
}

func (r *FileRule) classAndAnswer(f ruleFront) error {
	if f.Class != "" {
		if !classPattern.MatchString(f.Class) {
			return fmt.Errorf("class %q is not a class name", f.Class)
		}
		r.Class = f.Class
	}
	if f.Answer != nil {
		r.Answer = string(*f.Answer)
		if _, err := strconv.Atoi(r.Answer); r.Answer != "recommendation" && err != nil {
			return fmt.Errorf("a policy's answer is recommendation or an option number")
		}
	}
	return nil
}

func validFormatAction(a string) bool {
	if slices.Contains(formatActions, a) {
		return true
	}
	prefix, rest, ok := strings.Cut(a, ".")
	return ok && rest != "" && slices.Contains(toolPrefixes, prefix)
}

func (w *When) validate() error {
	for _, d := range w.DecisionAbout {
		if !slices.Contains(decisionKinds, d) {
			return fmt.Errorf("unknown decision_about %q", d)
		}
	}
	for _, g := range slices.Concat(w.Paths, w.PathsAll, w.Branch, w.Requirement) {
		if err := checkGlob(g); err != nil {
			return err
		}
	}
	if w.Time != "" && !timeWindowExpr.MatchString(w.Time) {
		return fmt.Errorf("time %q is not HH:MM-HH:MM", w.Time)
	}
	if w.ChangedLinesMax != nil && *w.ChangedLinesMax < 0 {
		return fmt.Errorf("changed_lines_max cannot be negative")
	}
	if w.CostUSDMax != nil && *w.CostUSDMax < 0 {
		return fmt.Errorf("cost_usd_max cannot be negative")
	}
	return nil
}

// splitFront returns the front matter and what follows it.
func splitFront(data []byte) (front, body []byte, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, nil, fmt.Errorf("the file must start with a --- line and YAML front matter")
	}
	head, tail, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, nil, fmt.Errorf("the front matter is not closed with a --- line")
	}
	if tail != "" && !strings.HasPrefix(tail, "\n") {
		return nil, nil, fmt.Errorf("the closing --- must be on its own line")
	}
	return []byte(head), []byte(strings.TrimPrefix(tail, "\n")), nil
}

// splitStatement splits the body into the statement and the "## Why" section.
func splitStatement(body []byte) (statement, why string) {
	text := strings.TrimSpace(string(body))
	if i := strings.Index("\n"+text, "\n## Why"); i >= 0 {
		statement = strings.TrimSpace(text[:max(i-1, 0)])
		why = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[max(i-1, 0):]), "## Why"))
		return statement, why
	}
	return text, ""
}
