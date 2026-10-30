package watch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// mergingForge is an approvingForge that records merges.
type mergingForge struct {
	*approvingForge
	approvers     []forge.ApprovedBy
	merges        []string
	squashed      []string
	retries       []int64
	approvalReads int
	mergeErr      error
	// stale are approvers whose approval predates the head, as ApprovedHead sees it.
	stale map[string]bool
}

func (m *mergingForge) ApprovedHead(_ context.Context, _ int, _ string, who []string) ([]string, error) {
	m.approvalReads++
	var out []string
	for _, u := range who {
		if !m.stale[u] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *mergingForge) Approvals(context.Context, int) (*forge.Approvals, error) {
	return &forge.Approvals{Approved: len(m.approvers) > 0, Approvers: m.approvers}, nil
}
func (m *mergingForge) Merge(_ context.Context, _ int, sha string) error {
	m.merges = append(m.merges, sha)
	return m.mergeErr
}

func (m *mergingForge) RetryJob(_ context.Context, job int64) error {
	m.retries = append(m.retries, job)
	return nil
}

// squashingForge also offers a squash merge.
type squashingForge struct{ *mergingForge }

func (s squashingForge) MergeSquash(_ context.Context, _ int, sha string) error {
	s.squashed = append(s.squashed, sha)
	return nil
}

func mergeFixture(t *testing.T, mode string) (*fixture, *mergingForge, string) {
	t.Helper()
	f := newFixture(t)
	head := headOf(t, f.lane.Worktree)
	mf := &mergingForge{approvingForge: &approvingForge{fakeForge: f.f}}
	f.w.ForgeFor = func(string) (forge.Forge, error) { return mf, nil }
	cfg := config.Default()
	cfg.Defaults.Autonomy.Merge = mode
	f.run.SetConfig(cfg)
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Author: "dev",
		Pipeline: &forge.Pipeline{ID: 9, Status: "success", SHA: head}}
	mf.approvers = []forge.ApprovedBy{{Username: "pat"}}
	return f, mf, head
}

func TestMergeAfterApprovalMergesTheApprovedHead(t *testing.T) {
	f, mf, head := mergeFixture(t, config.AfterApproval)
	f.w.Check(f.ctx)
	if len(mf.merges) != 1 || mf.merges[0] != head {
		t.Fatalf("merges = %v, want one at %s", mf.merges, head)
	}
	if got := f.feed(t); !strings.Contains(got, "approved by pat") {
		t.Errorf("feed = %s", got)
	}
}

func TestMergeAfterApprovalWaitsForEveryCondition(t *testing.T) {
	cases := map[string]func(f *fixture, mf *mergingForge, mr *forge.MR){
		"author approved only": func(_ *fixture, mf *mergingForge, _ *forge.MR) {
			mf.approvers = []forge.ApprovedBy{{Username: "dev"}}
		},
		"bot approved only": func(_ *fixture, mf *mergingForge, _ *forge.MR) {
			mf.approvers = []forge.ApprovedBy{{Username: "ci-helper", Bot: true}, {Username: "project_4_bot_x"}}
		},
		"approval predates the head": func(_ *fixture, mf *mergingForge, _ *forge.MR) {
			mf.stale = map[string]bool{"pat": true}
		},
		"no head":        func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.SHA, mr.Pipeline.SHA = "", "" },
		"not approved":   func(_ *fixture, mf *mergingForge, _ *forge.MR) { mf.approvers = nil },
		"pipeline red":   func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Pipeline.Status = "failed" },
		"pipeline stale": func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Pipeline.SHA = "old" },
		"no pipeline":    func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Pipeline = nil },
		"draft":          func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Draft = true },
		"forge blocks":   func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.MergeStatus = "need_rebase" },
		"open thread": func(f *fixture, _ *mergingForge, _ *forge.MR) {
			f.f.threads = []forge.Thread{{ID: "d1", Body: "x"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f, mf, _ := mergeFixture(t, config.AfterApproval)
			// An open thread would send the agent a run; only the merge is under test.
			mutate(f, mf, f.f.mr)
			f.w.Check(f.ctx)
			if len(mf.merges) != 0 {
				t.Fatalf("merged: %v", mf.merges)
			}
		})
	}
}

func TestMergeAfterApprovalHonoursMergeBots(t *testing.T) {
	f, mf, _ := mergeFixture(t, config.AfterApproval)
	cfg := config.Default()
	cfg.Defaults.Autonomy.Merge = config.AfterApproval
	cfg.Defaults.Autonomy.MergeBots = []string{"pat"}
	f.run.SetConfig(cfg)
	f.w.Check(f.ctx)
	if len(mf.merges) != 0 {
		t.Fatalf("a listed bot's approval merged: %v", mf.merges)
	}
}

func TestOtherMergeSettingsNeverMerge(t *testing.T) {
	for _, mode := range []string{config.Human, config.Agent} {
		f, mf, _ := mergeFixture(t, mode)
		f.w.Check(f.ctx)
		if len(mf.merges) != 0 {
			t.Fatalf("autonomy.merge %s merged: %v", mode, mf.merges)
		}
	}
}

func TestMergeRefusedIsTriedOncePerHead(t *testing.T) {
	f, mf, head := mergeFixture(t, config.AfterApproval)
	mf.mergeErr = errors.New("405 Method Not Allowed: branch cannot be merged")
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if len(mf.merges) != 1 {
		t.Fatalf("merge tried %d times", len(mf.merges))
	}
	if got := f.feed(t); !strings.Contains(got, "forge refused the merge") {
		t.Errorf("feed = %s", got)
	}
	// A new head is a new try.
	f.f.mr.SHA, f.f.mr.Pipeline.SHA = head+"2", head+"2"
	f.w.Check(f.ctx)
	if len(mf.merges) != 2 {
		t.Fatalf("new head not tried: %d", len(mf.merges))
	}
}

