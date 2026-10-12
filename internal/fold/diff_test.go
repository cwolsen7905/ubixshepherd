package fold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func writeFile(t *testing.T, dir, file, body string) {
	t.Helper()
	p := filepath.Join(dir, file)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findFile(fs []DiffFileInfo, path string) *DiffFileInfo {
	for i := range fs {
		if fs[i].Path == path {
			return &fs[i]
		}
	}
	return nil
}

func TestDiffGroupsScopeAndRedaction(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/diff")
	ctx := context.Background()

	// Committed: one in scope, one outside, a rename of a base file, a binary.
	writeFile(t, l.Worktree, "src/a.go", "package a\n")
	writeFile(t, l.Worktree, "docs/x.md", "x\n")
	writeFile(t, l.Worktree, "src/bin.dat", "a\x00b\x00")
	gitT(t, l.Worktree, "add", ".")
	gitT(t, l.Worktree, "commit", "-q", "-m", "work with api_key=abcd1234efgh in the subject")
	gitT(t, l.Worktree, "mv", "README.md", "src/README.md")
	gitT(t, l.Worktree, "commit", "-q", "-m", "move the readme")
	// Uncommitted: a staged edit, an unstaged edit, an untracked file, a secret.
	writeFile(t, l.Worktree, "src/a.go", "package a\n\nvar x = 1\n")
	gitT(t, l.Worktree, "add", "src/a.go")
	writeFile(t, l.Worktree, "src/a.go", "package a\n\nvar x = 2\n")
	writeFile(t, l.Worktree, "src/new.go", "package a\nconst secret_token = \"s3cr3tvalue99\"\n")
	writeFile(t, l.Worktree, "stray.txt", "invented\n")

	d, err := f.fold.Diff(ctx, l.ID, DiffRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != DiffStat || d.Lane != "feat/diff" || d.Head == "" || d.MergeBase == "" || d.BaseRef != "origin/main" {
		t.Errorf("heads = %+v", d)
	}
	if d.CommitsTotal != 2 || len(d.Commits) != 2 || d.Commits[1].Subject != "move the readme" {
		t.Errorf("commits = %+v", d.Commits)
	}
	if strings.Contains(d.Commits[0].Subject, "abcd1234efgh") {
		t.Errorf("subject not redacted: %s", d.Commits[0].Subject)
	}
	if x := findFile(d.Committed, "docs/x.md"); x == nil || x.InScope || x.Status != "added" {
		t.Errorf("docs/x.md = %+v", x)
	}
	if r := findFile(d.Committed, "src/README.md"); r == nil || r.Status != "renamed" || r.OldPath != "README.md" || !r.InScope {
		t.Errorf("rename = %+v", r)
	}
	if b := findFile(d.Committed, "src/bin.dat"); b == nil || !b.Binary || b.Size != 4 {
		t.Errorf("binary = %+v", b)
	}
	if a := findFile(d.Uncommitted, "src/a.go"); a == nil || a.Added != 2 || a.Removed != 0 || !a.InScope {
		t.Errorf("uncommitted a.go = %+v (staged and unstaged count against HEAD)", a)
	}
	if s := findFile(d.Untracked, "stray.txt"); s == nil || s.InScope || s.Size != 9 || s.Status != "untracked" {
		t.Errorf("stray.txt = %+v", s)
	}
	if d.OutOfScope != 2 {
		t.Errorf("out of scope = %d, want 2 (docs/x.md, stray.txt)", d.OutOfScope)
	}
	if d.Redacted == 0 || !strings.Contains(strings.Join(d.Notes, "\n"), "masked") {
		t.Errorf("redacted = %d, notes = %v", d.Redacted, d.Notes)
	}

	// Files mode adds hunk headers and no bodies.
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{Mode: DiffFiles})
	if err != nil {
		t.Fatal(err)
	}
	if a := findFile(d.Uncommitted, "src/a.go"); a == nil || len(a.Hunks) == 0 || !strings.HasPrefix(a.Hunks[0], "@@") {
		t.Errorf("hunks = %+v", a)
	}

	// One file: committed and uncommitted together, redacted.
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{File: "src/a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != DiffFile || !strings.Contains(d.Patch, "+var x = 2") || !strings.Contains(d.Patch, "+package a") {
		t.Errorf("patch = %q", d.Patch)
	}
	// An untracked file is shown by its start, and its secret is masked.
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{File: "src/new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Patch, "package a") || strings.Contains(d.Patch, "s3cr3tvalue99") || d.Redacted == 0 {
		t.Errorf("untracked = %q redacted=%d", d.Patch, d.Redacted)
	}
	// A file outside the worktree is refused.
	for _, bad := range []string{"../x", "/etc/passwd", "."} {
		if _, err := f.fold.Diff(ctx, l.ID, DiffRequest{File: bad}); err == nil {
			t.Errorf("file %q accepted", bad)
		}
	}
	// An unchanged tracked file says so.
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{File: "src/README.md"})
	if err != nil || d.Patch == "" {
		t.Errorf("renamed file: %q %v", d.Patch, err)
	}
}

