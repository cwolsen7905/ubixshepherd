package watch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/mergerule"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

const shadowRule = `---
kind: merge
mode: shadow
require:
  - pipeline_green
  - up_to_date
then: proceed
---

Merge a green, current request.
`

// storeRec writes the shadow rows to the fixture's store, as the daemon's recorder does.
type storeRec struct {
	st store.Store
}

func (r storeRec) Record(ctx context.Context, e crook.Entry) error {
	_, err := r.st.AddCrookVerdict(ctx, store.CrookVerdict{
		At: e.At, Action: string(e.Action), Actor: string(e.Actor), Kind: string(e.Kind),
		Rule: e.Rule, RuleVersion: e.RuleVersion, Stage: e.Stage, Reason: e.Reason,
		Repo: e.Repo, Lane: e.Lane, Run: e.Run, Facts: e.Facts,
	})
	return err
}

// shadowFixture is the watcher fixture with a shadow merge rule on the base branch and a
// commit on the lane, as a request's head.
func shadowFixture(t *testing.T, rule string) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	ws, _ := f.st.Workspaces(f.ctx)
	rs, _ := f.st.Repos(f.ctx, ws[0].ID)
	app := rs[0].Path
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if rule != "" {
		os.MkdirAll(filepath.Join(app, ".shepherd", "rules"), 0o755)
		os.WriteFile(filepath.Join(app, ".shepherd", "rules", "green.md"), []byte(rule), 0o644)
		g(app, "add", ".")
		g(app, "commit", "-q", "-m", "rule")
		g(app, "push", "-q", "origin", "HEAD:main")
		g(app, "fetch", "-q", "origin")
	}
	// The lane is cut from the base before the rule landed: bring it up to date.
	g(f.lane.Worktree, "merge", "-q", "--no-edit", "origin/main")
	os.WriteFile(filepath.Join(f.lane.Worktree, "new.txt"), []byte("x\n"), 0o644)
	g(f.lane.Worktree, "add", ".")
	g(f.lane.Worktree, "commit", "-q", "-m", "work")
	f.w.EnableShadow(&mergerule.Observer{Rec: storeRec{f.st}})
	return f, headOf(t, f.lane.Worktree)
}

func shadowRows(t *testing.T, f *fixture) []store.CrookVerdict {
	t.Helper()
	f.w.WaitShadow()
	rows, err := f.st.CrookVerdicts(f.ctx, store.CrookFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPollRecordsAShadowRow(t *testing.T) {
	f, head := shadowFixture(t, shadowRule)
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, URL: "https://gl.example.com/g/app/-/merge_requests/7",
		Pipeline: &forge.Pipeline{ID: 1, Status: "success", SHA: head}}
	f.w.Check(f.ctx)
	rows := shadowRows(t, f)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if r.Stage != mergerule.StageShadow || r.Action != "merge" || r.Kind != "proceed" || r.Rule != "green" || r.Repo != "app" || r.Lane != "feat/x" {
		t.Errorf("row = %+v", r)
	}
	if !strings.Contains(f.feed(t), "rule green would have merged !7") {
		t.Errorf("feed = %s", f.feed(t))
	}
	// The same head on the next poll adds nothing.
	f.w.Check(f.ctx)
	if n := len(shadowRows(t, f)); n != 1 {
		t.Errorf("rows after a second poll = %d", n)
	}
}

func TestShadowRowFollowsThePipelineAndFailsClosed(t *testing.T) {
	f, head := shadowFixture(t, shadowRule)
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 1, Status: "failed", SHA: head}}
	f.w.Check(f.ctx)
	rows := shadowRows(t, f)
	if len(rows) != 1 || rows[0].Kind == "proceed" {
		t.Fatalf("a failed pipeline = %+v", rows)
	}
	// No pipeline for this head is not a pass either.
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 2, Status: "success", SHA: "deadbeef"}}
	f.w.Check(f.ctx)
	for _, r := range shadowRows(t, f) {
		if r.Kind == "proceed" {
			t.Errorf("another commit's pipeline passed the rule: %+v", r)
		}
	}
}

func TestNoShadowRuleNoRows(t *testing.T) {
	f, head := shadowFixture(t, "")
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 1, Status: "success", SHA: head}}
	f.w.Check(f.ctx)
	if rows := shadowRows(t, f); len(rows) != 0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestShadowIsOffUnlessEnabled(t *testing.T) {
	f, head := shadowFixture(t, shadowRule)
	f.w.shadow = nil
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 1, Status: "success", SHA: head}}
	f.w.Check(f.ctx)
	if rows := shadowRows(t, f); len(rows) != 0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestMergedRequestRecordsTheOutcome(t *testing.T) {
	f, head := shadowFixture(t, shadowRule)
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 1, Status: "success", SHA: head}}
	f.w.Check(f.ctx)
	shadowRows(t, f)
	f.f.mr = &forge.MR{IID: 7, State: "merged", SHA: head, MergeSHA: "abcdef1234567"}
	f.w.Check(f.ctx)
	var outcome *store.CrookVerdict
	for _, r := range shadowRows(t, f) {
		if r.Stage == mergerule.StageOutcome {
			outcome = &r
		}
	}
	if outcome == nil || !strings.Contains(outcome.Reason, "the rule had said proceed") {
		t.Errorf("outcome = %+v", outcome)
	}
}

// slowRec blocks until released, as a stuck store would.
type slowRec struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (r *slowRec) Record(context.Context, crook.Entry) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	<-r.release
	return nil
}

func TestSlowShadowPassDoesNotHoldUpThePoll(t *testing.T) {
	f, head := shadowFixture(t, shadowRule)
	rec := &slowRec{release: make(chan struct{})}
	f.w.EnableShadow(&mergerule.Observer{Rec: rec})
	f.f.mr = &forge.MR{IID: 7, State: "opened", SHA: head, Pipeline: &forge.Pipeline{ID: 1, Status: "success", SHA: head}}
	done := make(chan struct{})
	go func() {
		f.w.Check(f.ctx)
		f.w.Check(f.ctx) // a pass is still running for the lane: this one does not queue another
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the poll waited for the shadow pass")
	}
	close(rec.release)
	f.w.WaitShadow()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.calls != 1 {
		t.Errorf("recorded %d times", rec.calls)
	}
}
