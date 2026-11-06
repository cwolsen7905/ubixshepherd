package sqlite

import (
	"context"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestDeskTurnLogKeepsTurnsAfterTheConversationIsTrimmed(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	three := 3
	for turn, l := range []store.DeskTurnLog{
		{Turn: 1, StartedBy: "event", Topic: "release", Waiting: &three, Events: []store.DeskLogEvent{{Kind: "run_failed", Repo: "app", Lane: "a", Run: 4}}},
		{Turn: 2},
	} {
		l.WorkspaceID = 1
		if err := db.SaveDeskTurnLog(ctx, l); err != nil {
			t.Fatal(turn, err)
		}
	}
	// A second save of a turn replaces the first.
	if err := db.SaveDeskTurnLog(ctx, store.DeskTurnLog{WorkspaceID: 1, Turn: 2, Topic: "later"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		db.AddDeskEvent(ctx, store.DeskEvent{WorkspaceID: 1, Kind: store.DeskUser, Text: "x"})
	}
	if err := db.TrimDeskEvents(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	got, err := db.DeskTurnLogs(ctx, 1)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	if got[0].Topic != "release" || got[0].StartedBy != "event" || got[0].Waiting == nil || *got[0].Waiting != 3 || len(got[0].Events) != 1 || got[0].Events[0].Run != 4 {
		t.Fatalf("turn 1: %+v", got[0])
	}
	if got[1].Topic != "later" || got[1].Waiting != nil || got[1].Created.IsZero() {
		t.Fatalf("turn 2: %+v", got[1])
	}
	if other, _ := db.DeskTurnLogs(ctx, 2); len(other) != 0 {
		t.Fatalf("another workspace's: %+v", other)
	}
}
