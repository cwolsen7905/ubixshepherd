package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func oneLane(t *testing.T, db *DB) int64 {
	t.Helper()
	ctx := context.Background()
	ws, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "a", Path: "/w/a"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := db.Repos(ctx, ws.ID)
	l, err := db.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "x", Branch: "x", Worktree: "/w/x", State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}

func TestLaneForgeTimesRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	at := time.Date(2026, 10, 11, 9, 30, 0, 123, time.UTC)
	in := store.LaneForge{LaneID: oneLane(t, db), MR: 7, MRState: "opened", MRCreated: at, MRUpdated: at.Add(time.Hour), PipelineStarted: at.Add(time.Minute)}
	if err := db.PutLaneForge(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := db.LaneForge(ctx, in.LaneID)
	if err != nil || got != in {
		t.Fatalf("got %+v, %v; want %+v", got, err, in)
	}
	if !got.PipelineFinished.IsZero() {
		t.Errorf("unset time came back as %v", got.PipelineFinished)
	}
}

// A database from before the timestamps gains the columns, and its rows read as unknown.
func TestLaneForgeTimesMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	lane := oneLane(t, db)
	if err := db.PutLaneForge(ctx, store.LaneForge{LaneID: lane, MR: 3, MRState: "opened"}); err != nil {
		t.Fatal(err)
	}
	// Step the schema back over the forge-times migration, which is the fifth to last
	// (then repos.removed, next_items, gate_results and keep_going), as a daemon one release old left it.
	last := len(migrations) - 5
	for _, tbl := range []string{"keep_going", "keep_going_starts"} {
		if _, err := db.db.ExecContext(ctx, `DROP TABLE `+tbl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE gate_results`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE next_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE repos DROP COLUMN removed`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"mr_created", "mr_updated", "pipeline_started", "pipeline_finished"} {
		if _, err := db.db.ExecContext(ctx, `ALTER TABLE lane_forge DROP COLUMN `+c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, last)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	f, err := db.LaneForge(ctx, lane)
	if err != nil || f.MR != 3 || !f.MRUpdated.IsZero() {
		t.Fatalf("after migrate: %+v, %v", f, err)
	}
}

func TestLaneActivity(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	ws, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "a", Path: "/w/a"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := db.Repos(ctx, ws.ID)
	mk := func(name, state string) store.Lane {
		l, err := db.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: name, Branch: name, Worktree: "/w/" + name, State: state})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	a, b, quiet, closed := mk("a", store.LaneOpen), mk("b", store.LaneOpen), mk("quiet", store.LaneOpen), mk("closed", store.LaneClosed)
	pause := func() { time.Sleep(3 * time.Millisecond) }
	run := func(l store.Lane) store.Run {
		r, err := db.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Log: "/l", State: store.RunRunning})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	want := func(lane store.Lane, by string) {
		t.Helper()
		got, err := db.LaneActivity(ctx, repos[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if got[lane.ID].By != by || got[lane.ID].At.IsZero() {
			t.Errorf("lane %s activity = %+v, want by %s", lane.Name, got[lane.ID], by)
		}
	}

	ra := run(a)
	want(a, store.ActivityRun)
	pause()
	end := time.Now().UTC()
	ra.State, ra.Ended = store.RunSucceeded, &end
	if err := db.UpdateRun(ctx, ra); err != nil {
		t.Fatal(err)
	}
	got, _ := db.LaneActivity(ctx, repos[0].ID)
	if !got[a.ID].At.Equal(end) {
		t.Errorf("run end not the activity: %v vs %v", got[a.ID].At, end)
	}
	pause()
	d, err := db.CreateDecision(ctx, store.Decision{RunID: ra.ID, Question: "q?", State: store.DecisionOpen})
	if err != nil {
		t.Fatal(err)
	}
	want(a, store.ActivityDecision)
	pause()
	rb := run(b) // later than a's decision, so b is the run-active lane
	want(b, store.ActivityRun)
	pause()
	if _, err := db.AnswerDecision(ctx, d.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	want(a, store.ActivityDecision)
	got, _ = db.LaneActivity(ctx, repos[0].ID)
	if !got[a.ID].At.After(got[b.ID].At) {
		t.Errorf("answer %v not after b's run %v", got[a.ID].At, got[b.ID].At)
	}
	pause()
	q, err := db.CreateRequest(ctx, store.Request{FromRun: ra.ID, Kind: "question", Message: "m", State: "open", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	want(a, store.ActivityRequest)
	pause()
	q.TargetRun = rb.ID
	if err := db.UpdateRequest(ctx, q); err != nil {
		t.Fatal(err)
	}
	want(b, store.ActivityRequest) // the lane it was routed to moved too

	got, _ = db.LaneActivity(ctx, repos[0].ID)
	if _, ok := got[quiet.ID]; ok {
		t.Error("a lane with nothing has activity")
	}
	if _, ok := got[closed.ID]; ok {
		t.Error("a closed lane has activity")
	}
}
