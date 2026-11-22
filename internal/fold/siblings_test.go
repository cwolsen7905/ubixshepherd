package fold

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func (f *fixture) openWith(name string, scope []string, docs ...string) store.Lane {
	f.t.Helper()
	o, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: name, Scope: scope, Docs: docs})
	if err != nil {
		f.t.Fatalf("open %s: %v", name, err)
	}
	return o.Lane
}

// listed reports whether the problem names file on a line of its own, optionally followed
// by a note in parentheses.
func listed(problem, file string) bool {
	for _, line := range strings.Split(problem, "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == file || strings.HasPrefix(line, file+" (") {
			return true
		}
	}
	return false
}

func (f *fixture) push(l store.Lane) Verdict {
	f.t.Helper()
	v, err := f.fold.CheckPush(context.Background(), &l, &f.repo, l.Worktree, []PushRef{pushRef(f.t, l.Worktree, l.Branch)})
	if err != nil {
		f.t.Fatalf("check push: %v", err)
	}
	return v
}

func TestSiblingsOfScopedFileMayBePushed(t *testing.T) {
	f := newFixture(t)
	l := f.openWith("work", []string{"src/foo.go"}, "docs/lanes.md")
	for _, file := range []string{"src/foo.go", "src/foo_test.go", "CHANGELOG.md", "docs/lanes.md"} {
		f.commit(l.Worktree, file)
	}
	if v := f.push(l); !v.OK {
		t.Fatalf("siblings refused: %+v", v)
	}

	// A test file whose source is not in scope, another doc page, a changelog below the
	// root and a non-Go test are not siblings.
	for _, file := range []string{"src/bar_test.go", "docs/other.md", "sub/CHANGELOG.md", "src/foo.test.ts"} {
		f.commit(l.Worktree, file)
	}
	v := f.push(l)
	if v.OK || len(v.Problems) != 1 {
		t.Fatalf("non-siblings pushed: %+v", v)
	}
	for _, file := range []string{"src/bar_test.go", "docs/other.md", "sub/CHANGELOG.md", "src/foo.test.ts"} {
		if !listed(v.Problems[0], file) {
			t.Errorf("%s not named: %s", file, v.Problems[0])
		}
	}
	for _, file := range []string{"src/foo_test.go", "CHANGELOG.md", "docs/lanes.md"} {
		if listed(v.Problems[0], file) {
			t.Errorf("sibling %s refused: %s", file, v.Problems[0])
		}
	}
}

func TestSiblingHeldByAnotherLaneStaysRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.openWith("a", []string{"src/foo.go"}, "docs/lanes.md")
	b := f.openWith("b", []string{"src/foo_test.go", "CHANGELOG.md", "docs/**"})
	for _, file := range []string{"src/foo.go", "src/foo_test.go", "CHANGELOG.md", "docs/lanes.md"} {
		f.commit(a.Worktree, file)
	}
	v := f.push(a)
	if v.OK || len(v.Problems) != 1 {
		t.Fatalf("held siblings pushed: %+v", v)
	}
	p := v.Problems[0]
	for _, file := range []string{"src/foo_test.go", "CHANGELOG.md", "docs/lanes.md"} {
		if !strings.Contains(p, file+" (held by lane b)") {
			t.Errorf("%s not shown as held by b: %s", file, p)
		}
	}
	if listed(p, "src/foo.go") {
		t.Errorf("scoped file refused: %s", p)
	}

	// The hold ends with the holder's lane.
	if err := f.fold.Store.SetLaneState(ctx, b.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	if v := f.push(a); !v.OK {
		t.Fatalf("released siblings refused: %+v", v)
	}
}

func TestSiblingsAreNotLeased(t *testing.T) {
	f := newFixture(t)
	// Two lanes may both reach CHANGELOG.md as a sibling, and a lane may open over a
	// sibling of another's: siblings never conflict at open.
	a := f.openWith("a", []string{"src/foo.go"})
	b := f.openWith("b", []string{"lib/bar.go"})
	f.commit(a.Worktree, "CHANGELOG.md")
	f.commit(b.Worktree, "CHANGELOG.md")
	if v := f.push(a); !v.OK {
		t.Errorf("a: %+v", v)
	}
	if v := f.push(b); !v.OK {
		t.Errorf("b: %+v", v)
	}
	f.openWith("c", []string{"src/foo_test.go"})
}

func TestSiblingChecksFilesStillRefused(t *testing.T) {
	f := newFixture(t)
	f.land(map[string]string{"CHANGELOG.md": "# Changes\n"})
	f.approve("CHANGELOG.md")
	l := f.openWith("work", []string{"src/foo.go"})
	f.commit(l.Worktree, "CHANGELOG.md")
	v := f.push(l)
	if v.OK || !strings.Contains(strings.Join(v.Problems, "\n"), "CHANGELOG.md") {
		t.Fatalf("approved check file pushed as a sibling: %+v", v)
	}
}

func TestSiblingDiffMarksInScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.openWith("a", []string{"src/foo.go"})
	f.openWith("b", []string{"src/bar_test.go"})
	for _, file := range []string{"src/foo_test.go", "src/bar_test.go", "other.txt"} {
		f.commit(a.Worktree, file)
	}
	d, err := f.fold.Diff(ctx, a.ID, DiffRequest{})
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, fi := range d.Committed {
		in[fi.Path] = fi.InScope
	}
	if !in["src/foo_test.go"] || in["src/bar_test.go"] || in["other.txt"] || d.OutOfScope != 2 {
		t.Errorf("in scope = %v, out of scope %d", in, d.OutOfScope)
	}
	one, err := f.fold.Diff(ctx, a.ID, DiffRequest{File: "src/foo_test.go"})
	if err != nil || one.OutOfScope != 0 {
		t.Errorf("file diff: %+v %v", one, err)
	}
}

func TestOpenDocsAreChecked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, bad := range []string{"", "/etc/passwd", "../x.md", "docs/*.md"} {
		_, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "d", Scope: []string{"src/**"}, Docs: []string{bad}})
		if !errors.Is(err, ErrRefused) {
			t.Errorf("docs %q: %v", bad, err)
		}
	}
	l := f.openWith("d", []string{"src/**"}, "docs/./lanes.md", "docs/lanes.md")
	if got := docsOf(ctx, f.repo.Path, l.Branch); len(got) != 1 || got[0] != "docs/lanes.md" {
		t.Errorf("recorded docs = %v", got)
	}
	if _, err := f.fold.Close(ctx, l.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := docsOf(ctx, f.repo.Path, l.Branch); len(got) != 0 {
		t.Errorf("docs left after close: %v", got)
	}
}
