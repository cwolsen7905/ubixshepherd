package standards

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	r, err := Parse("no-todo", []byte("---\nlevel: must\ncheck: \"! grep -r TODO .\"\napplies_to: [\"src/**\"]\nwhen: [commit]\n---\nNo TODO\nmarkers.\n\nmore body\n\n## Why\n\nBecause.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "no-todo" || r.Level != Must || r.Statement != "No TODO markers." || r.Rationale != "Because." ||
		r.Check != "! grep -r TODO ." || len(r.AppliesTo) != 1 || len(r.When) != 1 {
		t.Errorf("rule = %+v", r)
	}
	for name, in := range map[string]string{
		"level":     "---\nlevel: maybe\n---\nx\n",
		"no level":  "just text\n",
		"statement": "---\nlevel: must\n---\n\n## Why\n\nonly why\n",
		"id":        "---\nid: other\nlevel: must\n---\nx\n",
		"unknown":   "---\nlevel: must\nbogus: 1\n---\nx\n",
		"unclosed":  "---\nlevel: must\nx\n",
	} {
		if _, err := Parse("a-rule", []byte(in)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := Parse("Bad_ID", []byte("---\nlevel: must\n---\nx\n")); err == nil {
		t.Error("bad id parsed")
	}
	if d, err := Parse("gone", []byte("---\ndisabled: true\n---\n")); err != nil || !d.Disabled {
		t.Errorf("disabled = %+v, %v", d, err)
	}
}

func TestRenderRoundTrips(t *testing.T) {
	in := Rule{ID: "r-one", Level: Should, Statement: "Do it.", Rationale: "Why not.", Check: "true",
		AppliesTo: []string{"a/**"}, When: []string{"test"}}
	out, err := Parse("r-one", in.Render())
	if err != nil {
		t.Fatal(err)
	}
	if out.Level != in.Level || out.Statement != in.Statement || out.Rationale != in.Rationale ||
		out.Check != in.Check || out.AppliesTo[0] != "a/**" || out.When[0] != "test" {
		t.Errorf("round trip = %+v", out)
	}
}

func fixturePacks(t *testing.T) (root, repo string) {
	root, repo = t.TempDir(), t.TempDir()
	put(t, filepath.Join(root, "house"), "pack.md", "# house\n")
	put(t, filepath.Join(root, "house"), "a-rule.md", "---\nlevel: must\n---\nShepherd A.\n")
	put(t, filepath.Join(root, "house"), "b-rule.md", "---\nlevel: should\n---\nShepherd B.\n")
	put(t, filepath.Join(root, "house"), "c-rule.md", "---\nlevel: must\n---\nShepherd C.\n")
	put(t, filepath.Join(root, "extra"), "b-rule.md", "---\nlevel: must\n---\nExtra B.\n")
	put(t, filepath.Join(root, "house"), "broken.md", "---\nlevel: nope\n---\nx\n")
	put(t, filepath.Join(repo, "std"), "a-rule.md", "---\nlevel: should\n---\nRepo A.\n")
	put(t, filepath.Join(repo, "std"), "c-rule.md", "---\ndisabled: true\n---\n")
	put(t, filepath.Join(repo, "std"), "d-rule.md", "---\nlevel: must\n---\nRepo D.\n")
	return root, repo
}

func byID(s Set) map[string]Rule {
	m := map[string]Rule{}
	for _, r := range s.Rules {
		m[r.ID] = r
	}
	return m
}

func TestResolvePrecedence(t *testing.T) {
	root, repo := fixturePacks(t)
	s := Resolve(root, []string{"house", "extra", "missing"}, filepath.Join(repo, "std"))
	m := byID(s)
	if len(m) != 3 {
		t.Fatalf("rules = %v", s.Rules)
	}
	// The repo's rule replaces Shepherd's with the same id, including its level.
	if a := m["a-rule"]; a.Statement != "Repo A." || a.Level != Should || a.Pack != RepoPack {
		t.Errorf("a-rule = %+v", a)
	}
	// A later Shepherd pack replaces an earlier one.
	if b := m["b-rule"]; b.Statement != "Extra B." || b.Pack != "extra" {
		t.Errorf("b-rule = %+v", b)
	}
	if _, ok := m["c-rule"]; ok || len(s.Disabled) != 1 || s.Disabled[0] != "c-rule" {
		t.Errorf("c-rule not disabled: %v %v", m["c-rule"], s.Disabled)
	}
	if m["d-rule"].Statement != "Repo D." {
		t.Errorf("d-rule = %+v", m["d-rule"])
	}
	// A broken rule and a missing pack are reported, and the rest still applies.
	joined := strings.Join(s.Problems, "\n")
	if !strings.Contains(joined, "broken.md") || !strings.Contains(joined, "pack missing") {
		t.Errorf("problems = %v", s.Problems)
	}
	// Must before should.
	if s.Rules[len(s.Rules)-1].Level != Should {
		t.Errorf("order = %v", s.Rules)
	}
}

func TestResolveWithoutRepoPack(t *testing.T) {
	root, repo := fixturePacks(t)
	s := Resolve(root, []string{"house"}, filepath.Join(repo, "nothing-here"))
	if len(s.Rules) != 3 || len(s.Problems) != 1 {
		t.Errorf("set = %+v", s)
	}
}

func TestApplies(t *testing.T) {
	l := Lane{Scope: []string{"src/**"}, Files: []string{"src/a.go", "src/a_test.go", "docs/x.md"}}
	cases := []struct {
		r    Rule
		want bool
	}{
		{Rule{}, true},
		{Rule{AppliesTo: []string{"src/**/*.go"}}, true},
		{Rule{AppliesTo: []string{"web/**"}}, false},
		{Rule{When: []string{"commit"}}, true},
		{Rule{When: []string{"test"}}, true},
		{Rule{When: []string{"migration", "commit"}}, true},
		{Rule{AppliesTo: []string{"web/**"}, When: []string{"commit"}}, false},
	}
	for i, c := range cases {
		if got := c.r.Applies(l); got != c.want {
			t.Errorf("case %d (%+v) = %v", i, c.r, got)
		}
	}
	// A lane confined to one file does not do migrations or tests.
	one := Lane{Scope: []string{"src/main.go"}, Files: l.Files}
	if (Rule{When: []string{"migration"}}).Applies(one) || (Rule{When: []string{"test"}}).Applies(one) {
		t.Error("a tagged rule applies to a one-file lane")
	}
	extra := Lane{Scope: []string{"src/**"}, Tags: []string{"migration"}}
	if !(Rule{When: []string{"migration"}}).Applies(extra) {
		t.Error("a profile's extra tag is not active")
	}
}

func TestDigest(t *testing.T) {
	s := Set{
		PackDirs: []string{"/home/.shepherd/standards/house"},
		Disabled: []string{"old-rule"},
		Rules: []Rule{
			{ID: "must-one", Level: Must, Statement: "Always.", Rationale: "SECRET RATIONALE", Pack: "house", Path: "/p/must-one.md"},
			{ID: "web-only", Level: Must, Statement: "Web.", AppliesTo: []string{"web/**"}},
			{ID: "should-one", Level: Should, Statement: strings.Repeat("long ", 60), Pack: "house"},
		},
	}
	l := Lane{Scope: []string{"src/**"}, Files: []string{"src/a.go"}}
	d := Digest(s, l, DigestOptions{Docs: []string{"docs/standards", "CLAUDE.md"}})
	for _, want := range []string{"take precedence over the Shepherd digest", "do not stop to ask", "docs/standards, CLAUDE.md",
		"- MUST must-one: Always.", "- should should-one: long", "...", "1 more rule(s)", "old-rule"} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "SECRET RATIONALE") || strings.Contains(d, "web-only") || strings.Contains(d, "Web.") {
		t.Errorf("digest has what it should not:\n%s", d)
	}
	if Digest(Set{}, l, DigestOptions{}) != "" {
		t.Error("an empty set gave a digest")
	}
}

func TestDigestBudget(t *testing.T) {
	var s Set
	for _, id := range []string{"aa", "bb", "cc", "dd"} {
		s.Rules = append(s.Rules, Rule{ID: id, Level: Must, Statement: strings.Repeat("x", 80), Path: "/p/" + id + ".md"})
	}
	d := Digest(s, Lane{Scope: []string{"**"}}, DigestOptions{Budget: 200})
	if !strings.Contains(d, "- MUST aa:") || !strings.Contains(d, "- MUST bb:") {
		t.Errorf("first rules not inlined:\n%s", d)
	}
	if strings.Contains(d, "- MUST dd:") || !strings.Contains(d, "dd (/p/dd.md)") {
		t.Errorf("rules over the budget not listed by id and path:\n%s", d)
	}
}

func TestExistingDocs(t *testing.T) {
	wt := t.TempDir()
	put(t, filepath.Join(wt, "docs", "standards"), "x.md", "x")
	put(t, wt, "CLAUDE.md", "x")
	got := ExistingDocs(wt, []string{"docs/standards", "docs/architecture", "AGENTS.md", "CLAUDE.md"})
	if strings.Join(got, ",") != "docs/standards,CLAUDE.md" {
		t.Errorf("docs = %v", got)
	}
}

func TestChecks(t *testing.T) {
	set := Set{Rules: []Rule{
		{ID: "always", Level: Must, Check: "true"},
		{ID: "src-only", Level: Must, Check: "false", AppliesTo: []string{"src/**"}},
		{ID: "web-only", Level: Must, Check: "false", AppliesTo: []string{"web/**"}},
		{ID: "written", Level: Must},
	}}
	got := Checked(set, []string{"src/a.go"})
	if len(got) != 2 || got[0].ID != "always" || got[1].ID != "src-only" {
		t.Fatalf("checked = %v", got)
	}
	if len(Checked(set, nil)) != 1 {
		t.Error("an empty diff ran a path-scoped check")
	}
}

func TestRunChecks(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("checks are shell commands")
	}
	dir := t.TempDir()
	rules := []Rule{
		{ID: "passes", Level: Must, Check: "true"},
		{ID: "fails-must", Level: Must, Check: "echo nope TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789; exit 1"},
		{ID: "fails-should", Level: Should, Check: "exit 3"},
		{ID: "sees-base", Level: Must, Check: `test "$SHEPHERD_BASE_REF" = origin/dev && test "$(pwd -P)" = "$(cd . && pwd -P)"`},
		{ID: "slow", Level: Must, Check: "sleep 5"},
	}
	res := RunChecks(context.Background(), dir, "origin/dev", rules, 300*time.Millisecond)
	if !res[0].OK || res[0].Blocks() {
		t.Errorf("passes: %+v", res[0])
	}
	if res[1].OK || !res[1].Blocks() || !strings.Contains(res[1].Output, "nope") || strings.Contains(res[1].Output, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("fails-must: %+v", res[1])
	}
	if res[2].OK || res[2].Blocks() {
		t.Errorf("a should rule must not block: %+v", res[2])
	}
	if !res[3].OK {
		t.Errorf("base ref not passed: %+v", res[3])
	}
	if res[4].OK || !strings.Contains(res[4].Output, "timed out") {
		t.Errorf("slow: %+v", res[4])
	}
}

func TestSeedHouse(t *testing.T) {
	root := t.TempDir()
	wrote, err := SeedHouse(root)
	if err != nil || !wrote {
		t.Fatalf("seed = %v, %v", wrote, err)
	}
	s := Resolve(root, []string{"house"}, "")
	if len(s.Problems) != 0 || len(s.Rules) != 7 {
		t.Errorf("seed pack: %d rules, problems %v", len(s.Rules), s.Problems)
	}
	// Never over an existing pack, even one the person emptied.
	os.Remove(filepath.Join(root, "house", "no-todo-markers.md"))
	if wrote, _ := SeedHouse(root); wrote {
		t.Error("seeded over an existing pack")
	}
	if _, err := os.Stat(filepath.Join(root, "house", "no-todo-markers.md")); err == nil {
		t.Error("restored a removed rule")
	}
}

func TestExamplesMirrorTheSeed(t *testing.T) {
	seed, err := Seed()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("..", "..", "examples", "standards", "house")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(seed) {
		t.Errorf("examples has %d files, the seed %d", len(entries), len(seed))
	}
	for name, want := range seed {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("examples/standards/house/%s differs from internal/standards/seed/house/%s (copy it over)", name, name)
		}
	}
}
