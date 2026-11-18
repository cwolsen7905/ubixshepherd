package project

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
)

func TestMain(m *testing.M) {
	git.ClearEnvConfig()
	os.Exit(m.Run())
}

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// docsRepo is a clone of a bare origin whose default branch is main.
func docsRepo(t *testing.T) (clone string) {
	t.Helper()
	root := t.TempDir()
	bare, clone := filepath.Join(root, "o.git"), filepath.Join(root, "c")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	sh(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	if out, err := exec.Command("git", "clone", bare, clone).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	sh(t, clone, "checkout", "-b", "main")
	return clone
}

func commitBrief(t *testing.T, clone, text string) {
	t.Helper()
	f := filepath.Join(clone, "projects", "p", "brief.md")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte(text), 0o644)
	sh(t, clone, "add", ".")
	sh(t, clone, "commit", "-m", "brief")
	sh(t, clone, "push", "-u", "origin", "main")
}

var src = config.BriefSource{Repo: "docs"}

func TestReadApprovedTip(t *testing.T) {
	clone := docsRepo(t)
	commitBrief(t, clone, "Ship the thing.\n")
	r := &Reader{}
	b, err := r.ForProject(context.Background(), "p", src, clone)
	if err != nil || b.Text != "Ship the thing." || b.Commit == "" || time.Since(b.Approved) > time.Minute {
		t.Fatalf("got %+v, %v", b, err)
	}
}

func TestMissing(t *testing.T) {
	ctx := context.Background()
	clone := docsRepo(t)
	r := &Reader{}
	// No commits at all, then a repo without the file, then no repo.
	if _, err := r.ForProject(ctx, "p", src, clone); !errors.Is(err, ErrMissing) {
		t.Errorf("empty repo: %v", err)
	}
	commitBrief(t, clone, "x")
	if _, err := r.ForProject(ctx, "other", src, clone); !errors.Is(err, ErrMissing) {
		t.Errorf("no file: %v", err)
	}
	if _, err := r.ForProject(ctx, "p", src, filepath.Join(t.TempDir(), "none")); !errors.Is(err, ErrMissing) {
		t.Errorf("no repo: %v", err)
	}
	if _, err := r.Read(ctx, clone, "nope", "projects/p/brief.md"); !errors.Is(err, ErrMissing) {
		t.Errorf("no ref: %v", err)
	}
}

func TestUnmergedEditIsNotApproved(t *testing.T) {
	ctx := context.Background()
	clone := docsRepo(t)
	commitBrief(t, clone, "approved")
	// An edit on a side branch, and an uncommitted one on main, are both ignored.
	sh(t, clone, "checkout", "-b", "edit")
	commitBrief(t, clone, "proposed")
	sh(t, clone, "checkout", "main")
	os.WriteFile(filepath.Join(clone, "projects", "p", "brief.md"), []byte("scribble"), 0o644)
	b, err := (&Reader{}).ForProject(ctx, "p", src, clone)
	if err != nil || b.Text != "approved" {
		t.Fatalf("got %+v, %v", b, err)
	}
}

func TestCacheRefresh(t *testing.T) {
	ctx := context.Background()
	clone := docsRepo(t)
	commitBrief(t, clone, "one")
	now := time.Now()
	r := &Reader{TTL: time.Minute, Now: func() time.Time { return now }}
	get := func() string {
		b, err := r.ForProject(ctx, "p", src, clone)
		if err != nil {
			t.Fatal(err)
		}
		return b.Text
	}
	if get() != "one" {
		t.Fatal("first read")
	}
	commitBrief(t, clone, "two")
	if get() != "one" {
		t.Error("within the TTL the cached read is reused")
	}
	now = now.Add(2 * time.Minute)
	if get() != "two" {
		t.Error("after the TTL the file is read again")
	}
}
