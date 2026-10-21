package scope

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		glob, file string
		want       bool
	}{
		{"README.md", "README.md", true},
		{"README.md", "docs/README.md", false},
		{"docs", "docs/a/b.md", true},
		{"docs", "docsx/a.md", false},
		{"docs/*.md", "docs/v1.md", true},
		{"docs/*.md", "docs/sub/v1.md", false},
		{"internal/lease/**", "internal/lease/lease.go", true},
		{"internal/lease/**", "internal/lease/a/b/c.go", true},
		{"internal/lease/**", "internal/lease", true},
		{"internal/lease/**", "internal/leases/x.go", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/main.go", true},
		{"**/*.go", "a/b/main.md", false},
		{"app/*/src/Routes.php", "app/Web/src/Routes.php", true},
		{"app/*/src/Routes.php", "app/Web/src/Other.php", false},
		{"**", "anything/at/all", true},
	}
	for _, c := range cases {
		if got := Match(c.glob, c.file); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.glob, c.file, got, c.want)
		}
	}
}

func TestOverlap(t *testing.T) {
	files := []string{"README.md", "docs/v1.md", "docs/a.go", "internal/lease/lease.go", "internal/fold/fold.go"}
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"disjoint dirs", []string{"internal/lease/**"}, []string{"internal/fold/**"}, false},
		{"parent and child", []string{"internal/**"}, []string{"internal/lease/**"}, true},
		{"same file", []string{"README.md"}, []string{"README.md", "docs/**"}, true},
		{"glob and file", []string{"docs/*.md"}, []string{"docs/v1.md"}, true},
		{"same dir, different extensions", []string{"docs/*.md"}, []string{"docs/*.go"}, false},
		{"new files in a shared new dir", []string{"internal/new/**"}, []string{"internal/new/sub/**"}, true},
		{"new literal under a glob", []string{"internal/lease/**"}, []string{"internal/lease/new.go"}, true},
		{"everything", []string{"**"}, []string{"docs/v1.md"}, true},
	}
	for _, c := range cases {
		got := Overlap(c.a, c.b, files)
		if (len(got) > 0) != c.want {
			t.Errorf("%s: Overlap = %v, want overlap %v", c.name, got, c.want)
		}
	}
	got := Overlap([]string{"docs/**"}, []string{"docs/v1.md"}, files)
	if len(got) == 0 || !strings.Contains(strings.Join(got, " "), "docs/v1.md") {
		t.Errorf("overlap should name the file: %v", got)
	}
}

func TestWithout(t *testing.T) {
	files := []string{"a/x.go", "a/x_test.go", "a/sub/y.go", "b/z.go", "README.md"}
	cases := []struct {
		name      string
		globs     []string
		protected []string
		want      string
	}{
		{"no marks", []string{"a/**"}, nil, "a/x.go a/x_test.go a/sub/y.go"},
		{"glob drops the marked file only", []string{"a/**"}, []string{"a/x_test.go"}, "a/x.go a/sub/y.go"},
		{"literal file", []string{"README.md"}, []string{"README.md"}, ""},
		{"directory literal", []string{"a"}, []string{"a/sub/y.go"}, "a/x.go a/x_test.go"},
		{"mark outside the scope", []string{"b/**"}, []string{"a/x.go"}, "b/z.go"},
		{"unclean mark", []string{"a/*.go"}, []string{"./a/x.go"}, "a/x_test.go"},
	}
	for _, c := range cases {
		if got := strings.Join(Without(c.globs, files, c.protected), " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOverlapWithout(t *testing.T) {
	files := []string{"d/a.go", "d/a_test.go", "d/b.go"}
	// Two lanes that both reach a protected file only: no collision.
	if got := OverlapWithout([]string{"d/a*.go"}, []string{"d/*_test.go"}, files, []string{"d/a_test.go"}); len(got) != 0 {
		t.Errorf("overlap on a protected file = %v", got)
	}
	if got := Overlap([]string{"d/a*.go"}, []string{"d/*_test.go"}, files); len(got) != 1 {
		t.Errorf("without marks = %v", got)
	}
	// A real shared file still collides.
	if got := OverlapWithout([]string{"d/**"}, []string{"d/b.go"}, files, []string{"d/a_test.go"}); len(got) == 0 || got[0] != "d/b.go" {
		t.Errorf("shared unprotected file = %v", got)
	}
	// A literal naming a protected file that does not exist in files is ignored too.
	if got := OverlapWithout([]string{"d/new_test.go"}, []string{"d/**"}, nil, []string{"d/new_test.go"}); len(got) != 0 {
		t.Errorf("protected literal = %v", got)
	}
	if got := OverlapWithout([]string{"d/new.go"}, []string{"d/**"}, nil, []string{"d/new_test.go"}); len(got) != 1 {
		t.Errorf("unprotected literal = %v", got)
	}
}
