package waiting

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func TestTidyListsOpenFindingsAndHidesTheDuplicateNextItem(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	ws, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app")}})
	repos, _ := st.Repos(ctx, ws.ID)
	l := lane(t, st, repos[0], "closed-lane", 0)
	st.SetLaneState(ctx, l.ID, store.LaneClosed)
	it, _ := st.AddNextItem(ctx, store.NextItem{Title: "work", State: store.NextApproved, Repo: "app"})
	id := l.ID
	st.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id})

	before, err := Collect(ctx, st, config.Default(), 0)
	if err != nil || before.Counts.Next != 1 {
		t.Fatalf("before: %d next, %v", before.Counts.Next, err)
	}

	st.AddMuster(ctx, store.MusterItem{Key: "a", Kind: "next.lane-closed", SubjectKind: "next", SubjectID: it.ID, Reason: "closed"})
	done, _, _ := st.AddMuster(ctx, store.MusterItem{Key: "b", Kind: "lane.stale", SubjectKind: "lane", SubjectID: l.ID})
	applied := store.MusterApplied
	st.UpdateMuster(ctx, done.ID, store.MusterPatch{State: &applied})

	tidy, err := Tidy(ctx, st)
	if err != nil || len(tidy) != 1 || tidy[0].Key != "a" {
		t.Fatalf("tidy = %+v, %v", tidy, err)
	}
	after, err := Collect(ctx, st, config.Default(), 0)
	if err != nil || after.Counts.Next != 0 {
		t.Errorf("the item shows twice: %d next, %v", after.Counts.Next, err)
	}
}
