package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestKeepGoingRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	ws, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	k, err := db.KeepGoing(ctx, ws.ID)
	if err != nil || k.On || k.WorkspaceID != ws.ID {
		t.Fatalf("a workspace with no row is off: %+v, %v", k, err)
	}
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	want := store.KeepGoing{WorkspaceID: ws.ID, On: true, Since: at, By: store.KeepByCLI, SpentAtStart: 1.25,
		LastAction: at.Add(time.Hour), Cap: 3, MaxRuns: 1, IdleHours: 2}
	if err := db.SetKeepGoing(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := db.KeepGoing(ctx, ws.ID)
	if err != nil || !got.On || !got.Since.Equal(at) || got.SpentAtStart != 1.25 || got.Cap != 3 || got.MaxRuns != 1 || !got.LastAction.Equal(at.Add(time.Hour)) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	want.On, want.ReasonOff, want.OffAt = false, store.KeepOffIdle, at.Add(4*time.Hour)
	if err := db.SetKeepGoing(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, _ = db.KeepGoing(ctx, ws.ID)
	if got.On || got.ReasonOff != store.KeepOffIdle || !got.OffAt.Equal(want.OffAt) {
		t.Fatalf("after off: %+v", got)
	}
}

func TestLaneStarts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	if n, _ := db.LaneStarts(ctx, 7, "2026-10-11"); n != 0 {
		t.Fatalf("none yet: %d", n)
	}
	for want := 1; want <= 3; want++ {
		if n, err := db.CountLaneStart(ctx, 7, "2026-10-11"); err != nil || n != want {
			t.Fatalf("count %d: got %d, %v", want, n, err)
		}
	}
	if n, _ := db.CountLaneStart(ctx, 7, "2026-10-12"); n != 1 {
		t.Fatalf("a new day starts again: %d", n)
	}
	if n, _ := db.LaneStarts(ctx, 8, "2026-10-11"); n != 0 {
		t.Fatalf("lanes are separate: %d", n)
	}
}
