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
	approvers []forge.ApprovedBy
	merges    []string
	mergeErr  error
	// stale are approvers whose approval predates the head, as ApprovedHead sees it.
	stale map[string]bool
}

func (m *mergingForge) ApprovedHead(_ context.Context, _ int, _ string, who []string) ([]string, error) {
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
