package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

type storeRecorder struct{ st store.Store }

func (r storeRecorder) Record(ctx context.Context, e crook.Entry) error {
	_, err := r.st.AddCrookVerdict(ctx, store.CrookVerdict{At: e.At, Action: string(e.Action), Actor: string(e.Actor),
		Kind: string(e.Kind), Rule: e.Rule, RuleVersion: e.RuleVersion, Stage: e.Stage, Reason: e.Reason,
		Repo: e.Repo, Lane: e.Lane, Run: e.Run, Facts: e.Facts})
	return err
}

// shepherd crook why, log and rules read the verdict log through the daemon.
func TestCrookCLI(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	if code := h.run("crook", "log"); code != 0 || !strings.Contains(h.out.String(), "no verdicts logged") {
		t.Fatalf("empty log: %d %s%s", code, h.out, h.err)
	}

	crook.SetRecorder(storeRecorder{h.srv.Store}, nil)
	defer crook.SetRecorder(nil, nil)
	crook.Check(context.Background(), crook.Request{Action: crook.ActionPush, Actor: crook.ActorWorker,
		Facts: crook.Facts{Push: crook.AutonomyShepherd, Run: 42, Agent: "claude", Lane: "feat-x", Repo: "r"}})
	crook.Check(context.Background(), crook.Request{Action: crook.ActionRunStart, Actor: crook.ActorPerson})
	crook.SetRecorder(nil, nil)

	if code := h.run("crook", "why", "2"); code != 0 || !strings.Contains(h.out.String(), "6. default") || !strings.Contains(h.out.String(), "decided (nothing earlier had a say") {
		t.Fatalf("a default proceed ends its path at default: %d %s", code, h.out)
	}
	if code := h.run("crook", "why", "1"); code != 0 {
		t.Fatalf("why 1: %d %s", code, h.err)
	}
	for _, want := range []string{"verdict #1: refuse", "builtin/push-autonomy (version 1)", "run 42", "1. floor", "no opinion", "4. classify", "5. policy", "decided"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("why output lacks %q:\n%s", want, h.out)
		}
	}
	if strings.Contains(h.out.String(), "6. default") {
		t.Errorf("the path ends at the deciding stage:\n%s", h.out)
	}
	h.run("crook", "why", "run", "42", "--json")
	var list api.CrookVerdicts
	if err := json.Unmarshal(h.out.Bytes(), &list); err != nil || len(list.Verdicts) != 1 || list.Verdicts[0].Run != 42 {
		t.Fatalf("why run: %v %+v", err, list)
	}
	if code := h.run("crook", "why", "lane", "feat-x", "--kind", "proceed"); code != 0 || !strings.Contains(h.out.String(), "no verdicts logged") {
		t.Fatalf("why lane/kind: %d %s", code, h.out)
	}
	if code := h.run("crook", "rules"); code != 0 || !strings.Contains(h.out.String(), "builtin/push-autonomy") {
		t.Fatalf("rules: %d %s", code, h.out)
	}
	for _, bad := range [][]string{{"crook"}, {"crook", "why"}, {"crook", "why", "nope"}, {"crook", "why", "run", "x"}, {"crook", "log", "--kind", "maybe"}, {"crook", "sideways"}, {"crook", "why", "999"}, {"crook", "why", "1", "--kind", "refuse"}, {"crook", "why", "1", "--limit", "3"}} {
		if code := h.run(bad...); code == 0 {
			t.Errorf("%v should fail", bad)
		}
	}
}

