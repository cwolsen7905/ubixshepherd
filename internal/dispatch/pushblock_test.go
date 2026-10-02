package dispatch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The block stops every push from the lane's repo to a URL it knows, and nothing else:
// a test suite's own repo, whose remote is also called origin, pushes as usual.
func TestPushBlockIsScopedToTheLaneRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	block := []string(nil)
	g := func(dir string, args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e", "GIT_TERMINAL_PROMPT=0")
		cmd.Env = append(cmd.Env, block...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(dir string, args ...string) {
		t.Helper()
		if out, err := g(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	repo := func(name string) string {
		dir := filepath.Join(base, name)
		must(base, "init", "-q", "-b", "main", dir)
		os.WriteFile(filepath.Join(dir, "f"), []byte(name), 0o644)
		must(dir, "add", ".")
		must(dir, "commit", "-q", "-m", "init")
		return dir
	}
	bare := func(name string) string {
		dir := filepath.Join(base, name+".git")
		must(base, "init", "-q", "--bare", "-b", "main", dir)
		return dir
	}
	lane, origin, mirror, mirrorPush := repo("lane"), bare("origin"), bare("mirror"), bare("mirror-push")
	must(lane, "remote", "add", "origin", origin)
	must(lane, "remote", "add", "mirror", mirror)
	must(lane, "config", "remote.mirror.pushurl", mirrorPush)
	must(lane, "push", "-q", "origin", "main") // before the run: allowed
	other, otherOrigin := repo("other"), bare("other-origin")
	must(other, "remote", "add", "origin", otherOrigin)

	block = pushBlock(context.Background(), lane)
	if !strings.Contains(strings.Join(block, " "), noPush) {
		t.Fatalf("block = %v", block)
	}
	must(lane, "commit", "-q", "--allow-empty", "-m", "agent work")
	for _, args := range [][]string{
		{"push", "-q", "--no-verify", "origin", "HEAD:main"},
		{"push", "-q", "origin", "HEAD:refs/heads/side"},
		{"push", "-q", origin, "HEAD:main"},
		{"push", "-q", "mirror", "HEAD:main"},
		{"push", "-q", mirrorPush, "HEAD:main"},
	} {
		if out, err := g(lane, args...); err == nil {
			t.Errorf("git %v pushed during the run: %s", args, out)
		}
	}
	for b, want := range map[string]string{origin: "1", mirror: "0", mirrorPush: "0"} {
		if out, _ := g(b, "rev-list", "--count", "--all"); strings.TrimSpace(out) != want {
			t.Errorf("%s has %s commits, want %s: a push got through", filepath.Base(b), strings.TrimSpace(out), want)
		}
	}
	// Fetching the lane's origin still works, and so does pushing another repo.
	must(lane, "fetch", "-q", "origin")
	must(other, "push", "-q", "origin", "main")
}
