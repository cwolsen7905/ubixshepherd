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
