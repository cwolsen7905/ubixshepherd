package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if remote != "" {
		run("remote", "add", "origin", remote)
	}
}

func TestScan(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitInit(t, filepath.Join(root, "app"), "git@example.com:team/app.git")
	os.WriteFile(filepath.Join(root, "app", "go.mod"), []byte("module app\n"), 0o644)
	os.WriteFile(filepath.Join(root, "app", "Makefile"), nil, 0o644)
	gitInit(t, filepath.Join(root, "app-copy"), "https://example.com/team/app")
	gitInit(t, filepath.Join(root, "scratch"), "")
	gitInit(t, filepath.Join(root, "group", "lib"), "https://example.com/group/lib.git")
	// Not repos, or not to be scanned.
	os.MkdirAll(filepath.Join(root, "plain", "dir"), 0o755)
	gitInit(t, filepath.Join(root, "app-worktrees", "lane"), "x")
	gitInit(t, filepath.Join(root, "node_modules", "dep"), "x")
	gitInit(t, filepath.Join(root, "a", "b", "c", "too-deep"), "x")
	// A linked worktree has a .git file.
	os.MkdirAll(filepath.Join(root, "linked"), 0o755)
	os.WriteFile(filepath.Join(root, "linked", ".git"), []byte("gitdir: elsewhere\n"), 0o644)
	// A repo inside a repo is not descended into.
	gitInit(t, filepath.Join(root, "app", "nested"), "x")

	found, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Found{}
	for _, f := range found {
		byName[f.Name] = f
	}
	want := []string{"app", "app-copy", "group/lib", "scratch"}
	if len(found) != len(want) {
		t.Fatalf("found %d repos: %+v", len(found), found)
	}
	for _, n := range want {
		if _, ok := byName[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	if f := byName["app"]; !f.Suggest || len(f.Stacks) != 2 || f.Stacks[0] != "go" {
		t.Errorf("app = %+v", f)
	}
	if f := byName["app-copy"]; f.Suggest || f.Reason != "same remote as app" {
		t.Errorf("app-copy = %+v", f)
	}
	if f := byName["scratch"]; f.Suggest || f.Reason != "no origin remote" {
		t.Errorf("scratch = %+v", f)
	}
	if !byName["group/lib"].Suggest {
		t.Error("group/lib should be suggested")
	}
}

func TestNormalizeRemote(t *testing.T) {
	same := []string{
		"git@example.com:team/app.git",
		"https://example.com/team/app",
		"https://user:tok@example.com/team/app.git/",
		"ssh://git@example.com:2222/team/app.git",
		"HTTPS://Example.com/Team/App.git",
	}
	want := "example.com/team/app"
	for _, u := range same {
		if got := NormalizeRemote(u); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", u, got, want)
		}
	}
	if NormalizeRemote("") != "" {
		t.Error("empty remote should normalise to empty")
	}
}
