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

func TestRunStatusNeedsOnlyWhatWaitsOnThePerson(t *testing.T) {
	h := newHarness(t, "", false)
	id, _ := seedStatusRun(t, h)
	for state, msg := range map[string]string{
		store.RequestRouted:       "routed question about the api",
		store.RequestNeedsRouting: "unrouted question about the schema",
		store.RequestReplyReady:   "answered question about colours",
	} {
		if _, err := h.srv.Store.CreateRequest(context.Background(), store.Request{FromRun: id,
			Kind: store.RequestQuestion, Message: msg, State: state, Depth: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if code := h.run("run", "status", fmt.Sprint(id)); code != 0 {
		t.Fatalf("run status: %d %s", code, h.err)
	}
	out := h.out.String()
	for _, want := range []string{"which error style", "answered question about colours"} {
		if !strings.Contains(out, want) {
			t.Errorf("needs line lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"routed question about the api", "unrouted question about the schema"} {
		if strings.Contains(out, bad) {
			t.Errorf("needs line lists %q, which does not wait on the person:\n%s", bad, out)
		}
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

func TestRunStatusToolLeadsWithReport(t *testing.T) {
	h := newHarness(t, "", false)
	id, report := seedStatusRun(t, h)
	resps := mcpExchange(t, h.env,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run_status","arguments":{"id":%d}}}`, id),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_status","arguments":{"id":"x"}}}`,
	)
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("run_status: %s", text)
	}
	for _, want := range []string{"report    done", report, "needs     decision", "line 50"} {
		if !strings.Contains(text, want) {
			t.Errorf("run_status lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "line 10\n") {
		t.Errorf("run_status prints more than the tail:\n%s", text)
	}
	if _, isErr := toolText(t, resps[1]); !isErr {
		t.Error("run_status with a string id was accepted")
	}
}

// seedLongLogRun records a lane with a run whose log file spans several of the
// daemon's 64 KiB log chunks, and returns its id. Chunk boundaries can fall mid-line,
// so the tail must be assembled from the raw stream, not from chunk starts.
func seedLongLogRun(t *testing.T, h *harness, lines int) int64 {
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
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "output line %05d with padding to widen each chunk\n", i)
	}
	logPath := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(logPath, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "p", State: store.RunSucceeded, Log: logPath})
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

// A log far larger than one chunk still yields exactly the last 40 lines, joined
// across chunk boundaries.
func TestRunStatusTailAcrossChunks(t *testing.T) {
	h := newHarness(t, "", false)
	const lines = 3000
	id := seedLongLogRun(t, h, lines)

	if code := h.run("run", "status", fmt.Sprint(id)); code != 0 {
		t.Fatalf("run status: %d %s", code, h.err)
	}
	out := h.out.String()
	_, after, found := strings.Cut(out, "--- log tail (last 40 lines) ---\n")
	if !found {
		t.Fatalf("no log tail section:\n%s", out)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSuffix(after, "\n"), "\n") {
		got = append(got, l)
	}
	if len(got) != 40 {
		t.Fatalf("tail holds %d lines, want 40", len(got))
	}
	for i, l := range got {
		if want := fmt.Sprintf("output line %05d with padding to widen each chunk", lines-40+1+i); l != want {
			t.Fatalf("tail line %d = %q, want %q", i, l, want)
		}
	}
	if strings.Contains(after, "output line 00001") {
		t.Errorf("the tail leaks the log's start")
	}
}

// countingWriter records each Write call so a test can tell streamed output from one
// buffered write.
type countingWriter struct {
	buf    strings.Builder
	writes []int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, len(p))
	return w.buf.Write(p)
}

// `run logs` writes each chunk as it arrives instead of collecting the whole log first.
func TestRunLogsStreamsChunks(t *testing.T) {
	h := newHarness(t, "", false)
	const lines = 3000
	id := seedLongLogRun(t, h, lines)
	w := &countingWriter{}
	h.env.Stdout = w

	if code := Run(context.Background(), h.env, []string{"run", "logs", fmt.Sprint(id)}); code != 0 {
		t.Fatalf("run logs: %d %s", code, h.err)
	}
	var nonEmpty []int
	total := 0
	for _, n := range w.writes {
		total += n
		if n > 0 {
			nonEmpty = append(nonEmpty, n)
		}
	}
	if len(nonEmpty) < 2 {
		t.Fatalf("log was written in %d piece(s), want one per chunk", len(nonEmpty))
	}
	for _, n := range nonEmpty {
		if n > 64<<10 {
			t.Errorf("a single write of %d bytes exceeds one 64 KiB chunk", n)
		}
	}
	out := w.buf.String()
	if total != len(out) || !strings.HasPrefix(out, "output line 00001 ") || !strings.HasSuffix(out, fmt.Sprintf("output line %05d with padding to widen each chunk\n", lines)) {
		t.Errorf("streamed log is not the whole log (%d bytes)", len(out))
	}
}
