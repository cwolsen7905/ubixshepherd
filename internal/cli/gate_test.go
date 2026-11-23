package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// An agent's push, under push: agent in a repo with a gate, waits for a passing record
// of that exact commit, which `lane gate` makes. The person's own push is not held.
func TestAgentPushNeedsAPassingGate(t *testing.T) {
	clearRunEnv(t)
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	if code := h.run("lane", "open", "feat/x", "--scope", "src/**"); code != 0 {
		t.Fatalf("lane open: %s", h.err)
	}
	wt := filepath.Join(root, "app-worktrees", "feat-x")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", wt}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(content string) string {
		os.MkdirAll(filepath.Join(wt, "src"), 0o755)
		os.WriteFile(filepath.Join(wt, "src", "a.go"), []byte(content), 0o644)
		git("add", ".")
		git("commit", "-q", "-m", "feat: a")
		return git("rev-parse", "HEAD")
	}
	gate := func(cmd string) {
		cfg := config.Default()
		p := config.Profile{Gate: cmd}
		p.Autonomy.Push = config.Agent
		cfg.Repos = map[string]config.Profile{"app": p}
		h.srv.Fold.SetConfig(cfg)
	}
	push := func(head string) int {
		t.Helper()
		h.env.Cwd = wt
		h.env.Stdin = strings.NewReader("refs/heads/feat/x " + head + " refs/heads/feat/x " + strings.Repeat("0", 40) + "\n")
		return h.run("hook", "pre-push", "origin", git("remote", "get-url", "origin"))
	}
	ctx := context.Background()
	lane, err := h.srv.Store.Lane(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}

	head := commit("package a\n")
	gate("true")

	// The person pushes with no agent running: left alone.
	if code := push(head); code != 0 {
		t.Fatalf("a push with no agent running was held: %s", h.err)
	}

	_, err = h.srv.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: "l"})
	if err != nil {
		t.Fatal(err)
	}
	if code := push(head); code == 0 || !strings.Contains(h.err.String(), "lane gate") {
		t.Fatalf("agent push with no gate record was let through: %d %s", code, h.err)
	}

	h.env.Cwd = wt
	if code := h.run("lane", "gate"); code != 0 {
		t.Fatalf("lane gate: %s %s", h.out, h.err)
	}
	if code := push(head); code != 0 {
		t.Fatalf("agent push after a passing gate refused: %s", h.err)
	}

	// A new commit needs its own run.
	head = commit("package a\n\nvar x = 1\n")
	if code := push(head); code == 0 {
		t.Fatal("a pass for an earlier commit let a later one through")
	}

	// A failing gate is recorded as a failure and does not let the push go.
	gate("false")
	h.env.Cwd = wt
	if code := h.run("lane", "gate"); code == 0 {
		t.Fatal("lane gate reported a failing gate as passed")
	}
	if code := push(head); code == 0 || !strings.Contains(h.err.String(), "failed") {
		t.Fatalf("agent push after a failing gate: %d %s", code, h.err)
	}

	// Uncommitted changes: the gate would check something else, so it does not run.
	gate("true")
	os.WriteFile(filepath.Join(wt, "src", "a.go"), []byte("package a\n// dirty\n"), 0o644)
	h.env.Cwd = wt
	if code := h.run("lane", "gate"); code == 0 {
		t.Fatal("lane gate ran over uncommitted changes")
	}
	git("checkout", "--", ".")

	// A repo with no gate has nothing to check.
	gate("")
	if code := push(head); code != 0 {
		t.Fatalf("agent push with no gate configured refused: %s", h.err)
	}
}

