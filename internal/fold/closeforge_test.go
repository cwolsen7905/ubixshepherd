package fold

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// squashForge answers every branch with one request.
type squashForge struct{ mr *forge.MR }

func (squashForge) Name() string { return "fake" }
func (s squashForge) MRForBranch(context.Context, string) (*forge.MR, error) {
	if s.mr == nil {
		return nil, nil
	}
	c := *s.mr
	return &c, nil
}
func (squashForge) FailedJobs(context.Context, int64) ([]forge.Job, error)       { return nil, nil }
func (squashForge) RefPipeline(context.Context, string) (*forge.Pipeline, error) { return nil, nil }
func (squashForge) JobLog(context.Context, int64, int) (string, error)           { return "", nil }
func (squashForge) CreateMR(context.Context, string, string, string, string) (*forge.MR, error) {
	return nil, nil
}

// A squash merge looks unmerged to git; Close takes the forge's word for it, without
// --force, and records the merge on the lane.
func TestCloseAcceptsAForgeMergedRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.open("squashed")
	f.commit(l.Worktree, "src/a.go")
	head := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.fold.ForgeFor = func(string) (forge.Forge, error) {
		return squashForge{&forge.MR{IID: 12, State: "merged", SHA: head, MergeSHA: "deadbeef", URL: "https://gl/mr/12", Created: time.Now().Add(time.Minute)}}, nil
	}
	res, err := f.fold.Close(ctx, l.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.BranchDeleted || res.Lane.State != store.LaneClosed {
		t.Errorf("close result = %+v", res)
	}
	if lf, _ := f.fold.Store.LaneForge(ctx, l.ID); lf.MRState != "merged" || lf.MR != 12 {
		t.Errorf("lane forge = %+v", lf)
	}
}

func TestCloseDoesNotTrustAForgeMergeThatDoesNotCoverTheLane(t *testing.T) {
	for name, mr := range map[string]*forge.MR{
		"older than the lane": {IID: 3, State: "merged", SHA: "x", MergeSHA: "y", Created: time.Now().Add(-48 * time.Hour)},
		"not merged":          {IID: 3, State: "opened", SHA: "x", Created: time.Now().Add(time.Minute)},
		"no request":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			l := f.open("squashed")
			f.commit(l.Worktree, "src/a.go")
			f.fold.ForgeFor = func(string) (forge.Forge, error) { return squashForge{mr}, nil }
			if _, err := f.fold.Close(context.Background(), l.ID, false); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "squash") {
				t.Errorf("close: %v", err)
			}
		})
	}
}

func TestCloseRefusesWhenTheLaneHoldsMoreThanTheMergedRequest(t *testing.T) {
	f := newFixture(t)
	l := f.open("squashed")
	f.commit(l.Worktree, "src/a.go")
	head := gitT(t, l.Worktree, "rev-parse", "HEAD")
	f.commit(l.Worktree, "src/later.go") // after the request's head
	f.fold.ForgeFor = func(string) (forge.Forge, error) {
		return squashForge{&forge.MR{IID: 12, State: "merged", SHA: head, MergeSHA: "deadbeef", Created: time.Now().Add(time.Minute)}}, nil
	}
	if _, err := f.fold.Close(context.Background(), l.ID, false); !errors.Is(err, ErrRefused) {
		t.Errorf("close: %v", err)
	}
}