func TestDiffRangeLeavesOutTheWorktree(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/range")
	ctx := context.Background()
	f.commit(l.Worktree, "src/one.go")
	first := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/two.go")
	second := gitT(t, l.Worktree, "rev-parse", "HEAD")
	writeFile(t, l.Worktree, "src/loose.go", "x\n")

	d, err := f.fold.Diff(ctx, l.ID, DiffRequest{Since: first, Until: second})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commits) != 1 || len(d.Committed) != 1 || d.Committed[0].Path != "src/two.go" || len(d.Untracked) != 0 {
		t.Errorf("range = %+v", d)
	}
	if _, err := f.fold.Diff(ctx, l.ID, DiffRequest{Since: "--output=x"}); err == nil {
		t.Error("a flag accepted as a SHA")
	}
}

func TestDiffLimits(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/big")
	ctx := context.Background()

	for i := 0; i < DiffMaxCommits+3; i++ {
		writeFile(t, l.Worktree, fmt.Sprintf("src/c%02d.go", i), "x\n")
		gitT(t, l.Worktree, "add", ".")
		gitT(t, l.Worktree, "commit", "-q", "-m", fmt.Sprintf("commit %02d", i))
	}
	for i := 0; i < 20; i++ {
		writeFile(t, l.Worktree, fmt.Sprintf("tree/u%03d.txt", i), "x\n")
	}
	var big strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&big, "line %05d of a long generated file\n", i)
	}
	writeFile(t, l.Worktree, "src/big.txt", big.String())
	gitT(t, l.Worktree, "add", "src/big.txt")
	gitT(t, l.Worktree, "commit", "-q", "-m", "big")

	d, err := f.fold.Diff(ctx, l.ID, DiffRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commits) != DiffMaxCommits || d.CommitsTotal != DiffMaxCommits+4 || !d.Truncated {
		t.Errorf("commits = %d of %d, truncated %v", len(d.Commits), d.CommitsTotal, d.Truncated)
	}
	if d.Commits[len(d.Commits)-1].Subject != "big" {
		t.Errorf("the newest commit comes last: %+v", d.Commits[len(d.Commits)-1])
	}
	if !strings.Contains(strings.Join(d.Notes, "\n"), "older commit") {
		t.Errorf("notes = %v", d.Notes)
	}
	if n := len(d.Committed) + len(d.Uncommitted) + len(d.Untracked); n != DiffMaxCommits+3+1+20 || d.FilesMore != 0 {
		t.Errorf("files = %d more %d", n, d.FilesMore)
	}

	// More than the file cap: 200 listed, the rest counted.
	for i := 0; i < 200; i++ {
		writeFile(t, l.Worktree, fmt.Sprintf("tree/v%03d.txt", i), "x\n")
	}
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(d.Committed) + len(d.Uncommitted) + len(d.Untracked); n != DiffMaxFiles || d.FilesMore == 0 || !d.Truncated {
		t.Errorf("files = %d more %d truncated %v", n, d.FilesMore, d.Truncated)
	}

	// A patch over 64 KiB is cut on a line (here: one hunk) and says so.
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{File: "src/big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || len(d.Patch) > DiffMaxFilePatch || !strings.HasSuffix(d.Patch, "\n") ||
		!strings.Contains(strings.Join(d.Notes, "\n"), "cut at 64 KiB") {
		t.Errorf("patch %d bytes, truncated %v, notes %v", len(d.Patch), d.Truncated, d.Notes)
	}
}

