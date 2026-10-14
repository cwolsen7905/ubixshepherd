package waiting

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func TestCollectGaps(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	ws, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app")}})
	repos, _ := st.Repos(ctx, ws.ID)
	app := repos[0]
	conf := config.Default()
	run := func(l store.Lane, state string) store.Run {
		r, err := st.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Prompt: "x", State: state, Log: filepath.Join(root, "r.log")})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	report := func(r store.Run, status, text string) {
		if _, err := st.AddEvent(ctx, store.Event{RunID: r.ID, Kind: dispatch.EventReport, Status: status, Text: text}); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Blocked: last report blocked waits; a later progress report or a running run does not.
	blocked := lane(t, st, app, "blocked", 0)
	report(run(blocked, store.RunSucceeded), "blocked", "need the schema")
	moved := lane(t, st, app, "moved", 0)
	report(run(moved, store.RunSucceeded), "blocked", "x")
	report(run(moved, store.RunSucceeded), "done", "y")

	// 2. Scope: approved widening not applied waits; a refusal or a covered path does not.
	wide := lane(t, st, app, "wide", 0)
	if err := st.SetLaneScope(ctx, wide.ID, []string{"internal/a/**"}); err != nil {
		t.Fatal(err)
	}
	wr := run(wide, store.RunSucceeded)
	ask := func(q, a string) {
		d, err := st.CreateDecision(ctx, store.Decision{RunID: wr.ID, Question: q, State: store.DecisionOpen})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AnswerDecision(ctx, d.ID, a); err != nil {
			t.Fatal(err)
		}
	}
	ask("Widen scope to include `internal/b/x.go` and internal/a/y.go?", "Yes, go ahead")
	ask("Widen the scope to cmd/z.go?", "No, don't")
	ask("Widen the scope to internal/a/q.go?", "yes")

	// 3. Next closed unmerged waits; one the forge shows merged does not.
	closed := lane(t, st, app, "closed", 0)
	merged := lane(t, st, app, "squashed", 0)
	for _, l := range []store.Lane{closed, merged} {
		if err := st.SetLaneState(ctx, l.ID, store.LaneClosed); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutLaneForge(ctx, store.LaneForge{LaneID: merged.ID, MR: 9, MRState: "merged"}); err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.NextItem{
		{Title: "Lost", Source: store.NextByDesk, State: store.NextApproved, LaneID: closed.ID},
		{Title: "Landed", Source: store.NextByDesk, State: store.NextApproved, LaneID: merged.ID},
	} {
		if _, err := st.AddNextItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	// A merged MR on an open lane never lists, even with a failed pipeline and a stale state.
	stale := lane(t, st, app, "stale", 0)
	if err := st.PutLaneForge(ctx, store.LaneForge{LaneID: stale.ID, MR: 184, MRState: "opened", PipelineStatus: "failed", MergeSHA: "abc"}); err != nil {
		t.Fatal(err)
	}

	w, err := Collect(ctx, st, conf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Counts != (api.WaitingCounts{Blocked: 1, Scope: 1, Next: 1}) {
		t.Fatalf("counts %+v: %+v", w.Counts, w.Items)
	}
	for _, it := range w.Items {
		switch it.Kind {
		case api.WaitingBlocked:
			if it.Lane != "blocked" || !strings.Contains(it.Detail, "need the schema") {
				t.Errorf("blocked: %+v", it)
			}
		case api.WaitingScope:
			if it.Lane != "wide" || !strings.Contains(it.Title, "internal/b/x.go") || strings.Contains(it.Title, "internal/a/y.go") {
				t.Errorf("scope: %+v", it)
			}
		case api.WaitingNext:
			if it.Title != "Lost" {
				t.Errorf("next: %+v", it)
			}
		default:
			t.Errorf("unexpected: %+v", it)
		}
	}
}
