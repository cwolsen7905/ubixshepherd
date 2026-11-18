package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// seedStatusRun records a lane with a succeeded run whose log file holds 50 lines,
// a progress report followed by a long done report, an open decision and a pending
// question from the run. It returns the run's id and the report's full text.
func seedStatusRun(t *testing.T, h *harness) (int64, string) {
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
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	logPath := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(logPath, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "add the form", State: store.RunSucceeded, Log: logPath})
	if err != nil {
		t.Fatal(err)
	}
	add := func(status, text string) {
		t.Helper()
		if _, err := st.AddEvent(ctx, store.Event{RunID: run.ID, Kind: dispatch.EventReport, Status: status, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	add("progress", "half way through the form")
	report := "done: the form validates on the client and the server, " +
		"errors are announced to assistive tech, and make check passes on a clean tree"
	add("done", report)
	if _, err := st.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "may the form write outside its scope?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRequest(ctx, store.Request{FromRun: run.ID, Kind: store.RequestQuestion,
		Message: "which error style does the design pack want?", State: store.RequestPending, Depth: 1}); err != nil {
		t.Fatal(err)
	}
	return run.ID, report
}

func TestRunStatusLeadsWithReport(t *testing.T) {
	h := newHarness(t, "", false)
	id, report := seedStatusRun(t, h)

	if code := h.run("run", "status", fmt.Sprint(id)); code != 0 {
		t.Fatalf("run status: %d %s", code, h.err)
	}
	out := h.out.String()
	for _, want := range []string{
		"report    done",
		report, // the full summary text, not truncated
		"needs     decision",
		"which error style",
		"run " + fmt.Sprint(id),
		"agent     claude",
		"task      add the form",
		"--- log tail (last 40 lines) ---",
		"line 50",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line 10\n") || strings.Contains(out, "half way through the form") {
		t.Errorf("run status keeps more than the tail or the superseded progress report:\n%s", out)
	}
	// The report leads, then the header, then the tail.
	ri, hi, ti := strings.Index(out, "report    done"), strings.Index(out, "run "+fmt.Sprint(id)), strings.Index(out, "--- log tail")
	if ri < 0 || hi < 0 || ti < 0 || !(ri < hi && hi < ti) {
		t.Errorf("run status is not report, header, tail:\n%s", out)
	}
}

func TestRunStatusWithoutReportNeedsNothing(t *testing.T) {
	h := newHarness(t, "", false)
	id := seedUsage(t, h)

	if code := h.run("run", "status", fmt.Sprint(id)); code != 0 {
		t.Fatalf("run status: %d %s", code, h.err)
	}
	out := h.out.String()
	for _, want := range []string{"report    none yet", "needs     none", "usage     claude-opus-4-5"} {
		if !strings.Contains(out, want) {
			t.Errorf("run status lacks %q:\n%s", want, out)
		}
	}
}

func TestRunStatusJSONAddsFields(t *testing.T) {
	h := newHarness(t, "", false)
	id, report := seedStatusRun(t, h)

	if code := h.run("run", "status", fmt.Sprint(id), "--json"); code != 0 {
		t.Fatalf("run status --json: %d %s", code, h.err)
	}
	var got struct {
		api.RunView
		ReportStatus string `json:"report_status"`
		ReportText   string `json:"report_text"`
		NeedsPerson  string `json:"needs_person"`
		LogTail      string `json:"log_tail"`
	}
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
		t.Fatalf("run status --json is not JSON: %v\n%s", err, h.out.String())
	}
	// The run view itself is unchanged.
	if got.ID != id || got.State != store.RunSucceeded || got.Agent != "claude" {
		t.Errorf("run view changed: %+v", got.RunView)
	}
	if got.ReportStatus != "done" || got.ReportText != report {
		t.Errorf("report fields: %q %q", got.ReportStatus, got.ReportText)
	}
	if !strings.Contains(got.NeedsPerson, "decision") || !strings.Contains(got.NeedsPerson, "request") {
		t.Errorf("needs_person: %q", got.NeedsPerson)
	}
	if !strings.HasSuffix(got.LogTail, "line 50") || strings.Contains(got.LogTail, "line 10\n") {
		t.Errorf("log_tail is not the last 40 lines: %q", got.LogTail)
	}
	var raw map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "state", "prompt", "report_status", "report_text", "needs_person", "log_tail"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("json lacks %q", field)
		}
	}
}

func TestRunStatusRejectsBadIDs(t *testing.T) {
	h := newHarness(t, "", false)
	if code := h.run("run", "status"); code == 0 {
		t.Error("run status without an id was accepted")
	}
	if code := h.run("run", "status", "x"); code == 0 || !strings.Contains(h.err.String(), "not a number") {
		t.Errorf("run status x: %d %s", code, h.err)
	}
}
