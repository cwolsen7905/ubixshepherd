package fold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

func TestOverlappingScopeRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "a", Scope: []string{"src/**", "README.md"}}); err != nil {
		t.Fatal(err)
	}
	_, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "b", Scope: []string{"README.md"}})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "lane a") || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("overlap: %v", err)
	}
	// New paths: a glob below another lane's glob, before any file exists there.
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "c", Scope: []string{"src/auth/**"}})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("nested new path: %v", err)
	}
	// Disjoint is fine.
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "d", Scope: []string{"docs/**"}}); err != nil {
		t.Fatalf("disjoint: %v", err)
	}
}

func TestSharedPathsReported(t *testing.T) {
	f := newFixture(t)
	f.fold.Config, _ = config.Parse([]byte("repos:\n  app:\n    shared_paths: [README.md, .gitlab-ci.yml]\n"))
	o, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: "s", Scope: []string{"README.md", "src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Shared) != 1 || o.Shared[0] != "README.md" {
		t.Errorf("shared = %v", o.Shared)
	}
}

func pushRef(t *testing.T, dir, branch string) PushRef {
	t.Helper()
	sha := gitT(t, dir, "rev-parse", "HEAD")
	return PushRef{LocalRef: "refs/heads/" + branch, LocalSHA: sha, RemoteRef: "refs/heads/" + branch, RemoteSHA: strings.Repeat("0", 40)}
}

func TestCheckPush(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("work") // scope src/**
	f.commit(l.Worktree, "src/ok.go")

	v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{pushRef(t, l.Worktree, "work")})
	if err != nil || !v.OK {
		t.Fatalf("in-scope push: %+v %v", v, err)
	}

	f.commit(l.Worktree, "docs/stray.md")
	v, _ = f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{pushRef(t, l.Worktree, "work")})
	if v.OK || len(v.Problems) != 1 || !strings.Contains(v.Problems[0], "docs/stray.md") || strings.Contains(v.Problems[0], "src/ok.go") {
		t.Errorf("out-of-scope push: %+v", v)
	}

	other := pushRef(t, l.Worktree, "work")
	other.RemoteRef = "refs/heads/main"
	v, _ = f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{other})
	if v.OK || !strings.Contains(v.Problems[0], "pushes only its branch") {
		t.Errorf("push to main: %+v", v)
	}

	tag := pushRef(t, l.Worktree, "work")
	tag.RemoteRef = "refs/tags/v1.0.0"
	v, _ = f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{tag})
	if !v.OK || len(v.Problems) != 0 {
		t.Errorf("tag push in a repo whose tags are free: %+v", v)
	}

	// Not a lane: Shepherd stays out of the way.
	v, _ = f.fold.CheckPush(ctx, nil, nil, f.repo.Path, nil)
	if !v.OK || v.Lane != "" {
		t.Errorf("non-lane push: %+v", v)
	}
}

func TestCheckPushCountsOnlyNewCommits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("inc")
	f.commit(l.Worktree, "src/a.go")
	gitT(t, l.Worktree, "push", "-q", "origin", "HEAD:inc")
	remote := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/b.go")
	r := pushRef(t, l.Worktree, "inc")
	r.RemoteSHA = remote
	v, err := f.fold.CheckPush(ctx, &l, &f.repo, l.Worktree, []PushRef{r})
	if err != nil || !v.OK {
		t.Errorf("incremental push: %+v %v", v, err)
	}
}

func TestParsePushRefs(t *testing.T) {
	refs, err := ParsePushRefs("refs/heads/a 1111 refs/heads/a 0000\n\nrefs/heads/b 2222 refs/heads/b 3333\n")
	if err != nil || len(refs) != 2 || refs[1].RemoteSHA != "3333" {
		t.Errorf("refs = %+v, %v", refs, err)
	}
	if _, err := ParsePushRefs("bad line"); err == nil {
		t.Error("bad input accepted")
	}
}

func TestHookInstallStates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repo := f.repo.Path

	st, err := InstallHook(ctx, repo, "/usr/local/bin/shepherd")
	if err != nil || !st.Ours || st.Tracked {
		t.Fatalf("install: %+v %v", st, err)
	}
	if b, _ := os.ReadFile(st.Path); !strings.Contains(string(b), "hook pre-push") {
		t.Errorf("hook body:\n%s", b)
	}
	if st, _ = UninstallHook(ctx, repo); st.Ours {
		t.Error("still installed after uninstall")
	}

	// Someone else's hook is never replaced.
	os.WriteFile(st.Path, []byte("#!/bin/sh\necho mine\n"), 0o755)
	if _, err := InstallHook(ctx, repo, "x"); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), HookLine) {
		t.Errorf("foreign hook: %v", err)
	}
	if st, _ = UninstallHook(ctx, repo); !st.Foreign {
		t.Error("uninstall touched a foreign hook")
	}
	os.Remove(st.Path)

	// core.hooksPath into the work tree: reported as tracked, and lane open does not write there.
	gitT(t, repo, "config", "core.hooksPath", ".githooks")
	f.fold.Exe = "/usr/local/bin/shepherd"
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "h", Scope: []string{"x/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "pre-push")); err == nil {
		t.Error("lane open wrote into a tracked hooks directory")
	}
	if len(o.Notes) != 1 || !strings.Contains(o.Notes[0], "outside .git") {
		t.Errorf("notes = %v", o.Notes)
	}

	// Default hooks dir: lane open installs it.
	gitT(t, repo, "config", "--unset", "core.hooksPath")
	o, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "h2", Scope: []string{"y/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Notes) != 1 || !strings.Contains(o.Notes[0], "installed the pre-push hook") {
		t.Errorf("notes = %v", o.Notes)
	}
}

func TestLaneSetup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(f.repo.Path, ".env"), []byte("SECRET=1\n"), 0o600)
	f.fold.Config, _ = config.Parse([]byte("repos:\n  app:\n    setup: cp \"$SHEPHERD_REPO/.env\" .env\n"))
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "with-setup", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(o.Worktree, ".env")); err != nil || string(b) != "SECRET=1\n" {
		t.Errorf(".env not set up: %q %v", b, err)
	}
	if len(o.Notes) == 0 || !strings.Contains(o.Notes[0], "setup done") {
		t.Errorf("notes = %v", o.Notes)
	}
	f.fold.Config, _ = config.Parse([]byte("repos:\n  app:\n    setup: echo nope; exit 3\n"))
	o, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "bad-setup", Scope: []string{"docs/**"}})
	if err != nil || o.State != "open" || !strings.Contains(o.Notes[0], "setup failed") || !strings.Contains(o.Notes[0], "nope") {
		t.Errorf("failed setup: %+v %v", o, err)
	}
}
