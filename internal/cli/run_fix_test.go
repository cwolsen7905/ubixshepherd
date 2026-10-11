package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// seedFailedRun records a lane with a failed run that has a diagnosis, and returns its id.
func seedFailedRun(t *testing.T, h *harness) int64 {
	t.Helper()
	ctx := context.Background()
	st := h.srv.Store
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "app", Path: "/w/app"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(ctx, ws.ID)
	lane, err := st.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "feat/x", Branch: "feat/x", Worktree: "/w/app-wt/x", State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "p", State: store.RunFailed, Log: "/w/run.log"})
	if err != nil {
		t.Fatal(err)
	}
	code := 2
	if _, err := st.AddDiagnosis(ctx, store.Diagnosis{
		RunID: run.ID, Kind: store.DiagGateFailed, Summary: "the gate failed: make check exited 2", Evidence: "FAIL internal/x\nexit status 2",
		Command: "make check", ExitCode: &code, Files: []string{"internal/x.go"}, Created: time.Now(),
		Fixes: []store.Fix{
			{ID: "widen", Label: "Widen the lane's scope", Op: store.FixWidenScope, NeedsPerson: true, Args: map[string]string{"paths": "internal/x.go"}},
			{ID: "wait", Label: "Wait for the quota", Op: store.FixWaitQuota},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestRunWhyAndFix(t *testing.T) {
	h := newHarness(t, "", false)
	id := seedFailedRun(t, h)

	if code := h.run("run", "why", fmt.Sprint(id)); code != 0 {
		t.Fatalf("run why: %s", h.err)
	}
	out := h.out.String()
	for _, want := range []string{
		"the gate failed: make check exited 2", "kind      gate_failed", "make check (exit 2)", "internal/x.go",
		"| FAIL internal/x", "widen", "[your call]", "wait",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run why lacks %q:\n%s", want, out)
		}
	}

	// A fix with nothing to run says what it did.
	if code := h.run("run", "fix", fmt.Sprint(id), "wait"); code != 0 || !strings.Contains(h.out.String(), "nothing to do") {
		t.Errorf("run fix wait: %d %s %s", code, h.out, h.err)
	}
	// A fix that is not on offer is refused, and so is a missing action.
	if code := h.run("run", "fix", fmt.Sprint(id), "nope"); code == 0 || !strings.Contains(h.err.String(), "no suggested fix") {
		t.Errorf("run fix nope: %d %s", code, h.err)
	}
	if code := h.run("run", "fix", fmt.Sprint(id)); code == 0 {
		t.Error("run fix with no action was accepted")
	}
}

func TestRunWhyWithoutADiagnosis(t *testing.T) {
	h := newHarness(t, "", false)
	id := seedUsage(t, h)
	if code := h.run("run", "why", fmt.Sprint(id)); code != 0 || !strings.Contains(h.out.String(), "no diagnosis") {
		t.Errorf("run why: %d %s %s", code, h.out, h.err)
	}
}

func TestRunWhyAndFixTools(t *testing.T) {
	h := newHarness(t, "", false)
	id := seedFailedRun(t, h)
	resps := mcpExchange(t, h.env,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run_why","arguments":{"id":%d}}}`, id),
		fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_fix","arguments":{"id":%d,"action":"wait"}}}`, id),
		fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_fix","arguments":{"id":%d}}}`, id),
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_why","arguments":{"id":"x"}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "the gate failed") || !strings.Contains(text, "widen") {
		t.Errorf("run_why: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "nothing to do") {
		t.Errorf("run_fix: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[2]); !isErr {
		t.Error("run_fix without an action was accepted")
	}
	if _, isErr := toolText(t, resps[3]); !isErr {
		t.Error("run_why with a string id was accepted")
	}
}

func TestRunDismissAndTools(t *testing.T) {
	h := newHarness(t, "", false)
	id := seedFailedRun(t, h)
	ctx := context.Background()
	dismissed := func() bool {
		r, err := h.srv.Store.Run(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return r.Dismissed != nil
	}

	if code := h.run("run", "dismiss", fmt.Sprint(id)); code != 0 || !strings.Contains(h.out.String(), "log and record stay") {
		t.Fatalf("run dismiss: %d %s %s", code, h.out, h.err)
	}
	if !dismissed() {
		t.Error("run dismiss did not dismiss the run")
	}
	h.out.Reset()
	if code := h.run("run", "undismiss", fmt.Sprint(id)); code != 0 || !strings.Contains(h.out.String(), "back on the board") {
		t.Fatalf("run undismiss: %d %s %s", code, h.out, h.err)
	}
	if dismissed() {
		t.Error("run undismiss left the run dismissed")
	}
	if code := h.run("run", "dismiss", "x"); code == 0 {
		t.Error("run dismiss with a non-number was accepted")
	}

	resps := mcpExchange(t, h.env,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run_dismiss","arguments":{"id":%d}}}`, id),
		fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_undismiss","arguments":{"id":%d}}}`, id),
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_dismiss","arguments":{"id":"x"}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "dismissed run") {
		t.Errorf("run_dismiss: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "back on the board") {
		t.Errorf("run_undismiss: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[2]); !isErr {
		t.Error("run_dismiss with a string id was accepted")
	}
	if dismissed() {
		t.Error("the tools left the run dismissed")
	}
}
