package watch

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// followFixture is a workspace of two repos: app follows core.
type followFixture struct {
	fixture
	app, core store.Repo
	coreSeed  string
	g         func(dir string, args ...string)
}

func newFollowFixture(t *testing.T, f config.Follow) *followFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@e", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@e"} {
		t.Setenv(k, v)
	}
	root, _ := paths.Canonical(t.TempDir())
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	seeds := map[string]string{}
	for _, name := range []string{"core", "app"} {
		origin := filepath.Join(root, name+".git")
		g(root, "init", "-q", "--bare", "-b", "main", origin)
		seed := filepath.Join(root, name+"-seed")
		g(root, "clone", "-q", origin, seed)
		os.WriteFile(filepath.Join(seed, "README.md"), []byte(name+"\n"), 0o644)
		g(seed, "add", ".")
		g(seed, "commit", "-q", "-m", "init")
		g(seed, "push", "-q", "origin", "HEAD:main")
		g(ws, "clone", "-q", origin, filepath.Join(ws, name))
		seeds[name] = seed
	}
	g(seeds["core"], "tag", "-a", "-m", "first", "v0.1.0")
	g(seeds["core"], "push", "-q", "origin", "v0.1.0")

	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: ws}, []store.Repo{
		{Name: "app", Path: filepath.Join(ws, "app"), Remote: "git@gl.example.com:g/app.git"},
		{Name: "core", Path: filepath.Join(ws, "core"), Remote: "git@gl.example.com:g/core.git"},
	})
	repos, _ := st.Repos(ctx, w.ID)
	byName := map[string]store.Repo{}
	for _, r := range repos {
		byName[r.Name] = r
	}
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {Follows: []config.Follow{f}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	fo := &fold.Fold{Store: st, Config: cfg}
	bin := filepath.Join(root, "agent")
	os.WriteFile(bin, []byte(agent), 0o755)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := &dispatch.Runner{Store: st, Config: cfg, Dir: filepath.Join(root, "runs"), Log: log}
	dispatch.SetLookPath(r, func(string) (string, error) { return bin, nil })
	ff := &fakeForge{pipes: map[string]*forge.Pipeline{}}
	wa := &Watcher{Store: st, Fold: fo, Runner: r, Log: log, Interval: time.Hour,
		ForgeFor: func(string) (forge.Forge, error) { return ff, nil }}
	t.Cleanup(r.Wait)
	return &followFixture{
		fixture: fixture{w: wa, f: ff, st: st, run: r, ctx: ctx, agent: bin},
		app:     byName["app"], core: byName["core"], coreSeed: seeds["core"], g: g,
	}
}

// release commits to core and pushes an annotated tag.
func (f *followFixture) release(t *testing.T, tag, subject string) {
	t.Helper()
	os.WriteFile(filepath.Join(f.coreSeed, tag+".txt"), []byte(tag+"\n"), 0o644)
	f.g(f.coreSeed, "add", ".")
	f.g(f.coreSeed, "commit", "-q", "-m", subject)
	f.g(f.coreSeed, "push", "-q", "origin", "HEAD:main")
	f.g(f.coreSeed, "tag", "-a", "-m", "notes for "+tag, tag)
	f.g(f.coreSeed, "push", "-q", "origin", tag)
}

func (f *followFixture) lanes(t *testing.T) []store.Lane {
	t.Helper()
	lanes, err := f.st.Lanes(f.ctx, f.app.ID)
	if err != nil {
		t.Fatal(err)
	}
	return lanes
}

var bump = config.Follow{
	Repo:  "core",
	Lane:  "chore/{repo}-{major}.{minor}",
	Scope: []string{"deps.txt"},
	Task:  "Move core to ^{major}.{minor} (from {previous}), and commit.",
}

