package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoWorkspace is laneWorkspace (app has a real origin) plus two more repos on disk.
func repoWorkspace(t *testing.T) (root, app string) {
	t.Helper()
	root, app = laneWorkspace(t)
	for _, n := range []string{"kitg", "lib"} {
		dir := filepath.Join(root, n)
		os.MkdirAll(dir, 0o755)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		exec.Command("git", "-C", dir, "remote", "add", "origin", "https://example.com/t/"+n+".git").Run()
	}
	return root, app
}

func TestRepoAddListRemove(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := repoWorkspace(t)
	if code := h.run("init", root, "--only", "app"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	if !strings.Contains(h.out.String(), "To add another repo later: shepherd repo add <name>") {
		t.Errorf("init does not say how to add one later:\n%s", h.out)
	}

	// The reported case: a repo on disk, added by name, with no daemon restart.
	h.env.Cwd = root
	if code := h.run("repo", "add", "kitg"); code != 0 || !strings.Contains(h.out.String(), "kitg: added to workspace") ||
		!strings.Contains(h.out.String(), "defaults apply") {
		t.Fatalf("add by name: %d\n%s%s", code, h.out, h.err)
	}
	if code := h.run("repo", "add", "kitg"); code != 0 || !strings.Contains(h.out.String(), "already managed") {
		t.Errorf("add again: %d\n%s%s", code, h.out, h.err)
	}
	if code := h.run("repo", "add", "./lib"); code != 0 || !strings.Contains(h.out.String(), "lib: added") {
		t.Errorf("add by path: %d\n%s%s", code, h.out, h.err)
	}
	if code := h.run("repo", "add", "app", "missing"); code == 0 || !strings.Contains(h.err.String(), `no git repo "missing"`) {
		t.Errorf("one bad name: %d %s", code, h.err)
	}
	if code := h.run("repo", "add"); code != 2 {
		t.Errorf("add with nothing: %d", code)
	}

	if code := h.run("repo", "list"); code != 0 {
		t.Fatalf("list: %s", h.err)
	}
	for _, want := range []string{"app", "kitg", "lib", "managed"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, h.out)
		}
	}
	if code := h.run("repo", "list", "--json"); code != 0 || !strings.Contains(h.out.String(), `"state": "managed"`) {
		t.Errorf("list --json: %d\n%s", code, h.out)
	}

	if code := h.run("repo", "remove", "kitg", "--why", "retired"); code != 0 || !strings.Contains(h.out.String(), "no longer managed") {
		t.Fatalf("remove: %d\n%s%s", code, h.out, h.err)
	}
	if code := h.run("repo", "remove", "kitg"); code == 0 || !strings.Contains(h.err.String(), "no managed repo") {
		t.Errorf("remove twice: %d %s", code, h.err)
	}
	if code := h.run("repo", "scan"); code != 0 || !strings.Contains(h.out.String(), "kitg") || !strings.Contains(h.out.String(), "(removed)") {
		t.Errorf("scan after remove: %d\n%s%s", code, h.out, h.err)
	}
	if code := h.run("repo", "add", "kitg"); code != 0 || !strings.Contains(h.out.String(), "managed again") {
		t.Errorf("restore: %d\n%s%s", code, h.out, h.err)
	}
}

func TestRepoRemoveRefusesOpenLane(t *testing.T) {
	h := newHarness(t, "", false)
	root, app := repoWorkspace(t)
	if code := h.run("init", root, "--only", "app"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	if code := h.run("lane", "open", "feat/a", "--scope", "src/**"); code != 0 {
		t.Fatalf("lane open: %s", h.err)
	}
	if code := h.run("repo", "remove", "app"); code == 0 || !strings.Contains(h.err.String(), "1 open lane: feat/a") {
		t.Errorf("remove with an open lane: %d %s", code, h.err)
	}
	if code := h.run("repo", "list"); !strings.Contains(h.out.String(), "app") || code != 0 {
		t.Errorf("app is no longer listed:\n%s", h.out)
	}
}

func TestInitOnlyNotesUnlistedAndPrune(t *testing.T) {
	h := newHarness(t, "", false)
	root, app := repoWorkspace(t)
	if code := h.run("init", root, "--only", "app,kitg"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	if code := h.run("lane", "open", "feat/a", "--scope", "src/**"); code != 0 {
		t.Fatalf("lane open: %s", h.err)
	}

	// --only adds; it says what it left managed, and removes nothing.
	if code := h.run("init", root, "--only", "lib"); code != 0 {
		t.Fatalf("init --only lib: %s", h.err)
	}
	out := h.out.String()
	if !strings.Contains(out, "Also still managed, not listed: app, kitg") || !strings.Contains(out, "shepherd repo remove <name>") {
		t.Errorf("init --only output:\n%s", out)
	}
	if h.run("repo", "list"); !strings.Contains(h.out.String(), "kitg") {
		t.Errorf("--only dropped a repo:\n%s", h.out)
	}

	// --prune refuses while a lane is open, and changes nothing.
	if code := h.run("init", root, "--only", "lib", "--prune"); code == 0 || !strings.Contains(h.err.String(), "app has 1 open lane: feat/a") {
		t.Errorf("prune with an open lane: %d %s", code, h.err)
	}
	h.run("repo", "list")
	if !strings.Contains(h.out.String(), "kitg") || !strings.Contains(h.out.String(), "app") {
		t.Errorf("a refused prune changed the repos:\n%s", h.out)
	}

	// Closed, it unmanages the unlisted ones.
	if code := h.run("lane", "close", "feat/a", "--force"); code != 0 {
		t.Fatalf("lane close: %s%s", h.out, h.err)
	}
	if code := h.run("init", root, "--only", "lib", "--prune"); code != 0 || !strings.Contains(h.out.String(), "Stopped managing app") {
		t.Fatalf("prune: %d\n%s%s", code, h.out, h.err)
	}
	// What was pruned is found, not managed, and marked removed.
	h.run("repo", "scan")
	for _, n := range []string{"kitg", "app"} {
		if !strings.Contains(h.out.String(), n) || !strings.Contains(h.out.String(), "(removed)") {
			t.Errorf("scan after prune lacks %s:\n%s", n, h.out)
		}
	}
}

func TestInitInteractiveUnticksAfterConfirmation(t *testing.T) {
	root, _ := repoWorkspace(t)
	h := newHarness(t, "", false)
	if code := h.run("init", root, "--only", "app,kitg"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	// Untick kitg (it is the second managed row), accept, and confirm.
	h.env.Interactive = true
	h.env.Stdin = strings.NewReader("2\n\ny\n")
	// Rows: app, kitg, lib; all ticked.
	if code := h.run("init", root); code != 0 || !strings.Contains(h.out.String(), "This stops managing 1 repo: kitg") ||
		!strings.Contains(h.out.String(), "Stopped managing kitg") {
		t.Fatalf("interactive untick: %d\n%s%s", code, h.out, h.err)
	}
}
