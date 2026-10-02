package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// lane_list with no repo lists every open lane in every repo of the workspace, wherever
// the MCP server runs: the root, inside a repo, inside a lane's worktree. Each lane says
// who opened it.
func TestMCPLaneListEveryRepo(t *testing.T) {
	t.Setenv("SHEPHERD_CLIENT", "desk")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("SHEPHERD_RUN", "")
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	for _, name := range []string{"lib", "tools/kit"} {
		if out, err := exec.Command("git", "clone", "-q", filepath.Join(filepath.Dir(root), "origin.git"), filepath.Join(root, name)).CombinedOutput(); err != nil {
			t.Fatalf("clone %s: %v %s", name, err, out)
		}
	}
	// --all: clones of one origin look like duplicate working copies, which --yes skips.
	if code := h.run("init", root, "--all"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"app","name":"feat/a","scope":["src/**"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"lib","name":"feat/b","scope":["bin/**"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"tools/kit","name":"feat/c","scope":["bin/e2e/**"]}}}`,
	)
	for i, r := range resps {
		if text, isErr := toolText(t, r); isErr || !strings.Contains(text, "opened by desk (claude, ") {
			t.Fatalf("lane_open %d: %v %s", i+1, isErr, text)
		}
	}
	// One more from the CLI, so the list shows two surfaces.
	t.Setenv("CLAUDECODE", "")
	h.env.Client = "cli"
	if code := h.run("lane", "open", "fix/d", "--repo", "lib", "--scope", "docs/**"); code != 0 || !strings.Contains(h.out.String(), "opened by cli (pid ") {
		t.Fatalf("cli open: %d %s%s", code, h.out, h.err)
	}

	for _, cwd := range []string{root, app, filepath.Join(root, "app-worktrees", "feat-a"), filepath.Join(root, "tools/kit")} {
		h.env.Cwd = cwd
		resps = mcpExchange(t, h.env, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lane_list","arguments":{}}}`)
		text, isErr := toolText(t, resps[0])
		if isErr {
			t.Fatalf("lane_list from %s: %s", cwd, text)
		}
		for _, want := range []string{"app", "feat/a", "lib", "feat/b", "fix/d", "tools/kit", "feat/c", "bin/e2e/**", "opened by desk (claude, ", "opened by cli (pid "} {
			if !strings.Contains(text, want) {
				t.Errorf("lane_list from %s lacks %q:\n%s", cwd, want, text)
			}
		}
	}

	// where shows the origin of the lane it is in.
	h.env.Cwd = filepath.Join(root, "lib-worktrees", "fix-d")
	if code := h.run("where"); code != 0 || !strings.Contains(h.out.String(), "opened by  cli (pid ") || !strings.Contains(h.out.String(), "from "+root) {
		t.Errorf("where:\n%s%s", h.out, h.err)
	}
}
