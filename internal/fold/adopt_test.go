package fold

import (
	"context"
	"errors"
	"os"
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