// A worker may have the gate run on its own run's lane and no other.
func TestWorkerGatesOnlyItsOwnLane(t *testing.T) {
	clearRunEnv(t)
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	for _, args := range [][]string{{"feat/x", "--scope", "src/**"}, {"feat/y", "--scope", "other/**"}} {
		if code := h.run(append([]string{"lane", "open"}, args...)...); code != 0 {
			t.Fatalf("lane open: %s", h.err)
		}
	}
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {Gate: "true"}}
	h.srv.Fold.SetConfig(cfg)

	ctx := context.Background()
	x, err := h.srv.Store.Lane(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	y, err := h.srv.Store.Lane(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.srv.Store.CreateRun(ctx, store.Run{LaneID: x.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: "l"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := h.srv.MintWorker(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(h.env.Layout.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	var rt api.Runtime
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatal(err)
	}
	c := client.New("http://"+rt.Addr, tok)
	if _, err := c.GateLane(ctx, y.ID); err == nil || !strings.Contains(err.Error(), "own lane") {
		t.Fatalf("a worker gated another lane: %v", err)
	}
	if g, err := h.srv.Store.LastGateResult(ctx, y.ID); err != nil || g != nil {
		t.Fatalf("a refused request recorded a gate result: %+v %v", g, err)
	}
	if res, err := c.GateLane(ctx, x.ID); err != nil || !res.Passed {
		t.Fatalf("a worker could not gate its own lane: %+v %v", res, err)
	}
}

// The gate tool is listed, takes no argument that names a lane, repo or command, and maps
// to the worker command.
func TestGateToolMapsToWorkerCommand(t *testing.T) {
	for _, tl := range workerTools() {
		if tl.Name != "gate" {
			continue
		}
		if props, _ := tl.InputSchema["properties"].(map[string]any); len(props) != 0 {
			t.Fatalf("the gate tool takes arguments: %v", props)
		}
		args, err := tl.args(map[string]any{"lane": "feat/y", "command": "rm -rf /"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(args, " ") != "worker gate" {
			t.Fatalf("args = %v", args)
		}
		return
	}
	t.Fatal("no gate worker tool")
}

// `worker gate` records a pass for the run's own lane head, which the pre-push hook then
// accepts, and refuses when the worktree has uncommitted changes.
func TestWorkerGateRecordsPassForOwnLane(t *testing.T) {
	clearRunEnv(t)
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	if code := h.run("lane", "open", "feat/x", "--scope", "src/**"); code != 0 {
		t.Fatalf("lane open: %s", h.err)
	}
	cfg := config.Default()
	p := config.Profile{Gate: "true"}
	p.Autonomy.Push = config.Agent
	cfg.Repos = map[string]config.Profile{"app": p}
	h.srv.Fold.SetConfig(cfg)

	wt := filepath.Join(root, "app-worktrees", "feat-x")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", wt}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	os.MkdirAll(filepath.Join(wt, "src"), 0o755)
	os.WriteFile(filepath.Join(wt, "src", "a.go"), []byte("package a\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "feat: a")
	head := git("rev-parse", "HEAD")

	ctx := context.Background()
	lane, err := h.srv.Store.Lane(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.srv.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: "l"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := h.srv.MintWorker(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_RUN", fmt.Sprint(run.ID))
	t.Setenv("SHEPHERD_TOKEN", tok)
	b, err := os.ReadFile(h.env.Layout.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	var rt api.Runtime
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEPHERD_URL", "http://"+rt.Addr)

	// Uncommitted changes: refused, nothing recorded.
	os.WriteFile(filepath.Join(wt, "src", "a.go"), []byte("package a\n// dirty\n"), 0o644)
	if code := h.run("worker", "gate"); code == 0 {
		t.Fatal("worker gate ran over uncommitted changes")
	}
	git("checkout", "--", ".")

	h.out.Reset()
	if code := h.run("worker", "gate"); code != 0 || !strings.Contains(h.out.String(), "passed") {
		t.Fatalf("worker gate: %d %s %s", code, h.out, h.err)
	}
	g, err := h.srv.Store.LastGateResult(ctx, lane.ID)
	if err != nil || g == nil || !g.Passed || g.SHA != head {
		t.Fatalf("no pass recorded for the head: %+v %v", g, err)
	}

	// An argument is not accepted: nothing picks another lane.
	if code := h.run("worker", "gate", "feat/y"); code == 0 {
		t.Fatal("worker gate took a lane argument")
	}

	// The hook accepts the push of that commit.
	h.env.Cwd = wt
	h.env.Stdin = strings.NewReader("refs/heads/feat/x " + head + " refs/heads/feat/x " + strings.Repeat("0", 40) + "\n")
	if code := h.run("hook", "pre-push", "origin", git("remote", "get-url", "origin")); code != 0 {
		t.Fatalf("push after worker gate refused: %s", h.err)
	}
}
