package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// approvingForge is a fakeForge that can also be asked about approvals.
type approvingForge struct {
	*fakeForge
	approved bool
	job      *forge.PipelineJob
	err      error
	reads    int
}

func (a *approvingForge) Approvals(context.Context, int) (*forge.Approvals, error) {
	a.reads++
	if a.err != nil {
		return nil, a.err
	}
	return &forge.Approvals{Approved: a.approved}, nil
}
func (a *approvingForge) Approve(context.Context, int, string) error {
	panic("the watcher must never approve")
}
func (a *approvingForge) JobByName(context.Context, int64, string) (*forge.PipelineJob, error) {
	return a.job, nil
}
func (a *approvingForge) RetryJob(context.Context, int64) error {
	panic("the watcher must never retry a job")
}

func TestApprovalIsPolledCachedAndCleared(t *testing.T) {
	f := newFixture(t)
	head := headOf(t, f.lane.Worktree)
	af := &approvingForge{fakeForge: f.f, job: &forge.PipelineJob{ID: 5, Name: forge.RequireApprovalJob, Status: "manual"}}
	f.w.ForgeFor = func(string) (forge.Forge, error) { return af, nil }
	clock := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	f.w.Clock = func() time.Time { return clock }
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 9, Status: "running", SHA: head}}
	stored := func() (bool, string) {
		lf, err := f.st.LaneForge(f.ctx, f.lane.ID)
		if err != nil {
			t.Fatal(err)
		}
		return lf.Approved, lf.RequireApproval
	}

	f.w.Check(f.ctx)
	if ok, job := stored(); ok || job != "manual" || af.reads != 1 {
		t.Fatalf("first poll: approved=%v job=%q reads=%d", ok, job, af.reads)
	}

	// Nothing changed and the read is fresh: no second read, so an approval waits.
	af.approved = true
	clock = clock.Add(time.Minute)
	f.w.Check(f.ctx)
	if ok, _ := stored(); ok || af.reads != 1 {
		t.Fatalf("fresh read was repeated: approved=%v reads=%d", ok, af.reads)
	}

	// Past the refresh the forge is asked again.
	clock = clock.Add(ApprovalRefresh)
	f.w.Check(f.ctx)
	if ok, _ := stored(); !ok || af.reads != 2 {
		t.Fatalf("after refresh: approved=%v reads=%d", ok, af.reads)
	}

	// A new pipeline is read at once; the job is now success.
	af.job = &forge.PipelineJob{ID: 6, Name: forge.RequireApprovalJob, Status: "success"}
	f.f.mr.Pipeline = &forge.Pipeline{ID: 10, Status: "running", SHA: head}
	f.w.Check(f.ctx)
	if ok, job := stored(); !ok || job != "success" || af.reads != 3 {
		t.Fatalf("new pipeline: approved=%v job=%q reads=%d", ok, job, af.reads)
	}

	// A failed read keeps what was stored.
	af.err = errors.New("500")
	f.f.mr.Pipeline = &forge.Pipeline{ID: 11, Status: "running", SHA: head}
	f.w.Check(f.ctx)
	if ok, job := stored(); !ok || job != "success" {
		t.Fatalf("failed read changed the stored value: approved=%v job=%q", ok, job)
	}
	af.err = nil

	// A new request starts clean, and a merged one is cleared.
	af.approved, af.job = false, nil
	f.f.mr = &forge.MR{IID: 8, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 12, Status: "running", SHA: head}}
	f.w.Check(f.ctx)
	if ok, job := stored(); ok || job != "" {
		t.Fatalf("new request: approved=%v job=%q", ok, job)
	}
	af.approved = true
	clock = clock.Add(time.Hour)
	f.w.Check(f.ctx)
	if ok, _ := stored(); !ok {
		t.Fatal("approval not read")
	}
	f.f.mr.State, f.f.mr.MergeSHA = "closed", ""
	f.w.Check(f.ctx)
	if ok, job := stored(); ok || job != "" {
		t.Fatalf("closed request kept approval: approved=%v job=%q", ok, job)
	}
}

func TestApprovalNeedsAnApproverForge(t *testing.T) {
	f := newFixture(t) // the plain fakeForge is not an Approver
	head := headOf(t, f.lane.Worktree)
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 9, Status: "success", SHA: head}}
	f.w.Check(f.ctx)
	lf, _ := f.st.LaneForge(f.ctx, f.lane.ID)
	if lf.Approved || lf.RequireApproval != "" || lf.MR != 7 {
		t.Fatalf("stored = %+v", lf)
	}
}
