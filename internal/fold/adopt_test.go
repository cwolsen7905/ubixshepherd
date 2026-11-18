package fold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// pushBranch makes a branch on origin that no lane opened, as an MR author would.
func (f *fixture) pushBranch(branch string) {
	f.t.Helper()
	other := filepath.Join(filepath.Dir(f.ws), "author-"+strings.ReplaceAll(branch, "/", "-"))
	gitT(f.t, filepath.Dir(f.ws), "clone", "-q", f.origin, other)
	gitT(f.t, other, "checkout", "-q", "-b", branch)
	f.commit(other, "src/theirs.go")
	gitT(f.t, other, "push", "-q", "origin", branch)
}

func TestOpenAdoptChecksOutOriginBranch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.pushBranch("fix/review")
	res, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "fix/review", Scope: []string{"src/**"}, Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	l := res.Lane
	if _, err := os.Stat(filepath.Join(l.Worktree, "src/theirs.go")); err != nil {
		t.Error("worktree does not hold the adopted branch's work")
	}
	if gitT(t, l.Worktree, "rev-parse", "HEAD") != gitT(t, f.repo.Path, "rev-parse", "origin/fix/review") {
		t.Error("lane is not at origin/fix/review")
	}
	if gitT(t, l.Worktree, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/fix/review" {
		t.Error("adopted branch does not track origin")
	}
	// Without adopt the same open is refused, and says how.
	f.fold.Close(ctx, l.ID, true)
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "fix/review", Scope: []string{"src/**"}})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "--adopt") {
		t.Errorf("open without adopt: %v", err)
	}
}

func TestOpenAdoptRefuses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.pushBranch("fix/held")
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "first", Branch: "fix/held", Scope: []string{"src/a/**"}, Adopt: true}); err != nil {
		t.Fatal(err)
	}
	// Another open lane holds the branch: refused even with a disjoint scope.
	_, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "second", Branch: "fix/held", Scope: []string{"docs/**"}, Adopt: true})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "already holds") {
		t.Errorf("held branch: %v", err)
	}
	// No such branch on origin.
	_, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "ghost", Scope: []string{"x"}, Adopt: true})
	if !errors.Is(err, ErrRefused) {
		t.Errorf("missing branch: %v", err)
	}
	lanes, _ := f.fold.Store.Lanes(ctx, f.repo.ID)
	if len(lanes) != 1 {
		t.Errorf("lanes = %d, want 1", len(lanes))
	}
}

func TestCloseKeepsAnAdoptedBranch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.pushBranch("fix/mine-not")
	res, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "fix/mine-not", Scope: []string{"src/**"}, Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	l := res.Lane
	// Merged into main, so a created branch would be deleted on close.
	gitT(t, l.Worktree, "push", "-q", "origin", "HEAD:main")
	out, err := f.fold.Close(ctx, l.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.BranchDeleted || out.Lane.State != store.LaneClosed {
		t.Errorf("close = %+v", out)
	}
	if !strings.Contains(gitT(t, f.repo.Path, "branch", "--list", "fix/mine-not"), "fix/mine-not") {
		t.Error("adopted branch was deleted")
	}
	if _, err := os.Stat(l.Worktree); !os.IsNotExist(err) {
		t.Error("worktree left behind")
	}
}

func adoptedMarker(t *testing.T, repo, branch string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", repo, "config", "--get", adoptedKey(branch)).Output()
	return strings.TrimSpace(string(out))
}

func TestOpenClearsAStaleAdoptionMarker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	gitT(t, f.repo.Path, "config", adoptedKey("fresh"), "true")
	res, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "fresh", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := adoptedMarker(t, f.repo.Path, "fresh"); got != "" {
		t.Errorf("stale marker survived a normal open: %q", got)
	}
	// So closing it once merged deletes the branch it created.
	gitT(t, res.Lane.Worktree, "push", "-q", "origin", "HEAD:main")
	out, err := f.fold.Close(ctx, res.Lane.ID, false)
	if err != nil || !out.BranchDeleted {
		t.Errorf("close = %+v, %v", out, err)
	}
}

func TestFailedAdoptOpenLeavesNoMarker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.pushBranch("fix/blocked")
	// A local copy equal to origin's, checked out in the main checkout, so the
	// worktree cannot be added.
	gitT(t, f.repo.Path, "fetch", "-q", "origin")
	gitT(t, f.repo.Path, "checkout", "-q", "-b", "fix/blocked", "origin/fix/blocked")
	if _, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "fix/blocked", Scope: []string{"src/**"}, Adopt: true}); err == nil {
		t.Fatal("open succeeded, want a failure")
	}
	if got := adoptedMarker(t, f.repo.Path, "fix/blocked"); got != "" {
		t.Errorf("marker left after a failed adopt open: %q", got)
	}
	if lanes, _ := f.fold.Store.Lanes(ctx, f.repo.ID); len(lanes) != 0 {
		t.Errorf("lanes = %d, want 0", len(lanes))
	}
}
