package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// TestCloseWorktreeStates closes a merged lane whose worktree folder is a normal
// checkout, has been deleted, or exists without a checkout in it. The branch and the
// lane close in every case; only the folder's fate and the result's notes differ.
func TestCloseWorktreeStates(t *testing.T) {
	cases := map[string]struct {
		prepare  func(t *testing.T, f *fixture, l store.Lane)
		wantLeft bool
		wantNote string
	}{
		"normal worktree": {
			prepare: func(t *testing.T, f *fixture, l store.Lane) {},
		},
		"missing folder": {
			prepare: func(t *testing.T, f *fixture, l store.Lane) {
				if err := os.RemoveAll(l.Worktree); err != nil {
					t.Fatal(err)
				}
			},
			wantNote: "already gone",
		},
		"folder not a checkout": {
			prepare: func(t *testing.T, f *fixture, l store.Lane) {
				if err := os.RemoveAll(l.Worktree); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(l.Worktree, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(l.Worktree, "stray.txt"), []byte("stray\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantLeft: true,
			wantNote: "not a git checkout",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			l := f.open("work")
			f.commit(l.Worktree, "src/a.go")
			// Merged: the branch is in origin/main, so no force is needed.
			gitT(t, l.Worktree, "push", "-q", "origin", "HEAD:main")
			tc.prepare(t, f, l)

			res, err := f.fold.Close(ctx, l.ID, false)
			if err != nil {
				t.Fatalf("close: %v", err)
			}
			if res.Lane.State != store.LaneClosed {
				t.Errorf("lane state = %q, want closed", res.Lane.State)
			}
			if !res.BranchDeleted {
				t.Errorf("branch not deleted: %+v", res)
			}
			if _, err := os.Stat(l.Worktree); (err == nil) != tc.wantLeft {
				t.Errorf("worktree left in place = %v, want %v", err == nil, tc.wantLeft)
			}
			if tc.wantNote != "" {
				found := false
				for _, n := range res.Notes {
					if strings.Contains(n, tc.wantNote) {
						found = true
					}
				}
				if !found {
					t.Errorf("notes = %q, want one containing %q", res.Notes, tc.wantNote)
				}
			}
			// Git's record of the worktree is gone in every case.
			if list := gitT(t, f.repo.Path, "worktree", "list", "--porcelain"); strings.Contains(list, l.Worktree) {
				t.Errorf("git still lists the worktree:\n%s", list)
			}
			if tc.wantLeft {
				if _, err := os.ReadFile(filepath.Join(l.Worktree, "stray.txt")); err != nil {
					t.Errorf("folder was not left in place: %v", err)
				}
			}
		})
	}
}
