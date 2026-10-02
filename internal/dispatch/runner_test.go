package dispatch

import (
	"context"
	"errors"
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
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// fakeAgent is a stand-in for an agent CLI: it commits a file in the scope, optionally
// one outside it, then tries to push and records whether that worked.
const fakeAgent = `#!/bin/sh
echo "fake agent in $(pwd), run $SHEPHERD_RUN, lane $SHEPHERD_LANE"
case "$MODE" in sleep) sleep 30 ;; esac
mkdir -p src && echo work > src/work.txt
git add src/work.txt && git commit -q -m "agent work"
if [ "$MODE" = stray ]; then echo x > stray.txt && git add stray.txt && git commit -q -m stray; fi
if git push -q --no-verify origin HEAD 2>/dev/null; then echo "PUSH WORKED"; else echo "push blocked"; fi
echo "token glpat-AbCdEfGhIjKlMnOpQrStUv in output"
[ "$MODE" = fail ] && exit 3
exit 0
`

type fixture struct {
	runner *Runner
	st     store.Store
	lane   store.Lane
	origin string
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("MODE", mode)
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@e", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@e"} {
		t.Setenv(k, v)
	}
	root, _ := paths.Canonical(t.TempDir())
	g := func(dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	g(root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	g(root, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644)
	g(seed, "add", ".")
	g(seed, "commit", "-q", "-m", "init")
	g(seed, "push", "-q", "origin", "HEAD:main")
	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	g(ws, "clone", "-q", origin, filepath.Join(ws, "app"))

	st, err := sqlite.Open(context.Background(), filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w, _ := st.SaveWorkspace(context.Background(), store.Workspace{Name: "ws", Path: ws}, []store.Repo{{Name: "app", Path: filepath.Join(ws, "app")}})
	repos, _ := st.Repos(context.Background(), w.ID)
	cfg := config.Default()
	f := &fold.Fold{Store: st, Config: cfg}
	opened, err := f.Open(context.Background(), fold.OpenRequest{RepoID: repos[0].ID, Name: "work", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "fake-agent")
	os.WriteFile(agent, []byte(fakeAgent), 0o755)
	r := &Runner{
		Store: st, Config: cfg, Dir: filepath.Join(root, "runs"),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		lookPath: func(string) (string, error) { return agent, nil },
	}
	return &fixture{runner: r, st: st, lane: opened.Lane, origin: origin}
}

func (f *fixture) wait(t *testing.T, id int64) store.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err := f.st.Run(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if run.State != store.RunRunning {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("run did not end")
	return store.Run{}
}

func TestRunRecordsOutcomeAndBlocksPush(t *testing.T) {
	f := newFixture(t, "ok")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "do the work"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if done.State != store.RunSucceeded || done.Commits != 1 || len(done.Outside) != 0 || *done.ExitCode != 0 {
		t.Errorf("run = %+v", done)
	}
	log, _ := os.ReadFile(done.Log)
	for _, want := range []string{"fake agent in " + f.lane.Worktree, "lane work", "push blocked", "[REDACTED]", "succeeded (exit 0) with 1 commit"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(string(log), "glpat-") {
		t.Error("log not redacted")
	}
	if out, _ := exec.Command("git", "-C", f.origin, "branch", "--list", "work").Output(); len(out) > 0 {
		t.Error("the agent's push reached origin")
	}
}

func TestRunFlagsWorkOutsideScopeAndFailures(t *testing.T) {
	f := newFixture(t, "stray")
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "copilot", Prompt: "x"})
	done := f.wait(t, run.ID)
	if done.Commits != 2 || len(done.Outside) != 1 || done.Outside[0] != "stray.txt" {
		t.Errorf("stray run = %+v", done)
	}

	f2 := newFixture(t, "fail")
	run, _ = f2.runner.Start(context.Background(), StartRequest{LaneID: f2.lane.ID, Agent: "cursor", Prompt: "x"})
	if done := f2.wait(t, run.ID); done.State != store.RunFailed || *done.ExitCode != 3 {
		t.Errorf("failing run = %+v", done)
	}
}

func TestRunRefusals(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "gpt-9", Prompt: "x"}); !errors.Is(err, ErrRefused) {
		t.Errorf("unknown agent: %v", err)
	}
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "  "}); !errors.Is(err, ErrRefused) {
		t.Errorf("empty task: %v", err)
	}
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "copilot", Prompt: "y"}); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "one agent per lane") {
		t.Errorf("second run in lane: %v", err)
	}
	// The lane cannot be closed, nor pushed from, while the agent runs.
	fo := &fold.Fold{Store: f.st, Config: f.runner.Config}
	if _, err := fo.Close(ctx, f.lane.ID, true); !errors.Is(err, fold.ErrRefused) {
		t.Errorf("close during run: %v", err)
	}
	if v, _ := fo.CheckPush(ctx, &f.lane, f.lane.Worktree, nil); v.OK {
		t.Error("push allowed during a run")
	}

	if err := f.runner.Stop(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if done := f.wait(t, run.ID); done.State != store.RunStopped {
		t.Errorf("stopped run = %+v", done)
	}
}

func TestMaxRuns(t *testing.T) {
	f := newFixture(t, "sleep")
	f.runner.Config.Daemon.MaxRuns = 1
	ctx := context.Background()
	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.runner.Stop(ctx, run.ID); f.wait(t, run.ID) }()
	// A second lane, so only the machine-wide limit can refuse it.
	other, err := (&fold.Fold{Store: f.st, Config: f.runner.Config}).Open(ctx, fold.OpenRequest{RepoID: f.lane.RepoID, Name: "other", Scope: []string{"docs/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Start(ctx, StartRequest{LaneID: other.ID, Agent: "claude", Prompt: "y"}); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "max_runs") {
		t.Errorf("over the limit: %v", err)
	}
}

func TestRecoverMarksInterrupted(t *testing.T) {
	f := newFixture(t, "ok")
	ctx := context.Background()
	stale, _ := f.st.CreateRun(ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: "l"})
	if err := f.runner.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.Run(ctx, stale.ID); got.State != store.RunInterrupted || got.Ended == nil {
		t.Errorf("after recover = %+v", got)
	}
}

func TestAdapterArgs(t *testing.T) {
	a, _ := AdapterFor("claude")
	args := a.Args("PROMPT", "opus", "make check", "/w")
	if args[0] != "-p" || args[1] != "PROMPT" {
		t.Errorf("claude: the prompt must follow -p before the tool lists: %v", args)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--disallowedTools Bash(git push:*)", "Bash(make check:*)", "--model opus", "--permission-mode acceptEdits"} {
		if !strings.Contains(joined, want) {
			t.Errorf("claude args lack %q: %v", want, args)
		}
	}
	c, _ := AdapterFor("copilot")
	if j := strings.Join(c.Args("P", "", "make check", "/w"), " "); !strings.Contains(j, "--deny-tool shell(git push)") || !strings.Contains(j, "shell(make)") {
		t.Errorf("copilot args: %s", j)
	}
	cu, _ := AdapterFor("cursor")
	if j := strings.Join(cu.Args("P", "", "", "/w"), " "); !strings.Contains(j, "--workspace /w") {
		t.Errorf("cursor args: %s", j)
	}
	if b := Brief("fix it", "l", "r", "l", "main", "/w", []string{"src/**"}, "make check"); !strings.Contains(b, "Do not push") || !strings.Contains(b, "src/**") || !strings.HasSuffix(b, "fix it\n") {
		t.Errorf("brief:\n%s", b)
	}
}
