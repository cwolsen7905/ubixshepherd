package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// A start made while the daemon drains is kept, not refused; the next daemon starts it.
func TestDrainHoldsAStartAndTheNextDaemonRunsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
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
	g(root, "init", "-q", "--bare", origin)
	app := filepath.Join(root, "ws", "app")
	g(root, "clone", "-q", origin, app)
	os.WriteFile(filepath.Join(app, "README.md"), []byte("hi\n"), 0o644)
	g(app, "add", ".")
	g(app, "commit", "-q", "-m", "init")
	g(app, "push", "-q", "origin", "HEAD:main")

	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	agent := filepath.Join(root, "agent")
	os.WriteFile(agent, []byte("#!/bin/sh\ncat >/dev/null\necho done\n"), 0o755)
	serverOn := func() *Server {
		s, err := NewServer(st, config.Default(), "/cfg", log)
		if err != nil {
			t.Fatal(err)
		}
		s.Runner = &dispatch.Runner{Store: st, Config: config.Default(), Dir: filepath.Join(root, "runs"), Log: log}
		dispatch.SetLookPath(s.Runner, func(string) (string, error) { return agent, nil })
		return s
	}
	s := serverOn()
	w, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: filepath.Join(root, "ws")}, []store.Repo{{Name: "app", Path: app}})
	repos, _ := st.Repos(ctx, w.ID)
	opened, err := s.Fold.Open(ctx, fold.OpenRequest{RepoID: repos[0].ID, Name: "work", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	var d api.Draining
	if code := call(t, ts, s.Token, "POST", api.PathDrain, api.DrainRequest{TimeoutSeconds: 600}, &d); code != http.StatusAccepted {
		t.Fatalf("drain: %d", code)
	}
	var stat api.Status
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		call(t, ts, s.Token, "GET", api.PathStatus, nil, &stat)
		if stat.Draining != nil && stat.Draining.Drained {
			break
		}
	}
	if stat.Draining == nil || !stat.Draining.Drained || stat.Draining.Runs != 0 {
		t.Fatalf("status while draining with nothing running = %+v", stat.Draining)
	}

	var held api.RunView
	code := call(t, ts, s.Token, "POST", api.PathRuns, dispatch.StartRequest{LaneID: opened.Lane.ID, Agent: "claude", Prompt: "after"}, &held)
	if code != http.StatusAccepted || !strings.Contains(held.Held, "Shepherd is restarting") || held.ID != 0 {
		t.Fatalf("start while draining: %d %+v", code, held)
	}
	if runs, _ := st.Runs(ctx, 0, "", 10); len(runs) != 0 {
		t.Fatalf("a run began while draining: %+v", runs)
	}

	// The restart: a daemon on the same store answers, then starts what was held.
	next := serverOn()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- next.Run(runCtx, filepath.Join(root, "daemon.json")) }()
	defer func() { cancel(); <-done }()
	var runs []store.Run
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if runs, _ = st.Runs(ctx, 0, store.RunSucceeded, 10); len(runs) == 1 {
			break
		}
	}
	if len(runs) != 1 || runs[0].Prompt != "after" {
		t.Fatalf("after the restart the held start did not run: %+v", runs)
	}
	if h, _ := st.HeldStarts(ctx); len(h) != 0 {
		t.Errorf("held starts left: %+v", h)
	}
}

func TestDrainRefusesANegativeTimeout(t *testing.T) {
	s, ts := newServer(t)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: t.TempDir(), Log: s.Log}
	if code := call(t, ts, s.Token, "POST", api.PathDrain, api.DrainRequest{TimeoutSeconds: -1}, nil); code != http.StatusBadRequest {
		t.Errorf("negative timeout: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathDrain, api.DrainRequest{TimeoutSeconds: 10_000_000_000}, nil); code != http.StatusBadRequest {
		t.Errorf("huge timeout: %d", code)
	}
	// An agent's token cannot drain the daemon.
	worker, _ := s.MintWorker(1)
	if code := call(t, ts, worker, "POST", api.PathDrain, api.DrainRequest{TimeoutSeconds: 1}, nil); code != http.StatusForbidden {
		t.Errorf("worker drains: %d", code)
	}
}

// A stop made through the API during a drain is the restart's only when its body says
// so: the run ends interrupted with the reason restart. A bare stop is the person's.
func TestStopForRestartThroughTheAPIIsContinuedAfterTheRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
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
	g(root, "init", "-q", "--bare", origin)
	app := filepath.Join(root, "ws", "app")
	g(root, "clone", "-q", origin, app)
	os.WriteFile(filepath.Join(app, "README.md"), []byte("hi\n"), 0o644)
	g(app, "add", ".")
	g(app, "commit", "-q", "-m", "init")
	g(app, "push", "-q", "origin", "HEAD:main")

	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	agent := filepath.Join(root, "agent")
	os.WriteFile(agent, []byte("#!/bin/sh\ncat >/dev/null\nexec sleep 30\n"), 0o755)
	s, err := NewServer(st, config.Default(), "/cfg", log)
	if err != nil {
		t.Fatal(err)
	}
	s.Runner = &dispatch.Runner{Store: st, Config: config.Default(), Dir: filepath.Join(root, "runs"), Log: log}
	dispatch.SetLookPath(s.Runner, func(string) (string, error) { return agent, nil })
	w, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: filepath.Join(root, "ws")}, []store.Repo{{Name: "app", Path: app}})
	repos, _ := st.Repos(ctx, w.ID)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	start := func(name, scope string) int64 {
		t.Helper()
		opened, err := s.Fold.Open(ctx, fold.OpenRequest{RepoID: repos[0].ID, Name: name, Scope: []string{scope}})
		if err != nil {
			t.Fatal(err)
		}
		var run api.RunView
		if code := call(t, ts, s.Token, "POST", api.PathRuns, dispatch.StartRequest{LaneID: opened.Lane.ID, Agent: "claude", Prompt: name}, &run); code >= 300 || run.ID == 0 {
			t.Fatalf("start %s: %d %+v", name, code, run)
		}
		return run.ID
	}
	desk, person := start("desk", "a/**"), start("person", "b/**")

	if code := call(t, ts, s.Token, "POST", api.PathDrain, api.DrainRequest{TimeoutSeconds: 600}, nil); code != http.StatusAccepted {
		t.Fatalf("drain: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathRunStop(desk), api.StopRun{ForRestart: true}, nil); code != http.StatusAccepted {
		t.Fatalf("desk stop: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathRunStop(person), nil, nil); code != http.StatusAccepted {
		t.Fatalf("person stop: %d", code)
	}
	var stat api.Status
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		call(t, ts, s.Token, "GET", api.PathStatus, nil, &stat)
		if stat.Draining != nil && stat.Draining.Drained {
			break
		}
	}
	if stat.Draining == nil || !stat.Draining.Drained {
		t.Fatalf("the drain did not finish: %+v", stat.Draining)
	}
	if got, _ := st.Run(ctx, desk); got.State != store.RunInterrupted || got.Reason != store.ReasonRestart {
		t.Errorf("desk's stop: run = %+v, want interrupted for the restart", got)
	}
	if got, _ := st.Run(ctx, person); got.State != store.RunStopped || got.Reason != "" {
		t.Errorf("person's stop: run = %+v, want stopped", got)
	}
}