func TestDiffRefusesAClosedLane(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/gone")
	if _, err := f.fold.Close(context.Background(), l.ID, false); err != nil {
		t.Fatal(err)
	}
	_, err := f.fold.Diff(context.Background(), l.ID, DiffRequest{})
	if err == nil || !strings.Contains(err.Error(), "no worktree") {
		t.Errorf("err = %v", err)
	}
}

func TestDiffRunRangeAndGateBadge(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/gate")
	ctx := context.Background()
	st := f.fold.Store

	base := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/one.go")
	first := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/two.go")
	second := gitT(t, l.Worktree, "rev-parse", "HEAD")

	d, err := f.fold.Diff(ctx, l.ID, DiffRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Gate.State != store.GateNotRun || d.Gate.At != nil {
		t.Errorf("no record: gate = %+v", d.Gate)
	}

	// A pass on the first commit is stale once a second lands; a record on the head wins.
	st.AddGateResult(ctx, store.GateResult{LaneID: l.ID, SHA: first, Gate: "make check", Passed: true})
	d, _ = f.fold.Diff(ctx, l.ID, DiffRequest{})
	if d.Gate.State != store.GateStale || d.Gate.SHA != first || d.Gate.Command != "make check" {
		t.Errorf("earlier commit: gate = %+v", d.Gate)
	}
	st.AddGateResult(ctx, store.GateResult{LaneID: l.ID, SHA: second, Gate: "make check", Passed: false})
	d, _ = f.fold.Diff(ctx, l.ID, DiffRequest{})
	if d.Gate.State != store.GateFailed || d.Gate.SHA != second {
		t.Errorf("failing head: gate = %+v", d.Gate)
	}
	st.AddGateResult(ctx, store.GateResult{LaneID: l.ID, SHA: second, Gate: "make check", Passed: true})
	d, _ = f.fold.Diff(ctx, l.ID, DiffRequest{})
	if d.Gate.State != store.GatePassed || d.Gate.Source != store.GateSourceShip {
		t.Errorf("passing head: gate = %+v", d.Gate)
	}
	if heads := BranchHeads(ctx, f.repo.Path, []store.Lane{l}); heads[l.ID] != second {
		t.Errorf("branch head = %q, want %q", heads[l.ID], second)
	}

	// A run's own range: start_sha..end_sha, with no worktree files.
	run, err := st.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", State: store.RunRunning, StartSHA: first})
	if err != nil {
		t.Fatal(err)
	}
	run.State, run.EndSHA = store.RunSucceeded, second
	if err := st.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	writeFile(t, l.Worktree, "src/loose.go", "x\n")
	d, err = f.fold.Diff(ctx, l.ID, DiffRequest{Run: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commits) != 1 || len(d.Committed) != 1 || d.Committed[0].Path != "src/two.go" || len(d.Untracked) != 0 {
		t.Errorf("run range = %+v", d)
	}
	if d.Gate.State != store.GatePassed {
		t.Errorf("the badge is for the head whatever the range: %+v", d.Gate)
	}
	if _, err := f.fold.Diff(ctx, l.ID, DiffRequest{Run: run.ID, Since: base}); err == nil {
		t.Error("a run and a range accepted together")
	}
	otherLane, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "feat/other", Scope: []string{"lib/**"}})
	if err != nil {
		t.Fatal(err)
	}
	other := otherLane.Lane
	if _, err := f.fold.Diff(ctx, other.ID, DiffRequest{Run: run.ID}); err == nil {
		t.Error("another lane's run accepted")
	}
}
