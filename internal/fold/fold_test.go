package fold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

type fixture struct {
	t      *testing.T
	fold   *Fold
	ws     string
	origin string
	repo   store.Repo
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newFixture makes a bare origin with one commit on main, a clone of it as a workspace
// repo named app, and a Fold over a fresh store.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, _ := paths.Canonical(t.TempDir())
	origin := filepath.Join(root, "origin.git")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	gitT(t, root, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644)
	gitT(t, seed, "add", ".")
	gitT(t, seed, "commit", "-q", "-m", "init")
	gitT(t, seed, "push", "-q", "origin", "HEAD:main")

	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	app := filepath.Join(ws, "app")
	gitT(t, ws, "clone", "-q", origin, app)

	st, err := sqlite.Open(context.Background(), filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w, err := st.SaveWorkspace(context.Background(), store.Workspace{Name: "ws", Path: ws},
		[]store.Repo{{Name: "app", Path: app}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(context.Background(), w.ID)
	return &fixture{t: t, fold: &Fold{Store: st, Config: config.Default()}, ws: ws, origin: origin, repo: repos[0]}
}

func (f *fixture) open(name string) store.Lane {
	f.t.Helper()
	l, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: name, Scope: []string{"src/**"}})
	if err != nil {
		f.t.Fatalf("open %s: %v", name, err)
	}
	return l
}

// commit adds a file in a worktree and commits it.
func (f *fixture) commit(dir, file string) {
	f.t.Helper()
	os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755)
	os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o644)
	gitT(f.t, dir, "add", ".")
	gitT(f.t, dir, "commit", "-q", "-m", "add "+file)
}

func TestOpenCreatesBranchAndWorktree(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/login")
	want := filepath.Join(f.ws, "app-worktrees", "feat-login")
	if l.Worktree != want || l.Branch != "feat/login" || l.Base != "main" || l.State != store.LaneOpen {
		t.Errorf("lane = %+v", l)
	}
	if got := gitT(t, l.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/login" {
		t.Errorf("worktree on %s", got)
	}
	// Started from origin/main.
	if gitT(t, l.Worktree, "rev-parse", "HEAD") != gitT(t, f.repo.Path, "rev-parse", "origin/main") {
		t.Error("lane did not start at origin/main")
	}
}

func TestOpenStartsFromFreshOrigin(t *testing.T) {
	f := newFixture(t)
	// Someone pushes to main after the clone; the lane must include it.
	other := filepath.Join(filepath.Dir(f.ws), "other")
	gitT(t, filepath.Dir(f.ws), "clone", "-q", f.origin, other)
	f.commit(other, "new.txt")
	gitT(t, other, "push", "-q", "origin", "HEAD:main")

	l := f.open("fresh")
	if _, err := os.Stat(filepath.Join(l.Worktree, "new.txt")); err != nil {
		t.Error("lane started from a stale base")
	}
}

func TestOpenRefuses(t *testing.T) {
	f := newFixture(t)
	f.open("taken")
	gitT(t, f.repo.Path, "branch", "local-only")
	cases := map[string]OpenRequest{
		"bad name":       {RepoID: f.repo.ID, Name: "Bad Name", Scope: []string{"x"}},
		"two slashes":    {RepoID: f.repo.ID, Name: "a/b/c", Scope: []string{"x"}},
		"no scope":       {RepoID: f.repo.ID, Name: "noscope"},
		"absolute scope": {RepoID: f.repo.ID, Name: "abs", Scope: []string{"/etc/**"}},
		"escaping scope": {RepoID: f.repo.ID, Name: "esc", Scope: []string{"../other/**"}},
		"name in use":    {RepoID: f.repo.ID, Name: "taken", Branch: "other-branch", Scope: []string{"x"}},
		"branch exists":  {RepoID: f.repo.ID, Name: "local-only", Scope: []string{"x"}},
	}
	for name, req := range cases {
		_, err := f.fold.Open(context.Background(), req)
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want refused", name, err)
		}
	}
	// A refused open leaves no lane behind.
	lanes, _ := f.fold.Store.Lanes(context.Background(), f.repo.ID)
	if len(lanes) != 1 {
		t.Errorf("lanes after refusals = %d", len(lanes))
	}
}

func TestConcurrentOpenSameName(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: "race", Scope: []string{"x"}})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d of 4 concurrent opens succeeded: %v", ok, errs)
	}
}

func TestCloseRefusesThenClosesWhenMerged(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("work")
	f.commit(l.Worktree, "src/a.go")

	// Unmerged.
	if _, err := f.fold.Close(ctx, l.ID, false); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "squash") {
		t.Fatalf("unmerged close: %v", err)
	}
	// Dirty.
	os.WriteFile(filepath.Join(l.Worktree, "scratch.txt"), []byte("x"), 0o644)
	if _, err := f.fold.Close(ctx, l.ID, false); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "scratch.txt") {
		t.Fatalf("dirty close: %v", err)
	}
	os.Remove(filepath.Join(l.Worktree, "scratch.txt"))

	// Merge it the way a forge would, then close.
	gitT(t, l.Worktree, "push", "-q", "origin", "HEAD:main")
	res, err := f.fold.Close(ctx, l.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.BranchDeleted || res.Lane.State != store.LaneClosed || res.Lane.Closed == nil {
		t.Errorf("close result = %+v", res)
	}
	if _, err := os.Stat(l.Worktree); !os.IsNotExist(err) {
		t.Error("worktree left behind")
	}
	// The name is free again.
	f.open("work")
}

func TestForceCloseKeepsUnmergedBranch(t *testing.T) {
	f := newFixture(t)
	l := f.open("squashed")
	f.commit(l.Worktree, "src/b.go")
	res, err := f.fold.Close(context.Background(), l.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.BranchDeleted || len(res.Notes) == 0 {
		t.Errorf("forced close = %+v", res)
	}
	gitT(t, f.repo.Path, "rev-parse", "--verify", "refs/heads/squashed") // still there
}

func TestGC(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	live := f.open("live")
	// A worktree made by hand whose branch is already in origin/main.
	done := filepath.Join(f.ws, "app-worktrees", "by-hand")
	gitT(t, f.repo.Path, "worktree", "add", "-q", "-b", "by-hand", done, "origin/main")
	// One with unmerged work: not stale.
	busy := filepath.Join(f.ws, "app-worktrees", "busy")
	gitT(t, f.repo.Path, "worktree", "add", "-q", "-b", "busy", busy, "origin/main")
	f.commit(busy, "w.txt")

	ws, _ := f.fold.Store.Workspaces(ctx)
	stale, err := f.fold.GC(ctx, ws[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].Branch != "by-hand" {
		t.Errorf("stale = %+v", stale)
	}

	// A lane whose worktree was deleted by hand is reported too.
	os.RemoveAll(live.Worktree)
	stale, _ = f.fold.GC(ctx, ws[0].ID)
	found := false
	for _, s := range stale {
		if s.Lane == "live" {
			found = true
		}
	}
	if !found {
		t.Errorf("orphaned lane not reported: %+v", stale)
	}
}