// Shadow rows are counted apart in `crook rules` and listed by `crook shadow`.
func TestCrookShadowCLI(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	if code := h.run("crook", "shadow"); code != 0 || !strings.Contains(h.out.String(), "no shadow verdicts") {
		t.Fatalf("empty: %d %s%s", code, h.out, h.err)
	}
	ctx := context.Background()
	rec := storeRecorder{h.srv.Store}
	row := func(action, kind, stage, lane, why string) {
		if err := rec.Record(ctx, crook.Entry{Action: crook.Action(action), Actor: crook.ActorDaemon, Kind: crook.Kind(kind),
			Rule: "spec-verified", RuleVersion: 1, Stage: stage, Reason: why, Repo: "r", Lane: lane}); err != nil {
			t.Fatal(err)
		}
	}
	// A live merge-arm verdict by another rule, and three shadow rows plus an outcome.
	crook.SetRecorder(rec, nil)
	crook.Check(ctx, crook.Request{Action: crook.ActionPush, Actor: crook.ActorWorker,
		Facts: crook.Facts{Push: crook.AutonomyShepherd, Run: 1, Agent: "claude", Lane: "feat-x", Repo: "r"}})
	crook.SetRecorder(nil, nil)
	row("merge", "ask", "shadow", "a", "Not yet met: approved_checks_pass.")
	row("merge", "proceed", "shadow", "b", "all met")
	row("merge", "hold", "shadow", "c", "Not yet met: pipeline_green.")
	row("merge.outcome", "ask", "shadow-outcome", "b", "the person merged !7")

	if code := h.run("crook", "shadow"); code != 0 {
		t.Fatalf("shadow: %d %s", code, h.err)
	}
	for _, want := range []string{"spec-verified", "would have merged", "would have ask", "would have held", "approved_checks_pass", "the person merged !7"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("shadow output lacks %q:\n%s", want, h.out)
		}
	}
	h.run("crook", "shadow", "--json", "--rule", "spec-verified")
	var got shadowOut
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || len(got.Verdicts) != 3 || len(got.Outcomes) != 1 ||
		len(got.Counts) != 1 || got.Counts[0].Proceed != 1 || got.Counts[0].Merged != 1 {
		t.Fatalf("shadow json: %v %+v", err, got)
	}
	// The live count has only the real verdict; the shadow rule is in its own table.
	if code := h.run("crook", "rules"); code != 0 {
		t.Fatalf("rules: %d %s", code, h.err)
	}
	live, shadow, _ := strings.Cut(h.out.String(), "shadow rules")
	if !strings.Contains(live, "builtin/push-autonomy") || strings.Contains(live, "spec-verified") || !strings.Contains(shadow, "spec-verified") {
		t.Errorf("rules output:\n%s", h.out)
	}
	h.run("crook", "log")
	if !strings.Contains(h.out.String(), "spec-verified") {
		t.Errorf("crook log still lists every row, shadow included:\n%s", h.out)
	}
	if code := h.run("crook", "shadow", "extra"); code == 0 {
		t.Error("a stray argument should fail")
	}
}

// shepherd crook layers reads the repo layer from origin's base branch, not the worktree,
// from the default .shepherd folder.
func TestCrookLayersCLI(t *testing.T) {
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", app}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	rule := func(then string) string {
		return "---\nkind: guardrail\nactions: [push]\nthen: " + then + "\n---\nSays " + then + ".\n"
	}
	put := func(path, body string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(app, ".shepherd", "rules", "hold-push.md"), rule("refuse"))
	g("add", ".")
	g("commit", "-q", "-m", "rules")
	g("push", "-q", "origin", "HEAD:main")
	// A loosening in the worktree, uncommitted, must not show.
	put(filepath.Join(app, ".shepherd", "rules", "hold-push.md"), rule("ask"))

	if code := h.run("crook", "layers"); code != 0 {
		t.Fatalf("layers: %d %s%s", code, h.out, h.err)
	}
	out := h.out.String()
	for _, want := range []string{"repo (1)", "hold-push", "-> refuse"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(loosens)") || strings.Contains(out, "-> ask") {
		t.Errorf("the worktree's edit leaked in:\n%s", out)
	}
	h.out.Reset()
	if code := h.run("crook", "layers", "--json"); code != 0 {
		t.Fatalf("json: %s", h.err)
	}
	var v crook.RulesView
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || len(v.Layers) != 4 || len(v.Layers[3].Rules) != 1 {
		t.Fatalf("json view: %v %s", err, h.out)
	}
	if code := h.run("crook", "layers", "extra"); code == 0 {
		t.Error("a stray argument was accepted")
	}
}
