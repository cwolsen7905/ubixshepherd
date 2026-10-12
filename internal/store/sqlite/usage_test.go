package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestUsageRollup(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	_, _, runA := fixture(t, db, "a")
	_, _, runB := fixture(t, db, "b")

	add := func(u store.Usage) {
		t.Helper()
		if err := db.AddUsage(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	add(store.Usage{Day: "2026-10-10", RunID: runA.ID, Agent: "claude", Model: "opus", ContextWindow: 1_000_000, Requests: 3,
		Input: 10, CacheRead: 600_000, CacheCreation: 5_000, Output: 700, PeakContext: 250_000, Compactions: 1, CostUSD: 2})
	add(store.Usage{Day: "2026-10-10", RunID: runB.ID, Agent: "claude", Model: "opus", ContextWindow: 200_000, Requests: 2,
		Input: 5, CacheRead: 50_000, Output: 100, PeakContext: 60_000, CostUSD: 1})
	add(store.Usage{Day: "2026-10-10", Kind: store.UsageDesk, WorkspaceID: 1, Turn: 4, Agent: "claude", Model: "sonnet", Requests: 1,
		Input: 3, CacheRead: 210_000, Output: 40, PeakContext: 210_003, CostUSD: 0.5})
	// A CLI that reports nothing: counted, not measured.
	add(store.Usage{Day: "2026-10-10", RunID: 0, Kind: store.UsageRun, Agent: "cursor", CostUSD: 0.25})
	add(store.Usage{Day: "2026-10-11", RunID: 0, Kind: store.UsageDesk, Agent: "claude", Model: "sonnet", CostUSD: 99})

	got, err := db.UsageStats(ctx, "2026-10-10", "2026-10-10")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total.Count != 4 || got.Total.CostUSD != 3.75 {
		t.Errorf("total = %+v", got.Total)
	}
	if r := got.Runs; r.Count != 3 || r.Measured != 2 || r.Over200k != 1 || r.PeakContext != 250_000 || r.Compactions != 1 ||
		r.CacheRead != 650_000 || r.ContextWindow != 1_000_000 {
		t.Errorf("runs = %+v", r)
	}
	if d := got.Desk; d.Count != 1 || d.Over200k != 1 || d.PeakContext != 210_003 || d.CostUSD != 0.5 {
		t.Errorf("desk = %+v", d)
	}
	if len(got.ByModel) != 2 || got.ByModel[0].Key != "claude opus" || got.ByModel[0].Count != 2 || got.ByModel[1].Agent != "cursor" ||
		got.ByModel[1].Key != "cursor default model" {
		t.Errorf("by model = %+v", got.ByModel)
	}
	if len(got.ByLane) != 3 || got.ByLane[0].Key != "(no lane)" || got.ByLane[1].Key != "a/l-a" || got.ByLane[1].PeakContext != 250_000 ||
		got.ByLane[2].Key != "b/l-b" {
		t.Errorf("by lane = %+v", got.ByLane)
	}
	if len(got.DeskByModel) != 1 || got.DeskByModel[0].Key != "sonnet" {
		t.Errorf("desk by model = %+v", got.DeskByModel)
	}

	none, err := db.UsageStats(ctx, "2020-01-01", "2020-01-31")
	if err != nil || none.Total.Count != 0 {
		t.Errorf("empty range = %+v, %v", none, err)
	}
}

func TestRunUsageIsReplacedAndReadBack(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	_, _, run := fixture(t, db, "a")
	if _, err := db.RunUsage(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no usage yet: %v", err)
	}
	for _, peak := range []int64{10, 20} {
		if err := db.AddUsage(ctx, store.Usage{Day: "2026-10-10", RunID: run.ID, Agent: "claude", PeakContext: peak, Requests: 1}); err != nil {
			t.Fatal(err)
		}
	}
	u, err := db.RunUsage(ctx, run.ID)
	if err != nil || u.PeakContext != 20 || u.Kind != store.UsageRun {
		t.Fatalf("usage = %+v, %v", u, err)
	}
	st, _ := db.UsageStats(ctx, "2026-10-10", "2026-10-10")
	if st.Total.Count != 1 {
		t.Errorf("a re-recorded run counted %d times", st.Total.Count)
	}
}

// A store from before usage was kept migrates with its runs and spend intact.
func TestUsageMigrationKeepsOldRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shepherd.db")
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	old := 0
	for !strings.Contains(migrations[old], "CREATE TABLE usage") {
		old++
	}
	for i := 0; i < old; i++ {
		if _, err := raw.ExecContext(ctx, migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	for _, q := range []string{
		fmt.Sprintf(`PRAGMA user_version = %d`, old),
		`INSERT INTO workspaces (name, path, created) VALUES ('w', '/w', '2026-01-01T00:00:00Z')`,
		`INSERT INTO repos (workspace_id, name, path, created) VALUES (1, 'a', '/w/a', '2026-01-01T00:00:00Z')`,
		`INSERT INTO lanes (repo_id, name, branch, worktree, state, created) VALUES (1, 'l', 'l', '/w/a-wt/l', 'open', '2026-01-01T00:00:00Z')`,
		`INSERT INTO runs (lane_id, agent, prompt, state, log, started, cost_usd) VALUES (1, 'claude', 'p', 'done', '', '2026-01-01T00:00:00Z', 1.25)`,
		`INSERT INTO spend (day, source, ref, usd, created) VALUES ('2026-01-01', 'claude', 1, 1.25, '2026-01-01T00:00:00Z')`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := db.Run(ctx, 1)
	if err != nil || run.CostUSD != 1.25 {
		t.Fatalf("old run = %+v, %v", run, err)
	}
	if _, err := db.RunUsage(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an old run has no usage: %v", err)
	}
	st, err := db.UsageStats(ctx, "2026-01-01", "2026-01-01")
	if err != nil || st.Total.Count != 0 {
		t.Errorf("stats = %+v, %v", st, err)
	}
}

func TestDiagnoses(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	_, _, runA := fixture(t, db, "a")
	_, _, runB := fixture(t, db, "b")
	code := 2
	reset := time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)
	if _, err := db.AddDiagnosis(ctx, store.Diagnosis{RunID: runA.ID, Kind: store.DiagAgentError, Summary: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddDiagnosis(ctx, store.Diagnosis{RunID: runA.ID, Kind: store.DiagGateFailed, Summary: "second", Evidence: "FAIL x",
		Command: "make check", ExitCode: &code, Files: []string{"a.go"}, ResetAt: &reset,
		Fixes: []store.Fix{{ID: "continue", Op: store.FixContinue, Args: map[string]string{"prompt": "fix"}, DeskOK: true}}}); err != nil {
		t.Fatal(err)
	}
	all, err := db.Diagnoses(ctx, runA.ID)
	if err != nil || len(all) != 2 || all[0].Summary != "second" {
		t.Fatalf("diagnoses = %+v, %v", all, err)
	}
	d := all[0]
	if d.ExitCode == nil || *d.ExitCode != 2 || d.Command != "make check" || len(d.Files) != 1 || d.ResetAt == nil || !d.ResetAt.Equal(reset) ||
		len(d.Fixes) != 1 || !d.Fixes[0].DeskOK || d.Fixes[0].Args["prompt"] != "fix" || d.Created.IsZero() {
		t.Fatalf("round trip lost something: %+v", d)
	}
	latest, err := db.LatestDiagnoses(ctx, runA.ID, runB.ID)
	if err != nil || len(latest) != 1 || latest[runA.ID].Summary != "second" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	if got, _ := db.LatestDiagnoses(ctx); len(got) != 0 {
		t.Fatalf("no ids gave %+v", got)
	}
}

func TestCostEstimatedRoundTripsAndTotals(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	_, _, runA := fixture(t, db, "a")
	_, _, runB := fixture(t, db, "b")
	runA.CostEstimated, runA.CostUSD = true, 0.05
	if err := db.UpdateRun(ctx, runA); err != nil {
		t.Fatal(err)
	}
	if got, err := db.Run(ctx, runA.ID); err != nil || !got.CostEstimated {
		t.Fatalf("run = %+v, %v", got, err)
	}
	for _, u := range []store.Usage{
		{Day: "2026-10-10", RunID: runA.ID, Agent: "opencode", Model: "openrouter/x/y", CostUSD: 0.05, CostEstimated: true},
		{Day: "2026-10-10", RunID: runB.ID, Agent: "claude", Model: "opus", CostUSD: 2},
	} {
		if err := db.AddUsage(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if u, err := db.RunUsage(ctx, runA.ID); err != nil || !u.CostEstimated {
		t.Errorf("usage = %+v, %v", u, err)
	}
	st, err := db.UsageStats(ctx, "2026-10-10", "2026-10-10")
	if err != nil {
		t.Fatal(err)
	}
	if st.Total.CostUSD != 2.05 || st.Total.EstimatedUSD != 0.05 {
		t.Errorf("total = %+v: only the estimated run's cost is an estimate", st.Total)
	}
	h, err := db.RunHistory(ctx, store.RunHistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if h.EstimatedUSD != 0.05 {
		t.Errorf("history estimated = %v", h.EstimatedUSD)
	}
	est := 0
	for _, r := range h.Runs {
		if r.CostEstimated {
			est++
		}
	}
	if est != 1 {
		t.Errorf("%d runs marked estimated in the history, want 1", est)
	}
}

func TestEstimateMigrationMarksHostedOpenCodeRuns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shepherd.db")
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	for i, m := range migrations {
		if strings.Contains(m, "cost_estimated") {
			last = i
		}
	}
	for i := 0; i < last; i++ {
		if _, err := raw.ExecContext(ctx, migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	for _, q := range []string{
		fmt.Sprintf(`PRAGMA user_version = %d`, last),
		`INSERT INTO workspaces (name, path, created) VALUES ('w', '/w', '2026-01-01T00:00:00Z')`,
		`INSERT INTO repos (workspace_id, name, path, created) VALUES (1, 'a', '/w/a', '2026-01-01T00:00:00Z')`,
		`INSERT INTO lanes (repo_id, name, branch, worktree, state, created) VALUES (1, 'l', 'l', '/w/a-wt/l', 'open', '2026-01-01T00:00:00Z')`,
		`INSERT INTO runs (lane_id, agent, model, prompt, state, log, started, cost_usd) VALUES
			(1, 'opencode', 'openrouter/qwen/qwen3-coder', 'p', 'done', '', '2026-01-01T00:00:00Z', 0.05),
			(1, 'opencode', 'local/qwen3', 'p', 'done', '', '2026-01-01T00:00:00Z', 0),
			(1, 'opencode', '', 'p', 'done', '', '2026-01-01T00:00:00Z', 0),
			(1, 'claude', 'opus', 'p', 'done', '', '2026-01-01T00:00:00Z', 1)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id, want := range map[int64]bool{1: true, 2: false, 3: false, 4: false} {
		r, err := db.Run(ctx, id)
		if err != nil || r.CostEstimated != want {
			t.Errorf("run %d estimated = %v, %v; want %v", id, r.CostEstimated, err, want)
		}
	}
}
