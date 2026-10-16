package watch

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A squash merge looks unmerged to git; Fold.Close takes the forge's word for it, with
// no --force, and records the merge on the lane so its item settles as done.
func TestCloseAcceptsAForgeMergedRequest(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.w.Fold.ForgeFor = func(string) (forge.Forge, error) { return f.f, nil }
	head := f.commitAndHead(t) // a commit of its own: git cannot see it merged
	f.f.mr = &forge.MR{IID: 12, State: "merged", SHA: head, MergeSHA: "deadbeef", URL: "https://gl/mr/12", Created: time.Now().Add(time.Minute)}
	res, err := f.w.Fold.Close(f.ctx, f.lane.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Lane.State != store.LaneClosed {
		t.Errorf("close result = %+v", res)
	}
	if lf, _ := f.st.LaneForge(f.ctx, f.lane.ID); lf.MRState != "merged" || lf.MR != 12 {
		t.Errorf("lane forge = %+v", lf)
	}
	// The recorded merge is enough for the item to settle.
	lane, _ := f.st.Lane(f.ctx, f.lane.ID)
	lf, _ := f.st.LaneForge(f.ctx, f.lane.ID)
	if !store.Settle(it, &lane, lf).Merged {
		t.Error("the item does not settle as done")
	}
}

// commitAndHead adds a commit to the lane's branch and returns its tip.
func (f *fixture) commitAndHead(t *testing.T) string {
	t.Helper()
	g := func(args ...string) {
		if out, err := gitOut(f.lane.Worktree, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("-c", "user.name=t", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "work")
	return f.head(t)
}

func TestCloseDoesNotTrustAForgeMergeThatDoesNotCoverTheLane(t *testing.T) {
	for name, mr := range map[string]func(head string) *forge.MR{
		"older than the lane": func(h string) *forge.MR {
			return &forge.MR{IID: 3, State: "merged", SHA: h, MergeSHA: "y", Created: time.Now().Add(-48 * time.Hour)}
		},
		"not merged": func(h string) *forge.MR {
			return &forge.MR{IID: 3, State: "opened", SHA: h, Created: time.Now().Add(time.Minute)}
		},
		"no request": func(string) *forge.MR { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			head := f.commitAndHead(t)
			f.w.Fold.ForgeFor = func(string) (forge.Forge, error) { return &fakeForge{mr: mr(head)}, nil }
			if _, err := f.w.Fold.Close(f.ctx, f.lane.ID, false); !errors.Is(err, fold.ErrRefused) || !strings.Contains(err.Error(), "squash") {
				t.Errorf("close: %v", err)
			}
		})
	}
}

func TestCloseRefusesWhenTheLaneHoldsMoreThanTheMergedRequest(t *testing.T) {
	f := newFixture(t)
	head := f.commitAndHead(t)
	f.commitAndHead(t) // after the request's head
	f.w.Fold.ForgeFor = func(string) (forge.Forge, error) {
		return &fakeForge{mr: &forge.MR{IID: 12, State: "merged", SHA: head, MergeSHA: "deadbeef", Created: time.Now().Add(time.Minute)}}, nil
	}
	if _, err := f.w.Fold.Close(f.ctx, f.lane.ID, false); !errors.Is(err, fold.ErrRefused) {
		t.Errorf("close: %v", err)
	}
}

func gitOut(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
}
