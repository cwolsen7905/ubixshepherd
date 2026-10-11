package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/fold"
)

// shepherd lane diff and the lane_diff tool read a lane's worktree through the daemon:
// stat by default, --files, --file, --json; scope marks and redaction come with it.
func TestLaneDiffCLIAndMCP(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	if code := h.run("lane", "open", "feat/d", "--repo", "app", "--scope", "src/**"); code != 0 {
		t.Fatalf("open: %s", h.err)
	}
	wt := filepath.Join(root, "app-worktrees", "feat-d")
	os.MkdirAll(filepath.Join(wt, "src"), 0o755)
	os.WriteFile(filepath.Join(wt, "src", "in.go"), []byte("package in\nvar api_key = \"abcd1234efgh\"\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "stray.txt"), []byte("invented\n"), 0o644)

	if code := h.run("lane", "diff", "feat/d", "--repo", "app"); code != 0 {
		t.Fatalf("lane diff: %d %s", code, h.err)
	}
	out := h.out.String()
	for _, want := range []string{"lane feat/d (app)", "untracked", "src/in.go", "stray.txt", "OUT", "1 OUTSIDE"} {
		if !strings.Contains(out, want) {
			t.Errorf("stat lacks %q:\n%s", want, out)
		}
	}

	h.out.Reset()
	if code := h.run("lane", "diff", "feat/d", "--repo", "app", "--json"); code != 0 {
		t.Fatalf("--json: %s", h.err)
	}
	var d fold.LaneDiff
	if err := json.Unmarshal(h.out.Bytes(), &d); err != nil || len(d.Untracked) != 2 || d.Lane != "feat/d" {
		t.Errorf("json: %v %+v", err, d)
	}

	h.out.Reset()
	if code := h.run("lane", "diff", "feat/d", "--repo", "app", "--file", "src/in.go"); code != 0 {
		t.Fatalf("--file: %s", h.err)
	}
	if o := h.out.String(); !strings.Contains(o, "package in") || strings.Contains(o, "abcd1234efgh") {
		t.Errorf("--file:\n%s", o)
	}
	if code := h.run("lane", "diff", "feat/d", "--repo", "app", "--files", "--file", "x"); code != 2 {
		t.Errorf("--files with --file exited %d, want usage (2)", code)
	}

	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lane_diff","arguments":{"repo":"app","name":"feat/d"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lane_diff","arguments":{"repo":"app","name":"feat/d","file":"stray.txt"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lane_diff","arguments":{"repo":"app","name":"feat/d","file":"a","files":true}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "stray.txt") || !strings.Contains(text, "OUT") {
		t.Errorf("lane_diff: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "invented") {
		t.Errorf("lane_diff file: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[2]); !isErr {
		t.Error("files and file together accepted")
	}
}
