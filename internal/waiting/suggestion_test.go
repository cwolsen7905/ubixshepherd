package waiting

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func TestCollectNextSuggestions(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	ws, _ := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app")}})
	repos, _ := st.Repos(ctx, ws.ID)
	suggest := func(it store.NextItem, line string) store.NextItem {
		it, err := st.UpdateNextItem(ctx, it.ID, store.NextPatch{Suggestion: &line})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	add := func(title, state string) store.NextItem {
		it, err := st.AddNextItem(ctx, store.NextItem{Title: title, State: state, Repo: "app"})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}

	plain := add("approved, nothing to say", store.NextApproved)
	asked := suggest(add("approved with a suggestion", store.NextApproved), "done?: ALREADY DONE: landed in abc123")
	both := suggest(add("deferred with a suggestion", store.NextDeferred), "done or drop?: closed a week ago")
	// Finishing or dropping answers a suggestion, so the store clears it.
	fin := suggest(add("done", store.NextApproved), "done?")
	doneState := store.NextDone
	st.UpdateNextItem(ctx, fin.ID, store.NextPatch{State: &doneState})
	gone := add("dropped", store.NextApproved)
	suggest(gone, "done?")
	dropped := store.NextDropped
	st.UpdateNextItem(ctx, gone.ID, store.NextPatch{State: &dropped})
	// A suggestion on an item whose lane merged is left to settle as done.
	l := lane(t, st, repos[0], "merged", 0)
	mergedItem := suggest(add("lane merged", store.NextApproved), "done?")
	id := l.ID
	st.UpdateNextItem(ctx, mergedItem.ID, store.NextPatch{LaneID: &id})
	if err := st.SetLaneState(ctx, l.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	if err := st.PutLaneForge(ctx, store.LaneForge{LaneID: l.ID, MR: 5, MRState: "merged"}); err != nil {
		t.Fatal(err)
	}

	out, err := Collect(ctx, st, config.Default(), 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]api.WaitingItem{}
	for _, it := range out.Items {
		if it.Kind == api.WaitingNext {
			got[it.NextID] = it
		}
	}
	if len(got) != 2 || out.Counts.Next != 2 {
		t.Fatalf("want the two suggested items, got %+v (count %d)", got, out.Counts.Next)
	}
	if _, ok := got[plain.ID]; ok {
		t.Error("an approved item with no suggestion waits on no one")
	}
	for _, it := range []store.NextItem{asked, both} {
		w, ok := got[it.ID]
		if !ok || w.Action != "decide" || w.Detail != it.Suggestion || w.Since.IsZero() {
			t.Errorf("item %d: %+v", it.ID, w)
		}
	}
}
