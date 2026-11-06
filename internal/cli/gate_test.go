package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
