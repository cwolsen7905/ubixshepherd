package dispatch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// diagnosisOf is a run's newest diagnosis, waiting a moment for it to land.
func diagnosisOf(t *testing.T, f *fixture, runID int64) store.Diagnosis {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ds, err := f.st.Diagnoses(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ds) > 0 {
			return ds[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %d has no diagnosis", runID)
	return store.Diagnosis{}
}

func hasFix(d store.Diagnosis, id string) (store.Fix, bool) {
	for _, f := range d.Fixes {
		if f.ID == id {
			return f, true
		}
	}
	return store.Fix{}, false
}

func startAndDiagnose(t *testing.T, f *fixture, agent string) (store.Run, store.Diagnosis) {
	t.Helper()
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: agent, Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	return done, diagnosisOf(t, f, run.ID)
}

func TestDiagnoseAgentErrorKeepsRedactedEvidence(t *testing.T) {
	f := newFixture(t, "boom")
	run, d := startAndDiagnose(t, f, "claude")
	if d.Kind != store.DiagAgentError || run.State != store.RunFailed {
		t.Fatalf("diagnosis = %+v, run %s", d, run.State)
	}
	if !strings.Contains(d.Summary, "exit 2") || !strings.Contains(d.Summary, "panic: runtime error") {
		t.Errorf("summary = %q", d.Summary)
	}
	if !strings.Contains(d.Evidence, "compiling") || strings.Contains(d.Evidence, "hunter2") || strings.Contains(d.Evidence, "glpat-") {
		t.Errorf("evidence = %q", d.Evidence)
	}
	if d.Log != run.Log || d.ExitCode == nil || *d.ExitCode != 2 {
		t.Errorf("log/exit = %q %v", d.Log, d.ExitCode)
	}
	cont, ok := hasFix(d, "continue")
	if !ok || !cont.DeskOK || cont.NeedsPerson || !strings.Contains(cont.Args["prompt"], "panic: runtime error") {
		t.Errorf("continue fix = %+v", cont)
	}
	if other, ok := hasFix(d, "retry_other"); !ok || other.DeskOK {
		t.Errorf("retry_other = %+v", other)
	}
	if _, ok := hasFix(d, "open_log"); !ok {
		t.Error("no open_log fix")
	}
	// The feed line carries the reason, not just "failed".
	if k := feedKind(t, f, "panic: runtime error"); k != store.FeedRunFailed {
		t.Errorf("feed kind = %q", k)
	}
}

func TestDiagnoseQuotaHasResetTime(t *testing.T) {
	f := newFixture(t, "limit")
	_, d := startAndDiagnose(t, f, "cursor")
	if d.Kind != store.DiagQuota || d.ResetAt == nil || !d.ResetAt.After(time.Now()) {
		t.Fatalf("diagnosis = %+v", d)
	}
	if !strings.Contains(d.Summary, "out of quota") || !strings.Contains(d.Summary, d.ResetAt.Local().Format("Jan 2 15:04")) {
		t.Errorf("summary = %q", d.Summary)
	}
	if w, ok := hasFix(d, "wait_quota"); !ok || !w.DeskOK {
		t.Errorf("wait fix = %+v", w)
	}
}

func TestDiagnoseLimitWordsInWorkAreNotQuota(t *testing.T) {
	f := newFixture(t, "mention")
	_, d := startAndDiagnose(t, f, "cursor")
	if d.Kind != store.DiagAgentError {
		t.Errorf("kind = %s", d.Kind)
	}
}

func TestDiagnoseHungAndNoChange(t *testing.T) {
	old := openCodeIdle
	openCodeIdle = func() time.Duration { return 400 * time.Millisecond }
	t.Cleanup(func() { openCodeIdle = old })
	f := openCodeFixture(t, "hang")
	_, d := startAndDiagnose(t, f, "opencode")
	if d.Kind != store.DiagHung || !strings.Contains(d.Summary, "printed nothing for 400ms") {
		t.Errorf("hung = %+v", d)
	}
}

func TestDiagnoseNoChange(t *testing.T) {
	g := openCodeFixture(t, "wait")
	_, d := startAndDiagnose(t, g, "opencode")
	if d.Kind != store.DiagNoChange || !strings.Contains(d.Summary, "changed nothing") {
		t.Errorf("no change = %+v", d)
	}
}

func TestDiagnoseStoppedAndInterrupted(t *testing.T) {
	f := newFixture(t, "sleep")
	ctx := context.Background()
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	time.Sleep(200 * time.Millisecond)
	if err := f.runner.Stop(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	f.wait(t, run.ID)
	d := diagnosisOf(t, f, run.ID)
	if d.Kind != store.DiagStopped {
		t.Errorf("stopped = %+v", d)
	}
	if c, ok := hasFix(d, "continue"); !ok || c.DeskOK || !c.NeedsPerson {
		t.Errorf("a stopped run's continue is the person's: %+v", c)
	}

	g := newFixture(t, "sleep")
	run, _ = g.runner.Start(ctx, StartRequest{LaneID: g.lane.ID, Agent: "claude", Prompt: "x"})
	time.Sleep(200 * time.Millisecond)
	stop, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	g.runner.Shutdown(stop)
	d = diagnosisOf(t, g, run.ID)
	if d.Kind != store.DiagInterrupted || !strings.Contains(d.Summary, "daemon stopped") {
		t.Errorf("shutdown = %+v", d)
	}
}

func TestDiagnoseRecoverKeepsLogTail(t *testing.T) {
	f := newFixture(t, "ok")
	ctx := context.Background()
	log := t.TempDir() + "/9.log"
	os.WriteFile(log, []byte("# shepherd run 9\nworking on it\ntoken=abcdefghijklmnop1234\n"), 0o600)
	stale, _ := f.st.CreateRun(ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: log, Session: "s"})
	if err := f.runner.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	d := diagnosisOf(t, f, stale.ID)
	if d.Kind != store.DiagInterrupted || !strings.Contains(d.Evidence, "working on it") || strings.Contains(d.Evidence, "abcdefghijklmnop1234") {
		t.Errorf("diagnosis = %+v", d)
	}
	if !strings.Contains(d.Summary, "crashed") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestDiagnoseStartFailed(t *testing.T) {
	f := newFixture(t, "ok")
	f.runner.lookPath = func(string) (string, error) { return t.TempDir() + "/missing-agent", nil }
	// A binary that is found on PATH but cannot be executed fails at spawn, with a run.
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x", NewSession: true})
	if err == nil || run.ID == 0 {
		t.Fatalf("run %d, error %v: want a run that failed to start", run.ID, err)
	}
	d := diagnosisOf(t, f, run.ID)
	if d.Kind != store.DiagStartFailed || !strings.Contains(d.Summary, "could not start") {
		t.Errorf("diagnosis = %+v", d)
	}
	if _, ok := hasFix(d, "retry"); !ok {
		t.Error("no retry fix")
	}
}

func TestDiagnosePushRefusedByTheHook(t *testing.T) {
	f := newFixture(t, "refused")
	_, d := startAndDiagnose(t, f, "claude")
	if d.Kind != store.DiagOutsideScope || !strings.Contains(d.Summary, "push was refused") || !strings.Contains(d.Evidence, "stray.txt") {
		t.Errorf("diagnosis = %+v", d)
	}
	if w, ok := hasFix(d, "widen_scope"); !ok || !w.NeedsPerson || w.DeskOK {
		t.Errorf("widen_scope = %+v", w)
	}

	g := newFixture(t, "forbidden")
	_, d = startAndDiagnose(t, g, "claude")
	if d.Kind != store.DiagCommitsForbidden {
		t.Errorf("forbidden = %+v", d)
	}
}

func TestDiagnoseTurnCap(t *testing.T) {
	f := newFixture(t, "turns")
	_, d := startAndDiagnose(t, f, "claude")
	if d.Kind != store.DiagBudgetCap || !strings.Contains(d.Summary, "turn limit") {
		t.Errorf("diagnosis = %+v", d)
	}
	if r, ok := hasFix(d, "raise_budget"); !ok || !r.NeedsPerson {
		t.Errorf("raise_budget = %+v", r)
	}
}

func TestDiagnoseOutsideScopeOnASucceededRun(t *testing.T) {
	f := newFixture(t, "stray")
	run, d := startAndDiagnose(t, f, "claude")
	if run.State != store.RunSucceeded || d.Kind != store.DiagOutsideScope || len(d.Files) != 1 || d.Files[0] != "stray.txt" {
		t.Errorf("run %s, diagnosis = %+v", run.State, d)
	}
}

func TestDiagnoseGateFailedKeepsCommandAndTail(t *testing.T) {
	f, _ := shipFixture(t, "ok", strings.Replace(pushes, `gate: "true"`, `gate: "echo src/work.go:12 FAIL: nope; exit 7"`, 1))
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	waitFeed(t, f, "asked claude to fix it")
	var d store.Diagnosis
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && d.Kind != store.DiagGateFailed {
		ds, _ := f.st.Diagnoses(context.Background(), run.ID)
		for _, x := range ds {
			if x.Kind == store.DiagGateFailed {
				d = x
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d.Kind != store.DiagGateFailed || d.ExitCode == nil || *d.ExitCode != 7 || !strings.Contains(d.Command, "exit 7") {
		t.Fatalf("diagnosis = %+v", d)
	}
	if !strings.Contains(d.Evidence, "FAIL: nope") || len(d.Files) != 1 || d.Files[0] != "src/work.go" || d.Log == "" {
		t.Errorf("evidence/files/log = %q %v %q", d.Evidence, d.Files, d.Log)
	}
	if ship, ok := hasFix(d, "ship"); !ok || !ship.NeedsPerson || ship.DeskOK {
		t.Errorf("ship fix = %+v", ship)
	}
	// The run itself stays succeeded: routing and shipping depend on it.
	if got, _ := f.st.Run(context.Background(), run.ID); got.State != store.RunSucceeded {
		t.Errorf("run = %s", got.State)
	}
}

func TestDiagnoseCommitRulesAndStandardsText(t *testing.T) {
	f, _ := shipFixture(t, "ok", pushes+"    forbid: [\"(?i)agent work\"]\n")
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	waitFeed(t, f, "commit rules failed")
	ds, _ := f.st.Diagnoses(context.Background(), run.ID)
	found := false
	for _, d := range ds {
		if d.Kind == store.DiagCommitsForbidden && strings.Contains(d.Evidence, "agent work") {
			found = true
		}
	}
	if !found {
		t.Errorf("diagnoses = %+v", ds)
	}

	d := diagnoseStandards("standard go-vet (must) failed: `go vet ./...`\n./a.go:3: bad thing\nmore")
	if d.Kind != store.DiagStandardsFailed || d.Command != "go vet ./..." || !strings.Contains(d.Summary, "go-vet") || !strings.Contains(d.Summary, "bad thing") {
		t.Errorf("standards = %+v", d)
	}
}

func TestEvidenceIsCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "line %d %s\n", i, strings.Repeat("x", 100))
	}
	d := commandFailure(store.DiagGateFailed, "the gate", "make", nil, b.String(), "")
	if len(d.Evidence) > evidenceBytes+10 || strings.Count(d.Evidence, "\n") > evidenceLines || !strings.Contains(d.Evidence, "line 499") {
		t.Errorf("evidence is %d bytes, %d lines", len(d.Evidence), strings.Count(d.Evidence, "\n"))
	}
}

func TestApplyFixHonoursWhoMayTakeIt(t *testing.T) {
	f := newFixture(t, "boom")
	ctx := context.Background()
	run, d := startAndDiagnose(t, f, "claude")
	_ = d
	// The desk may not take what is the person's, nor edit what it takes.
	if _, err := f.runner.ApplyFix(ctx, run.ID, "retry_other", ByDesk, nil); err == nil || !strings.Contains(err.Error(), "person's call") {
		t.Errorf("desk retry_other: %v", err)
	}
	if _, err := f.runner.ApplyFix(ctx, run.ID, "continue", ByDesk, map[string]string{"prompt": "x"}); err == nil {
		t.Error("desk edited a fix")
	}
	if _, err := f.runner.ApplyFix(ctx, run.ID, "nope", ByPerson, nil); err == nil {
		t.Error("unknown fix applied")
	}
	if _, err := f.runner.ApplyFix(ctx, run.ID, "retry_other", ByPerson, nil); err == nil || !strings.Contains(err.Error(), "name the agent") {
		t.Errorf("retry_other without an agent: %v", err)
	}
	res, err := f.runner.ApplyFix(ctx, run.ID, "continue", ByDesk, nil)
	if err != nil || res.Run == nil || !res.Done {
		t.Fatalf("continue: %+v %v", res, err)
	}
	if got := f.wait(t, res.Run.ID); got.Parent != run.ID || !strings.Contains(got.Prompt, "panic: runtime error") {
		t.Errorf("continued run = %+v", got)
	}
	res, err = f.runner.ApplyFix(ctx, run.ID, "retry", ByPerson, nil)
	if err != nil || res.Run == nil || res.Run.Parent != 0 {
		t.Fatalf("retry: %+v %v", res, err)
	}
	f.wait(t, res.Run.ID)
	if res, err := f.runner.ApplyFix(ctx, run.ID, "open_log", ByDesk, nil); err != nil || res.Message != run.Log {
		t.Errorf("open_log: %+v %v", res, err)
	}
}

func TestClassifyEndKeepsTheTail(t *testing.T) {
	code := 1
	run := store.Run{ID: 5, State: store.RunFailed, ExitCode: &code, Log: "/l"}
	d, ok := classifyEnd(endInfo{Run: run, Agent: "claude", Idled: true, Idle: time.Minute, Tail: "step one\nstep two token=abcdefghijklmnop1234"})
	if !ok || d.Kind != store.DiagHung || !strings.Contains(d.Evidence, "step two") || strings.Contains(d.Evidence, "abcdefghijklmnop1234") {
		t.Errorf("hung = %+v", d)
	}
	if _, ok := classifyEnd(endInfo{Run: store.Run{State: store.RunSucceeded}, Agent: "claude"}); ok {
		t.Error("a clean run was diagnosed")
	}
	// Shepherd's own block on an agent's push is not a refusal that failed the run.
	d, _ = classifyEnd(endInfo{Run: run, Agent: "claude", Tail: "git push\nfatal: unable to access shepherd-run-blocks-push://x\nboom: exploded"})
	if d.Kind == store.DiagPushRefused {
		t.Errorf("own push block read as a refusal: %+v", d)
	}
}