func TestFollowWaitsForThePublishedReleaseAndActsOnce(t *testing.T) {
	f := newFollowFixture(t, bump)

	// The first look records where core is, and starts nothing.
	f.w.Check(f.ctx)
	if got := f.feed(t); !strings.Contains(got, "app follows core from v0.1.0") || len(f.lanes(t)) != 0 {
		t.Fatalf("first sight: lanes %d, feed = %s", len(f.lanes(t)), got)
	}

	f.release(t, "v0.2.0", "core: a new seam")
	f.f.pipes["v0.2.0"] = &forge.Pipeline{ID: 5, Status: "running"}
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if got := f.feed(t); strings.Count(got, "its pipeline is running") != 1 || len(f.lanes(t)) != 0 {
		t.Fatalf("while the pipeline runs: lanes %d, feed = %s", len(f.lanes(t)), got)
	}

	f.f.pipes["v0.2.0"].Status = "success"
	f.w.Check(f.ctx)
	f.settle(t)
	lanes := f.lanes(t)
	if len(lanes) != 1 || lanes[0].Name != "chore/core-0.2" || lanes[0].Scope[0] != "deps.txt" {
		t.Fatalf("lanes = %+v", lanes)
	}
	runs, _ := f.st.Runs(f.ctx, lanes[0].ID, "", 10)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	for _, want := range []string{"core released v0.2.0", "notes for v0.2.0", "- core: a new seam", "Move core to ^0.2 (from v0.1.0)"} {
		if !strings.Contains(runs[0].Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, runs[0].Prompt)
		}
	}
	if got := f.feed(t); !strings.Contains(got, "asked claude to move app to core v0.2.0") {
		t.Errorf("feed = %s", got)
	}

	// Seen again, the release sets off nothing more.
	f.w.Check(f.ctx)
	if n := len(f.lanes(t)); n != 1 {
		t.Errorf("acted twice: %d lanes", n)
	}
}

func TestFollowSkipsReleasesBelowItsBump(t *testing.T) {
	f := newFollowFixture(t, bump)
	f.w.Check(f.ctx)
	f.release(t, "v0.1.1", "core: a fix")
	f.f.pipes["v0.1.1"] = &forge.Pipeline{ID: 6, Status: "success"}
	f.w.Check(f.ctx)
	if got := f.feed(t); !strings.Contains(got, "follows its minor releases and up") || len(f.lanes(t)) != 0 {
		t.Fatalf("lanes %d, feed = %s", len(f.lanes(t)), got)
	}
}

func TestFollowHoldsAFailedRelease(t *testing.T) {
	f := newFollowFixture(t, bump)
	f.w.Check(f.ctx)
	f.release(t, "v0.2.0", "core: a new seam")
	f.f.pipes["v0.2.0"] = &forge.Pipeline{ID: 7, Status: "failed", URL: "https://gl/p/7"}
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if got := f.feed(t); strings.Count(got, "pipeline for v0.2.0 failed") != 1 || len(f.lanes(t)) != 0 {
		t.Fatalf("lanes %d, feed = %s", len(f.lanes(t)), got)
	}
	// A retried pipeline that passes lets it through.
	f.f.pipes["v0.2.0"] = &forge.Pipeline{ID: 8, Status: "success"}
	f.w.Check(f.ctx)
	f.settle(t)
	if n := len(f.lanes(t)); n != 1 {
		t.Errorf("lanes = %d after the retry passed", n)
	}
}

func TestFollowOnTagsAlone(t *testing.T) {
	tagged := bump
	tagged.After = config.Tagged
	f := newFollowFixture(t, tagged)
	f.w.ForgeFor = func(string) (forge.Forge, error) { return nil, os.ErrNotExist }
	f.w.Check(f.ctx)
	f.release(t, "v1.0.0", "core: one")
	f.w.Check(f.ctx)
	f.settle(t)
	if lanes := f.lanes(t); len(lanes) != 1 || lanes[0].Name != "chore/core-1.0" {
		t.Fatalf("lanes = %+v; feed = %s", lanes, f.feed(t))
	}
}

func TestFollowConfigIsChecked(t *testing.T) {
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {Follows: []config.Follow{{Repo: "core", MinBump: "tiny", After: "soon", Agent: "nobody"}}}}
	err := cfg.Validate()
	for _, want := range []string{"scope", "task: empty", "min_bump", "after", "agent"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v lacks %q", err, want)
		}
	}
}
