package desk

import (
	"context"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A wake-up turn keeps what it started with: the events with the repo, lane and run each
// was about, the topic, and the waiting count. The row outlives the conversation's trim.
func TestTurnLogKeepsEventsAtTurnStart(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, func(o *Options) { o.Grace = time.Millisecond; o.Keep = 3 })
	if _, err := r.st.SaveWorkspace(ctx, r.ws, []store.Repo{{Name: "app", Path: "/w/app"}}); err != nil {
		t.Fatal(err)
	}
	repos, err := r.st.Repos(ctx, r.ws.ID)
	if err != nil || len(repos) != 1 {
		t.Fatalf("%v %v", err, repos)
	}
	lane, err := r.st.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "feat/x", Branch: "feat/x", Worktree: "/w/x", State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.st.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "p", State: store.RunFailed})
	if err != nil {
		t.Fatal(err)
	}
	d := r.desk(t)
	r.m.Wake([]store.FeedItem{{Kind: store.FeedRunFailed, Text: "run failed", Ref: run.ID}})
	sub := d.Subscribe()
	defer d.Unsubscribe(sub)
	es := r.waitEnded(t, 1)
	logs, err := r.st.DeskTurnLogs(ctx, r.ws.ID)
	if err != nil || len(logs) != 1 || logs[0].Turn != es[0].Turn {
		t.Fatalf("%v %+v", err, logs)
	}
	want := store.DeskLogEvent{Kind: store.FeedRunFailed, Repo: "app", Lane: "feat/x", Run: run.ID}
	if len(logs[0].Events) != 1 || logs[0].Events[0] != want {
		t.Fatalf("events: %+v", logs[0].Events)
	}
}
