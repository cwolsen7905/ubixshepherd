package cli

import (
	"context"
	"encoding/json"
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
	crook.SetRecorder(nil, nil)

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
	for _, bad := range [][]string{{"crook"}, {"crook", "why"}, {"crook", "why", "nope"}, {"crook", "why", "run", "x"}, {"crook", "log", "--kind", "maybe"}, {"crook", "sideways"}, {"crook", "why", "999"}} {
		if code := h.run(bad...); code == 0 {
			t.Errorf("%v should fail", bad)
		}
	}
}
