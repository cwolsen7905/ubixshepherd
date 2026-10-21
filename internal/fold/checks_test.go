package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// land commits files to the repo's main branch on origin, the way a merged checks lane
// would have.
func (f *fixture) land(files map[string]string) {
	f.t.Helper()
	for name, body := range files {
		p := filepath.Join(f.repo.Path, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	gitT(f.t, f.repo.Path, "add", ".")
	gitT(f.t, f.repo.Path, "commit", "-q", "-m", "land checks")
	gitT(f.t, f.repo.Path, "push", "-q", "origin", "HEAD:main")
}

// approve marks paths as the person would.
func (f *fixture) approve(paths ...string) []store.CheckFile {
	f.t.Helper()
	got, err := f.fold.ApproveChecks(context.Background(), ApproveRequest{RepoID: f.repo.ID, Paths: paths, By: "person (cli)"})
	if err != nil {
		f.t.Fatalf("approve %v: %v", paths, err)
	}
	return got
}

func refusedWith(t *testing.T, what string, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: accepted", what)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%s: %q lacks %q", what, err, w)
		}
	}
}

func TestApproveChecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/b_test.go": "package src\n// b\n"})
	head := gitT(t, f.repo.Path, "rev-parse", "origin/main")

	_, err := f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/nope_test.go"}, By: "person (cli)"})
	refusedWith(t, "missing path", err, "not on", "src/nope_test.go")
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src"}, By: "person (cli)"})
	refusedWith(t, "directory", err, "directory")
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/*_test.go"}, By: "person (cli)"})
	refusedWith(t, "pattern", err, "patterns")
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"../x"}, By: "person (cli)"})
	refusedWith(t, "outside the repo", err, "inside the repo")
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go"}, ItemID: 99, By: "person (cli)"})
	refusedWith(t, "unknown item", err, "item 99")

	// Committed locally but not on origin/main is not approvable.
	f.commit(f.repo.Path, "src/local_test.go")
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/local_test.go"}, By: "person (cli)"})
	refusedWith(t, "not on origin", err, "not on")
	gitT(t, f.repo.Path, "reset", "-q", "--hard", "origin/main")

	// All or none: one bad path approves nothing.
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go", "src/nope_test.go"}, By: "person (cli)"})
	refusedWith(t, "one bad path", err, "nope")
	if live, _ := f.fold.Protected(ctx, f.repo.ID); len(live) != 0 {
		t.Fatalf("a refused approve marked %+v", live)
	}

	got := f.approve("src/a_test.go", "./src/b_test.go")
	if len(got) != 2 || got[0].BaseCommit != head || got[0].Blob != gitT(t, f.repo.Path, "rev-parse", "origin/main:src/a_test.go") || got[0].ApprovedBy != "person (cli)" {
		t.Fatalf("approved: %+v", got)
	}
	_, err = f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go"}, By: "person (cli)"})
	refusedWith(t, "twice", err, "already")
}

func TestApproveRefusesPathInOpenLaneScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n"})
	l := f.open("revise") // scope src/**
	_, err := f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go"}, By: "person (cli)"})
	refusedWith(t, "in an open lane's scope", err, "revise", "in flight")
	if _, err := f.fold.Close(ctx, l.ID, true); err != nil {
		t.Fatal(err)
	}
	f.approve("src/a_test.go")
}

func TestApproveRequiresApprover(t *testing.T) {
	f := newFixture(t)
	_, err := f.fold.ApproveChecks(context.Background(), ApproveRequest{RepoID: f.repo.ID, Paths: []string{"README.md"}})
	refusedWith(t, "no approver", err, "who made it")
}

func TestOpenExcludesCheckFiles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/impl.go": "package src\n", "checks/only_test.go": "package c\n"})
	f.approve("src/a_test.go", "checks/only_test.go")

	// A broad scope opens, and says what it may not touch.
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "build", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range o.Notes {
		found = found || n == "excluded: 1 approved check files (src/a_test.go)"
	}
	if !found {
		t.Errorf("notes = %q", o.Notes)
	}
	if o.Scope[0] != "src/**" {
		t.Errorf("the scope is recorded as given: %v", o.Scope)
	}

	// A scope whose every match is a marked file is refused, and so is a literal.
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "only", Scope: []string{"checks/**"}})
	refusedWith(t, "only marked files", err, "only approved check files", "checks/only_test.go", "decision")
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "lit", Scope: []string{"src/a_test.go"}})
	refusedWith(t, "literal", err, "names an approved check file")

	// A scope that matches nothing yet is not refused.
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "new", Scope: []string{"lib/**"}}); err != nil {
		t.Errorf("a scope of new paths: %v", err)
	}
}

func TestTwoLanesMayReachTheSameCheckFile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/a.go": "package src\n", "src/b.go": "package src\n"})
	// Without marks the two scopes collide on the test file.
	one, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "one", Scope: []string{"src/a*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "two", Scope: []string{"src/*_test.go"}})
	refusedWith(t, "unmarked overlap", err, "overlaps lane one")
	if _, err := f.fold.Close(ctx, one.ID, true); err != nil {
		t.Fatal(err)
	}
	f.approve("src/a_test.go")
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "one", Scope: []string{"src/a*.go"}}); err != nil {
		t.Fatal(err)
	}
	// "two" reaches only the marked file, so it is refused for that, not as an overlap.
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "two", Scope: []string{"src/*_test.go"}})
	refusedWith(t, "only marked", err, "only approved check files")
	// Two lanes whose scopes both reach the marked file open, since neither may change it.
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "three", Scope: []string{"src/b.go", "src/*_test.go"}}); err != nil {
		t.Fatalf("second lane reaching the same marked file: %v", err)
	}
}

func TestRescopeAndWidenExcludeCheckFiles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/impl.go": "package src\n", "docs/x.md": "x\n", "chk/c_test.go": "package c\n"})
	f.approve("src/a_test.go", "chk/c_test.go")
	l := f.open("work")

	// Adding a literal naming a marked file is refused, with or without a decision.
	_, err := f.fold.Rescope(ctx, l.ID, []string{"chk/c_test.go"}, nil)
	refusedWith(t, "literal", err, "names an approved check file")
	_, err = f.fold.Rescope(ctx, l.ID, []string{"chk/**"}, nil)
	refusedWith(t, "only marked", err, "only approved check files")

	// A glob that reaches marked and other files is taken and reports the exclusion.
	lane, excluded, err := f.fold.RescopeExcluding(ctx, l.ID, []string{"docs/**", "chk/*_test.go", "chk/new.go"}, nil)
	if err != nil {
		t.Fatalf("mixed add: %v", err)
	}
	if len(excluded) != 2 || lane.Scope[len(lane.Scope)-1] != "chk/new.go" {
		t.Errorf("rescope: %v %v", lane.Scope, excluded)
	}
	if got := ExcludedFrom(lane.Scope, mustProtected(t, f)); len(got) != 2 {
		t.Errorf("ExcludedFrom = %v", got)
	}
}

func mustProtected(t *testing.T, f *fixture) []store.CheckFile {
	t.Helper()
	p, err := f.fold.Protected(context.Background(), f.repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pushProblems(t *testing.T, f *fixture, l store.Lane) Verdict {
	t.Helper()
	v, err := f.fold.CheckPushTo(context.Background(), &l, &f.repo, l.Worktree, "origin", []PushRef{pushRef(t, l.Worktree, l.Branch)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

const checksMessage = "changes approved check files (decided by the person; raise a decision to change them):"

func TestPushRefusesCheckFileChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/impl.go": "package src\n", "src/keep_test.go": "package src\n// keep\n"})
	f.approve("src/a_test.go")
	l := f.open("work") // scope src/**: the marked file is inside it

	// A lane with a legitimate change passes.
	f.commit(l.Worktree, "src/ok.go")
	if v := pushProblems(t, f, l); !v.OK {
		t.Fatalf("legitimate push: %+v", v)
	}

	// Editing.
	os.WriteFile(filepath.Join(l.Worktree, "src/a_test.go"), []byte("package src\n// weakened\n"), 0o644)
	gitT(t, l.Worktree, "commit", "-q", "-am", "edit the check")
	v := pushProblems(t, f, l)
	if v.OK || v.rule != crook.RuleScopePush || !strings.Contains(strings.Join(v.Problems, "\n"), checksMessage+"\n    src/a_test.go") {
		t.Fatalf("edit: %+v (rule %q)", v, v.rule)
	}
	// It is not also reported as out of scope.
	if strings.Contains(strings.Join(v.Problems, "\n"), "outside lane") {
		t.Errorf("reported twice: %v", v.Problems)
	}
	gitT(t, l.Worktree, "reset", "-q", "--hard", "HEAD~1")

	// Deleting.
	gitT(t, l.Worktree, "rm", "-q", "src/a_test.go")
	gitT(t, l.Worktree, "commit", "-q", "-m", "delete the check")
	if v := pushProblems(t, f, l); v.OK || !strings.Contains(strings.Join(v.Problems, "\n"), checksMessage) {
		t.Fatalf("delete: %+v", v)
	}
	gitT(t, l.Worktree, "reset", "-q", "--hard", "HEAD~1")

	// Renaming away: a delete plus an add.
	gitT(t, l.Worktree, "mv", "src/a_test.go", "src/moved_test.go")
	gitT(t, l.Worktree, "commit", "-q", "-m", "rename the check")
	if v := pushProblems(t, f, l); v.OK || !strings.Contains(strings.Join(v.Problems, "\n"), "src/a_test.go") {
		t.Fatalf("rename: %+v", v)
	}
	gitT(t, l.Worktree, "reset", "-q", "--hard", "HEAD~1")

	// A marked file plus an out-of-scope file: each is reported on its own.
	os.WriteFile(filepath.Join(l.Worktree, "src/a_test.go"), []byte("package src\n// weakened\n"), 0o644)
	os.WriteFile(filepath.Join(l.Worktree, "stray.txt"), []byte("x\n"), 0o644)
	gitT(t, l.Worktree, "add", ".")
	gitT(t, l.Worktree, "commit", "-q", "-m", "both")
	v = pushProblems(t, f, l)
	all := strings.Join(v.Problems, "\n")
	if v.OK || len(v.Problems) != 2 || !strings.Contains(all, "outside lane work's scope") || !strings.Contains(all, "stray.txt") || !strings.Contains(all, checksMessage) {
		t.Fatalf("both: %+v", v)
	}
	gitT(t, l.Worktree, "reset", "-q", "--hard", "HEAD~1")

	// An unmarked sibling is still free to change.
	os.WriteFile(filepath.Join(l.Worktree, "src/keep_test.go"), []byte("package src\n// fine\n"), 0o644)
	gitT(t, l.Worktree, "commit", "-q", "-am", "edit an unmarked test")
	if v := pushProblems(t, f, l); !v.OK {
		t.Fatalf("unmarked sibling: %+v", v)
	}

	// After a release the same edit passes.
	os.WriteFile(filepath.Join(l.Worktree, "src/a_test.go"), []byte("package src\n// revised\n"), 0o644)
	gitT(t, l.Worktree, "commit", "-q", "-am", "revise the check")
	if v := pushProblems(t, f, l); v.OK {
		t.Fatal("the edit passed while marked")
	}
	if _, err := f.fold.ReleaseChecks(ctx, ReleaseRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go"}, Why: "the check was wrong"}); err != nil {
		t.Fatal(err)
	}
	if v := pushProblems(t, f, l); !v.OK {
		t.Fatalf("after release: %+v", v)
	}
}

func TestPushWithoutMarksIsUnchanged(t *testing.T) {
	f := newFixture(t)
	l := f.open("work")
	f.commit(l.Worktree, "src/a_test.go")
	if v := pushProblems(t, f, l); !v.OK || v.rule != crook.RuleDefault {
		t.Fatalf("%+v (rule %q)", v, v.rule)
	}
}

func TestPushBroadScopeCannotReachMarkedFile(t *testing.T) {
	f := newFixture(t)
	f.land(map[string]string{"internal/x/x_test.go": "package x\n"})
	f.approve("internal/x/x_test.go")
	l, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: "wide", Scope: []string{"**"}})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(l.Worktree, "internal/x/x_test.go"), []byte("package x\n// edited\n"), 0o644)
	gitT(t, l.Worktree, "commit", "-q", "-am", "edit")
	if v := pushProblems(t, f, l.Lane); v.OK || !strings.Contains(strings.Join(v.Problems, ""), "internal/x/x_test.go") {
		t.Fatalf("%+v", v)
	}
}

func TestReleaseChecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/impl.go": "package src\n"})
	it, err := f.fold.Store.AddNextItem(ctx, store.NextItem{Title: "build it", State: store.NextApproved, Source: "person"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.fold.ApproveChecks(ctx, ApproveRequest{RepoID: f.repo.ID, Paths: []string{"src/a_test.go"}, ItemID: it.ID, By: "person (cli)"}); err != nil {
		t.Fatal(err)
	}
	rel := func(force bool, why string, paths ...string) ([]store.CheckFile, error) {
		return f.fold.ReleaseChecks(ctx, ReleaseRequest{RepoID: f.repo.ID, Paths: paths, Why: why, Force: force})
	}
	_, err = rel(false, "", "src/a_test.go")
	refusedWith(t, "no reason", err, "why")
	_, err = rel(false, "wrong", "src/zzz_test.go")
	refusedWith(t, "unmarked", err, "not an approved check file")

	// A build lane open for the item holds the release back; force overrides.
	l := f.open("build")
	if _, err := f.fold.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &l.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = rel(false, "wrong", "src/a_test.go")
	refusedWith(t, "build lane open", err, "lane build is open", "force")
	got, err := rel(true, "wrong", "src/a_test.go")
	if err != nil || len(got) != 1 || got[0].Live() || got[0].ReleaseWhy != "wrong" {
		t.Fatalf("forced release: %+v %v", got, err)
	}
}

func TestCheckStatus(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/b_test.go": "package src\n"})
	f.approve("src/a_test.go", "src/b_test.go")
	st, err := f.fold.CheckStatus(ctx, f.repo.ID)
	if err != nil || len(st) != 2 || st[0].State != CheckCurrent || st[1].State != CheckCurrent {
		t.Fatalf("status: %+v %v", st, err)
	}
	// Someone changes one on the base branch by hand, and removes the other.
	f.land(map[string]string{"src/a_test.go": "package src\n// pushed by hand\n"})
	gitT(t, f.repo.Path, "rm", "-q", "src/b_test.go")
	gitT(t, f.repo.Path, "commit", "-q", "-m", "remove")
	gitT(t, f.repo.Path, "push", "-q", "origin", "HEAD:main")
	st, _ = f.fold.CheckStatus(ctx, f.repo.ID)
	states := map[string]string{}
	for _, s := range st {
		states[s.Path] = s.State
	}
	if states["src/a_test.go"] != CheckChanged || states["src/b_test.go"] != CheckMissing {
		t.Errorf("states = %v", states)
	}
}

func TestShipProblems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.land(map[string]string{"src/a_test.go": "package src\n", "src/impl.go": "package src\n"})
	f.approve("src/a_test.go")
	l := f.open("work")
	f.commit(l.Worktree, "src/ok.go")
	if p, err := f.fold.CheckShip(ctx, l); err != nil || len(p) != 0 {
		t.Fatalf("clean lane: %v %v", p, err)
	}

	// A change to the marked file, committed with the hook skipped.
	os.WriteFile(filepath.Join(l.Worktree, "src/a_test.go"), []byte("package src\n// weakened\n"), 0o644)
	gitT(t, l.Worktree, "commit", "-q", "-am", "edit the check")
	p, err := f.fold.CheckShip(ctx, l)
	if err != nil || len(p) != 1 || !strings.Contains(p[0], checksMessage) || !strings.Contains(p[0], "src/a_test.go") {
		t.Fatalf("changed: %v %v", p, err)
	}

	// A head whose blob differs from the approved one although the lane's diff from
	// its base shows nothing: the base branch itself was edited by hand.
	if _, err := f.fold.Close(ctx, l.ID, true); err != nil {
		t.Fatal(err)
	}
	f.land(map[string]string{"src/a_test.go": "package src\n// pushed by hand\n"})
	l2, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "after", Scope: []string{"lib/**"}})
	if err != nil {
		t.Fatal(err)
	}
	f.commit(l2.Worktree, "lib/x.go")
	p, err = f.fold.CheckShip(ctx, l2.Lane)
	if err != nil || len(p) != 1 || !strings.Contains(p[0], "differ from the approved copy") || !strings.Contains(p[0], "src/a_test.go") {
		t.Fatalf("blob drift: %v %v", p, err)
	}
}