func TestAcceptedMergeIsNotSentAgain(t *testing.T) {
	f, mf, _ := mergeFixture(t, config.AfterApproval)
	f.w.Check(f.ctx)
	f.w.Check(f.ctx) // the fake forge still shows the request open
	if len(mf.merges) != 1 {
		t.Fatalf("merge sent %d times", len(mf.merges))
	}
}

// redFixture is a request whose head pipeline failed, with only require-approval failed.
func redFixture(t *testing.T) (*fixture, *mergingForge) {
	t.Helper()
	f, mf, _ := mergeFixture(t, config.AfterApproval)
	f.f.mr.Pipeline.Status = "failed"
	f.f.mr.MergeStatus = "ci_must_pass"
	f.f.jobs = []forge.Job{{ID: 5, Name: forge.RequireApprovalJob}}
	mf.job = &forge.PipelineJob{ID: 5, Name: forge.RequireApprovalJob, Status: "failed"}
	return f, mf
}

func TestRequireApprovalIsRetriedOncePerPipelineAndNotMerged(t *testing.T) {
	f, mf := redFixture(t)
	f.w.Check(f.ctx)
	f.w.Check(f.ctx)
	if len(mf.retries) != 1 || mf.retries[0] != 5 {
		t.Fatalf("retries = %v, want one of job 5", mf.retries)
	}
	if len(mf.merges) != 0 {
		t.Fatalf("merged on a red pipeline: %v", mf.merges)
	}
	if got := f.feed(t); !strings.Contains(got, "retried require-approval") {
		t.Errorf("feed = %s", got)
	}
	// Green now: the next poll merges.
	f.f.mr.Pipeline.Status, f.f.mr.MergeStatus = "success", "mergeable"
	f.w.Check(f.ctx)
	if len(mf.merges) != 1 {
		t.Fatalf("merges = %v after green", mf.merges)
	}
}

func TestRequireApprovalIsRetriedWhateverBlockedStatusTheForgeWords(t *testing.T) {
	for _, st := range []string{"", "ci_must_pass", "blocked_status", "not_approved"} {
		f, mf := redFixture(t)
		f.f.mr.MergeStatus = st
		f.w.Check(f.ctx)
		if len(mf.retries) != 1 {
			t.Errorf("status %q: retries = %v, want one", st, mf.retries)
		}
	}
}

func TestRequireApprovalRetryRefusals(t *testing.T) {
	cases := map[string]func(f *fixture, mf *mergingForge, mr *forge.MR){
		"not approved":      func(_ *fixture, mf *mergingForge, _ *forge.MR) { mf.approvers = nil },
		"author only":       func(_ *fixture, mf *mergingForge, _ *forge.MR) { mf.approvers = []forge.ApprovedBy{{Username: "dev"}} },
		"approval is stale": func(_ *fixture, mf *mergingForge, _ *forge.MR) { mf.stale = map[string]bool{"pat": true} },
		"open thread":       func(f *fixture, _ *mergingForge, _ *forge.MR) { f.f.threads = []forge.Thread{{ID: "d1", Body: "x"}} },
		"another job failed": func(f *fixture, _ *mergingForge, _ *forge.MR) {
			f.f.jobs = append(f.f.jobs, forge.Job{ID: 6, Name: "go-check"})
		},
		"only another job":  func(f *fixture, _ *mergingForge, _ *forge.MR) { f.f.jobs = []forge.Job{{ID: 6, Name: "go-check"}} },
		"pipeline is stale": func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Pipeline.SHA = "old" },
		"draft":             func(_ *fixture, _ *mergingForge, mr *forge.MR) { mr.Draft = true },
		"job not failed":    func(_ *fixture, mf *mergingForge, _ *forge.MR) { mf.job.Status = "success" },
		"opted out": func(f *fixture, _ *mergingForge, _ *forge.MR) {
			cfg := config.Default()
			cfg.Defaults.Autonomy.Merge = config.Human
			f.run.SetConfig(cfg)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f, mf := redFixture(t)
			mutate(f, mf, f.f.mr)
			f.w.Check(f.ctx)
			if len(mf.retries) != 0 || len(mf.merges) != 0 {
				t.Fatalf("retries %v merges %v, want none", mf.retries, mf.merges)
			}
		})
	}
}

func TestMergeSquashesWhereTheForgeCan(t *testing.T) {
	f, mf, head := mergeFixture(t, config.AfterApproval)
	sf := squashingForge{mf}
	f.w.ForgeFor = func(string) (forge.Forge, error) { return sf, nil }
	f.w.Check(f.ctx)
	if len(mf.merges) != 0 {
		t.Fatalf("plain merge used: %v", mf.merges)
	}
	if len(mf.squashed) != 1 || mf.squashed[0] != head {
		t.Fatalf("squashed = %v, want one at %s", mf.squashed, head)
	}
}

func TestUnfinishedPipelineCostsNoApprovalReads(t *testing.T) {
	for _, st := range []string{"running", "pending", "created"} {
		f, mf, _ := mergeFixture(t, config.AfterApproval)
		f.f.mr.Pipeline.Status = st
		f.w.Check(f.ctx)
		if mf.approvalReads != 0 || len(mf.merges) != 0 {
			t.Errorf("pipeline %s: %d approval reads, merges %v", st, mf.approvalReads, mf.merges)
		}
	}
}
