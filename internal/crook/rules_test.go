package crook

import (
	"strings"
	"testing"
	"time"
)

func layer(name string, files map[string]string) LayerFiles {
	l := LayerFiles{Layer: name, Files: map[string][]byte{}}
	for k, v := range files {
		l.Files[k] = []byte(v)
	}
	return l
}

func TestParseRuleRejectsUnknowns(t *testing.T) {
	bad := map[string]string{
		"unknown field":    "---\nkind: guardrail\nactions: [push]\nthen: ask\nbogus: 1\n---\nx\n",
		"unknown kind":     "---\nkind: wish\n---\nx\n",
		"unknown action":   "---\nkind: guardrail\nactions: [fly]\nthen: ask\n---\nx\n",
		"unknown prefix":   "---\nkind: guardrail\nactions: [acme.go]\nthen: ask\n---\nx\n",
		"unknown actor":    "---\nkind: guardrail\nactions: [push]\nactor: [bot]\nthen: ask\n---\nx\n",
		"unknown matcher":  "---\nkind: guardrail\nactions: [push]\nwhen:\n  colour: red\nthen: ask\n---\nx\n",
		"unknown decision": "---\nkind: guardrail\nactions: [push]\nwhen:\n  decision_about: lunch\nthen: ask\n---\nx\n",
		"unknown require":  "---\nkind: merge\nrequire: [vibes]\nthen: proceed\n---\nx\n",
		"no then":          "---\nkind: guardrail\nactions: [push]\n---\nx\n",
		"guardrail allows": "---\nkind: guardrail\nactions: [push]\nthen: proceed\n---\nx\n",
		"no actions":       "---\nkind: policy\nthen: ask\n---\nx\n",
		"bad time":         "---\nkind: guardrail\nactions: [push]\nwhen:\n  time: noon\nthen: ask\n---\nx\n",
		"classifier then":  "---\nkind: classifier\napplies_to: action\nclass: big\nthen: ask\n---\nx\n",
		"bad class":        "---\nkind: classifier\napplies_to: action\nclass: Big\n---\nx\n",
		"answer elsewhere": "---\nkind: policy\nactions: [push]\nthen: ask\nanswer: recommendation\n---\nx\n",
		"answer no about":  "---\nkind: standing-answer\nactions: [answer_decision]\nanswer: 1\n---\nx\n",
		"no front matter":  "just text\n",
		"open front":       "---\nkind: policy\n",
	}
	for name, text := range bad {
		if _, err := ParseRule(LayerRepo, "r", []byte(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseRule(LayerRepo, "Bad_Id", []byte("---\nkind: policy\n---\n")); err == nil {
		t.Error("bad id accepted")
	}
}

func TestParseRuleSections(t *testing.T) {
	r, err := ParseRule(LayerRepo, "x", []byte("---\nkind: guardrail\nactions: push\nthen: ask\n---\nHold pushes.\n\n## Why\nBecause.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Statement != "Hold pushes." || r.Why != "Because." || r.Mode != ModeLive || r.SHA == "" {
		t.Errorf("rule = %+v", r)
	}
	m, err := ParseRule(LayerRepo, "m", []byte("---\nkind: merge\nmode: live\nthen: proceed\n---\nx\n"))
	if err != nil || len(m.Warnings) == 0 {
		t.Errorf("live merge rule: err %v, warnings %v", err, m.Warnings)
	}
}

func TestBadFileIsNotUsedAndKeepsEarlierRule(t *testing.T) {
	good := "---\nkind: guardrail\nactions: [push]\nthen: ask\n---\nask.\n"
	set := Merge(layer(LayerWorkspace, map[string]string{"a.md": good}), layer(LayerRepo, map[string]string{"a.md": "---\nkind: nope\n---\n"}))
	if len(set.Errors) != 1 || len(set.Rules) != 1 || set.Rules[0].Layer != LayerWorkspace {
		t.Errorf("set = %+v", set)
	}
}

func TestLayersReplaceDisableAndFlagLoosening(t *testing.T) {
	g := func(then string) string {
		return "---\nkind: guardrail\nactions: [push]\nthen: " + then + "\n---\nsaid " + then + ".\n"
	}
	set := Merge(
		layer(LayerRepo, map[string]string{"a.md": g("hold"), "b.md": "---\ndisabled: true\n---\n", "c.md": g("refuse")}),
		layer(LayerWorkspace, map[string]string{"a.md": g("ask"), "b.md": g("ask"), "d.md": g("ask")}),
		layer(LayerProject, map[string]string{"c.md": g("ask")}),
	)
	if len(set.Errors) != 0 {
		t.Fatal(set.Errors)
	}
	var ids []string
	for _, r := range set.Rules {
		ids = append(ids, r.Layer+"/"+r.ID)
	}
	// b is switched off; a and c are replaced by the repo layer.
	if got := strings.Join(ids, " "); got != "workspace/d repo/a repo/c" {
		t.Errorf("effective = %s", got)
	}
	want := map[string]Override{
		"a": {ID: "a", From: LayerWorkspace, By: LayerRepo, Loosening: true},
		"b": {ID: "b", From: LayerWorkspace, By: LayerRepo, Disabled: true, Loosening: true},
		"c": {ID: "c", From: LayerProject, By: LayerRepo},
	}
	if len(set.Overrides) != len(want) {
		t.Fatalf("overrides = %+v", set.Overrides)
	}
	for _, o := range set.Overrides {
		if o != want[o.ID] {
			t.Errorf("override %s = %+v, want %+v", o.ID, o, want[o.ID])
		}
	}
}

func TestGlobs(t *testing.T) {
	for _, c := range []struct {
		glob, p string
		want    bool
	}{
		{"db/migrations/**", "db/migrations/0042.sql", true},
		{"db/migrations/**", "db/other.sql", false},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "a/b/README.md", true},
		{".shepherd/**", ".shepherd/rules/x.md", true},
		{"SHP-FR-WEB-*", "SHP-FR-WEB-002", true},
		{"a.b", "axb", false},
	} {
		if got := globMatch([]string{c.glob}, c.p); got != c.want {
			t.Errorf("%q on %q = %v", c.glob, c.p, got)
		}
	}
}

func TestTimeWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		w    string
		now  time.Time
		want bool
	}{
		{"09:00-17:00", at(9, 0), true},
		{"09:00-17:00", at(17, 0), false},
		{"22:00-06:00", at(23, 30), true},
		{"22:00-06:00", at(5, 59), true},
		{"22:00-06:00", at(12, 0), false},
	} {
		if got := inWindow(c.w, c.now); got != c.want {
			t.Errorf("%s at %v = %v", c.w, c.now.Format("15:04"), got)
		}
	}
}

func evalOne(t *testing.T, files map[string]string, q Query) Outcome {
	t.Helper()
	set := Merge(layer(LayerRepo, files))
	if len(set.Errors) > 0 {
		t.Fatal(set.Errors)
	}
	return Evaluate(set, q)
}

func TestMostRestrictiveWinsAndTieCitesLaterLayer(t *testing.T) {
	g := func(then string) string {
		return "---\nkind: guardrail\nactions: [push]\nthen: " + then + "\n---\nx\n"
	}
	set := Merge(layer(LayerWorkspace, map[string]string{"w.md": g("ask")}), layer(LayerRepo, map[string]string{"r.md": g("ask"), "s.md": g("hold")}))
	got := Evaluate(set, Query{Action: "push", Actor: ActorWorker, Facts: map[string]any{}})
	if got.Verdict != Ask || got.Rule != "r" || got.Layer != LayerRepo {
		t.Errorf("got %+v", got)
	}
}

func TestPersonSkipsRulesButNotTheFloor(t *testing.T) {
	files := map[string]string{"g.md": "---\nkind: guardrail\nactions: [push]\nactor: [person]\nthen: refuse\n---\nx\n"}
	if got := evalOne(t, files, Query{Action: "push", Actor: ActorPerson, Facts: map[string]any{}}); got.Verdict != Proceed {
		t.Errorf("person: %+v", got)
	}
	got := evalOne(t, files, Query{Action: "push", Actor: ActorPerson, Facts: map[string]any{"paths": []string{".shepherd/rules/x.md"}}})
	if got.Verdict != Ask || got.Rule != FloorRulesFolder {
		t.Errorf("person on rules folder: %+v", got)
	}
}

func TestFloorKeepsAStricterVerdict(t *testing.T) {
	files := map[string]string{"g.md": "---\nkind: guardrail\nactions: [push]\nthen: refuse\n---\nno.\n"}
	got := evalOne(t, files, Query{Action: "push", Actor: ActorWorker, Facts: map[string]any{"paths": []string{".shepherd/x"}}})
	if got.Verdict != Refuse || got.Rule != "g" {
		t.Errorf("got %+v", got)
	}
}

func TestClassifierFeedsPolicy(t *testing.T) {
	files := map[string]string{
		"big.md":  "---\nkind: classifier\napplies_to: action\nclass: big\nactions: [push]\nwhen:\n  changed_lines_max: 10000\n  paths: [\"db/**\"]\n---\nx\n",
		"hold.md": "---\nkind: policy\nactions: [push]\nclass: big\nthen: hold\n---\nbig things wait.\n",
	}
	got := evalOne(t, files, Query{Action: "push", Actor: ActorDaemon, Facts: map[string]any{"paths": []string{"db/a.sql"}, "changed_lines": 5}})
	if got.Verdict != Hold || got.Rule != "hold" || len(got.Classes) != 1 {
		t.Errorf("got %+v", got)
	}
	got = evalOne(t, files, Query{Action: "push", Actor: ActorDaemon, Facts: map[string]any{"paths": []string{"x"}, "changed_lines": 5}})
	if got.Verdict != Ask || got.Rule != RuleDefault {
		t.Errorf("unclassified: %+v", got)
	}
}

func TestStartRunWithNoSlotHolds(t *testing.T) {
	zero := 0
	got := evalOne(t, nil, Query{Action: "start_run", Actor: ActorDaemon, Limits: Limits{RunsFree: &zero}})
	if got.Verdict != Hold || got.Rule != RuleRunCaps {
		t.Errorf("got %+v", got)
	}
}

func TestStandingAnswerStartsInShadow(t *testing.T) {
	files := map[string]string{"sa.md": "---\nkind: standing-answer\nactions: [answer_decision]\nwhen:\n  decision_about: scope\nanswer: recommendation\n---\nTake the recommendation.\n"}
	q := Query{Action: "answer_decision", Actor: ActorDesk, Facts: map[string]any{"decision_about": "scope"}}
	got := evalOne(t, files, q)
	if got.Verdict != Ask || len(got.Shadow) != 1 || got.Shadow[0].Verdict != Proceed {
		t.Errorf("got %+v", got)
	}
	live := map[string]string{"sa.md": strings.Replace(files["sa.md"], "kind: standing-answer", "kind: standing-answer\nmode: live", 1)}
	if got := evalOne(t, live, q); got.Verdict != Proceed || got.Answer != "recommendation" {
		t.Errorf("live: %+v", got)
	}
}
