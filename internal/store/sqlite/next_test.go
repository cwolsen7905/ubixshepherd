package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestNextItemsOrderAndMove(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	add := func(title, state string) store.NextItem {
		it, err := db.AddNextItem(ctx, store.NextItem{Title: title, State: state})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	p1 := add("p1", "")
	d1 := add("d1", store.NextDeferred)
	a1 := add("a1", store.NextApproved)
	p2 := add("p2", store.NextPlanned)
	a2 := add("a2", store.NextApproved)
	titles := func() string {
		items, _ := db.NextItems(ctx)
		s := ""
		for _, it := range items {
			s += it.Title + " "
		}
		return s
	}
	if got := titles(); got != "a1 a2 p1 p2 d1 " {
		t.Fatalf("order: %s", got)
	}
	db.MoveNextItem(ctx, a2.ID, "up")
	if got := titles(); got != "a2 a1 p1 p2 d1 " {
		t.Errorf("up: %s", got)
	}
	db.MoveNextItem(ctx, p2.ID, "top")
	db.MoveNextItem(ctx, p2.ID, "down")
	if got := titles(); got != "a2 a1 p1 p2 d1 " {
		t.Errorf("top then down: %s", got)
	}
	// Moving the first up, or the last down, is a no-op; a bad direction is refused.
	db.MoveNextItem(ctx, a2.ID, "up")
	if _, err := db.MoveNextItem(ctx, a2.ID, "sideways"); err == nil {
		t.Error("sideways accepted")
	}
	// A state change goes to the bottom of the new state.
	approved := store.NextApproved
	if it, err := db.UpdateNextItem(ctx, p1.ID, store.NextPatch{State: &approved}); err != nil || it.Rank <= a1.Rank {
		t.Errorf("approve: %v %+v", err, it)
	}
	if got := titles(); got != "a2 a1 p1 p2 d1 " {
		t.Errorf("after approve: %s", got)
	}
	done := store.NextDone
	it, err := db.UpdateNextItem(ctx, d1.ID, store.NextPatch{State: &done})
	if err != nil || it.DoneAt == nil {
		t.Errorf("done: %v %+v", err, it)
	}
	planned := store.NextPlanned
	if it, _ := db.UpdateNextItem(ctx, d1.ID, store.NextPatch{State: &planned}); it.DoneAt != nil {
		t.Errorf("reopened item keeps done_at: %+v", it)
	}
	if _, err := db.NextItem(ctx, 999); err != store.ErrNotFound {
		t.Errorf("unknown: %v", err)
	}
	if _, err := db.AddNextItem(ctx, store.NextItem{Title: " "}); err == nil {
		t.Error("empty title accepted")
	}
}

func TestGateResultsAndBadge(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if g, err := db.LastGateResult(ctx, 1); err != nil || g != nil {
		t.Fatalf("none yet: %v %v", g, err)
	}
	// lanes(id) is a foreign key: open one through a workspace.
	w, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: t.TempDir()}, []store.Repo{{Name: "app", Path: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := db.Repos(ctx, w.ID)
	lane, err := db.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "feat/x", Branch: "feat/x", Base: "main", Worktree: t.TempDir(), State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	db.AddGateResult(ctx, store.GateResult{LaneID: lane.ID, SHA: "aaa", Gate: "make check", Passed: true})
	db.AddGateResult(ctx, store.GateResult{LaneID: lane.ID, SHA: "bbb", Gate: "make check", Passed: false})
	db.AddGateResult(ctx, store.GateResult{LaneID: lane.ID, SHA: "aaa", Gate: "make check", Passed: false})
	if g, _ := db.GateResultFor(ctx, lane.ID, "aaa"); g == nil || g.Passed || g.Source != store.GateSourceShip || g.At.IsZero() {
		t.Errorf("newest for aaa = %+v", g)
	}
	if g, _ := db.GateResultFor(ctx, lane.ID, "ccc"); g != nil {
		t.Errorf("for ccc = %+v", g)
	}
	if g, _ := db.LastGateResult(ctx, lane.ID); g == nil || g.SHA != "aaa" {
		t.Errorf("last = %+v", g)
	}
}
