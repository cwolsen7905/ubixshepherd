package watch

import (
	"context"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// reviewForge is a forge whose AI review job is whatever the test sets, and which counts
// retries. It never approves.
type reviewForge struct {
	*fakeForge
	job     *forge.PipelineJob
	retries int
}

func (r *reviewForge) Approvals(context.Context, int) (*forge.Approvals, error) {
	return &forge.Approvals{}, nil
}
func (r *reviewForge) Approve(context.Context, int, string) error {
	panic("the watcher must never approve")
}
func (r *reviewForge) JobByName(_ context.Context, _ int64, name string) (*forge.PipelineJob, error) {
	if name == forge.AIReviewJob {
		return r.job, nil
	}
	return nil, nil
}
func (r *reviewForge) RetryJob(context.Context, int64) error { r.retries++; return nil }

func TestUnreviewedAIReviewIsRetriedOnceThenLeftToThePerson(t *testing.T) {
	f := newFixture(t)
	rf := &reviewForge{fakeForge: f.f, job: &forge.PipelineJob{ID: 5, Name: forge.AIReviewJob, Status: "failed", URL: "https://gl/j/5"}}
	f.w.ForgeFor = func(string) (forge.Forge, error) { return rf, nil }
	f.f.log = "ERROR: Job failed: exit code 75"
	f.f.mr = &forge.MR{IID: 7, State: "opened", URL: "https://gl/mr/7", Pipeline: &forge.Pipeline{ID: 100, Status: "success"}}

	f.w.Check(f.ctx)
	if rf.retries != 1 {
		t.Fatalf("retries = %d, want 1", rf.retries)
	}
	// The retried job is running: nothing more.
	rf.job = &forge.PipelineJob{ID: 6, Name: forge.AIReviewJob, Status: "running"}
	f.w.Check(f.ctx)
	// It is unreviewed again (the summary says so): no second retry, and the person is told.
	rf.job = &forge.PipelineJob{ID: 6, Name: forge.AIReviewJob, Status: "failed", URL: "https://gl/j/6"}
	f.f.log = "review: UNREVIEWED (no key)"
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if rf.retries != 1 {
		t.Errorf("retries = %d, want 1", rf.retries)
	}
	if got := f.feed(t); strings.Count(got, "no automated review") != 1 || !strings.Contains(got, "https://gl/mr/7") {
		t.Errorf("feed = %s", got)
	}
}

func TestReviewedAIReviewIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	rf := &reviewForge{fakeForge: f.f, job: &forge.PipelineJob{ID: 5, Name: forge.AIReviewJob, Status: "failed"}}
	f.w.ForgeFor = func(string) (forge.Forge, error) { return rf, nil }
	f.f.log = "review posted 3 threads, then the script failed"
	f.f.mr = &forge.MR{IID: 7, State: "opened", Pipeline: &forge.Pipeline{ID: 100, Status: "success"}}
	f.w.Check(f.ctx)
	if rf.retries != 0 {
		t.Errorf("retries = %d, want 0", rf.retries)
	}
}
