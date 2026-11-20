package scope

import (
	"reflect"
	"strings"
	"testing"
)

func TestCovers(t *testing.T) {
	for _, c := range []struct {
		outer, g string
		want     bool
	}{
		{"internal", "internal/fold/**", true},
		{"internal", "internal", true},
		{"internal/fold", "internal", false},
		{"internal/**", "internal/fold/*.go", true},
		{"internal/**", "internal/fold/a.go", true},
		{"internal/**", "cmd/x.go", false},
		{"internal/**", "**", false},
		{"**", "anything/**", true},
		// A plain path under a wildcard scope may name a directory, so it is not provably inside.
		{"docs/*.md", "docs/a.md", false},
		{"docs/**", "docs/a.md", true},
		{"docs/*.md", "docs/*.md", true},
		{"docs/*.md", "docs/*", false},
		{"docs/*.md", "docs/a/b.md", false},
		{"docs/*", "docs/a", false},
		{"docs/*", "docs/a/**", false},
		{"**/x.go", "internal/x.go", false},
		{"**/x.go", "**/internal/x.go", true},
		{"**/x.go", "**/x.go", true},
		{"internal/*/x.go", "internal/fold/x.go", false},
		{"internal/*/*.go", "internal/*/x.go", true},
		{"internal/*/x.go", "internal/*/y.go", false},
		{"*.md", "docs/a.md", false},
	} {
		if got := Covers(c.outer, c.g); got != c.want {
			t.Errorf("Covers(%q, %q) = %v, want %v", c.outer, c.g, got, c.want)
		}
	}
}

func TestNarrow(t *testing.T) {
	cur := []string{"internal/**", "docs/a.md"}
	got, err := Narrow(cur, []string{"docs/a.md"}, nil)
	if err != nil || !reflect.DeepEqual(got, []string{"internal/**"}) {
		t.Fatalf("drop: %v %v", got, err)
	}
	got, err = Narrow(cur, []string{"internal/**"}, []string{"internal/fold/**"})
	if err != nil || !reflect.DeepEqual(got, []string{"docs/a.md", "internal/fold/**"}) {
		t.Fatalf("replace: %v %v", got, err)
	}
	for name, c := range map[string]struct{ drop, to []string }{
		"widen":                    {nil, []string{"cmd/**"}},
		"widen beyond":             {[]string{"docs/a.md"}, []string{"docs/**"}},
		"not held":                 {[]string{"cmd/**"}, nil},
		"nothing":                  {nil, nil},
		"leaves empty":             {[]string{"internal/**", "docs/a.md"}, nil},
		"drop and re-add widening": {[]string{"internal/**"}, []string{"**"}},
	} {
		if got, err := Narrow(cur, c.drop, c.to); err == nil {
			t.Errorf("%s: allowed, got %v", name, got)
		}
	}
	if _, err := Narrow(cur, nil, []string{"cmd/**"}); err == nil || !strings.Contains(err.Error(), "shrinks") {
		t.Errorf("message: %v", err)
	}
}

func TestPlanScope(t *testing.T) {
	got := PlanScope("# Plan\n\n- **Scope:** `internal/x/**`, docs/y.md\n")
	if !reflect.DeepEqual(got, []string{"internal/x/**", "docs/y.md"}) {
		t.Errorf("got %v", got)
	}
	// Prose is not a scope; a later real line still is.
	got = PlanScope("Scope: see the section below\n\nScope: docs/y.md\n")
	if !reflect.DeepEqual(got, []string{"docs/y.md"}) {
		t.Errorf("prose then real: %v", got)
	}
	if PlanScope("Scope: see the section below\n") != nil {
		t.Error("prose taken as globs")
	}
	if PlanScope("no scope here\n") != nil {
		t.Error("expected nil")
	}
}
