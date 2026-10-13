package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The next-up list: an item is the person's to approve, and its state follows its lane.
func TestNextItemsApprovalAndDerivedState(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	run, _, la, lb := twoRuns(t, s)

	var v api.NextItemView
	if code := call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "  Lane diff step 2 glpat-AbCdEfGhIjKlMnOpQrStUv ", Plan: "docs/lane-diff.md"}, &v); code != http.StatusOK ||
		v.State != store.NextPlanned || strings.Contains(v.Title, "glpat-") || v.Source != store.NextByPerson {
		t.Fatalf("add: %d %+v", code, v)
	}
	if code := call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "x", Plan: "../etc/passwd"}, nil); code != http.StatusBadRequest {
		t.Errorf("plan outside the repo: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "   "}, nil); code != http.StatusBadRequest {
		t.Errorf("empty title: %d", code)
	}

	// The desk may add, but not approve or reorder, outside a turn the person started.
	system, _ := s.MintDesk(false, "")
	var proposed api.NextItemView
	if code := call(t, ts, system, "POST", api.PathNext, api.NextAdd{Title: "Idea from chat"}, &proposed); code != http.StatusOK || proposed.Source != store.NextByDesk || !proposed.Proposed {
		t.Fatalf("desk add: %d %+v", code, proposed)
	}
	if code := call(t, ts, system, "POST", api.PathNext, api.NextAdd{Title: "Sneaky", State: store.NextApproved}, nil); code != http.StatusForbidden {
		t.Errorf("desk added an approved item: %d", code)
	}
	approved := store.NextApproved
	if code := call(t, ts, system, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{State: &approved}, nil); code != http.StatusForbidden {
		t.Errorf("desk approved outside a human turn: %d", code)
	}
	if code := call(t, ts, system, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{Move: "top"}, nil); code != http.StatusForbidden {
		t.Errorf("desk moved outside a human turn: %d", code)
	}
	note := "from the chat"
	if code := call(t, ts, system, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{Note: &note}, nil); code != http.StatusOK {
		t.Errorf("desk note: %d", code)
	}
	// An agent's token reaches none of it.
	worker, _ := s.MintWorker(run.ID)
	if code := call(t, ts, worker, "GET", api.PathNext, nil, nil); !denied(code) {
		t.Errorf("worker read the list: %d", code)
	}

	human, _ := s.MintDesk(true, "")
	if code := call(t, ts, human, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{State: &approved}, &v); code != http.StatusOK || v.State != store.NextApproved || v.Proposed {
		t.Fatalf("approve in a human turn: %d %+v", code, v)
	}
	if code := call(t, ts, s.Token, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{State: ptr("started")}, nil); code != http.StatusBadRequest {
		t.Errorf("started is derived, not set: %d", code)
	}

	// The approved item is next; linking it to an open lane makes it started.
	var l api.NextList
	call(t, ts, s.Token, "GET", api.PathNext, nil, &l)
	if l.NextUp != proposed.ID || len(l.Items) != 2 || l.Items[0].ID != proposed.ID {
		t.Fatalf("list order: %+v", l)
	}
	if code := call(t, ts, s.Token, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{LaneID: &la.ID}, &v); code != http.StatusOK || v.State != store.NextStarted || v.LaneName != "a" || v.Stored != store.NextApproved {
		t.Fatalf("link: %d %+v", code, v)
	}
	l = api.NextList{}
	call(t, ts, s.Token, "GET", api.PathNext, nil, &l)
	if l.NextUp != 0 {
		t.Errorf("an item with a lane is not next: %+v", l)
	}

	// Closed without a merge: back to the chosen state, with the reason.
	if err := s.Store.SetLaneState(ctx, la.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	call(t, ts, s.Token, "GET", api.PathNextItem(proposed.ID), nil, &v)
	if v.State != store.NextApproved || !strings.Contains(v.Why, "closed without a merge") {
		t.Errorf("closed lane, no merge: %+v", v)
	}

	// Linked to a lane whose request merged: done, kept and dated.
	if code := call(t, ts, s.Token, "PATCH", api.PathNextItem(proposed.ID), api.NextUpdate{LaneID: &lb.ID}, nil); code != http.StatusOK {
		t.Fatal(code)
	}
	s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: lb.ID, MR: 7, MRState: "merged"})
	s.Store.SetLaneState(ctx, lb.ID, store.LaneClosed)
	call(t, ts, s.Token, "GET", api.PathNextItem(proposed.ID), nil, &v)
	if v.State != store.NextDone || v.DoneAt == nil || v.MR != "!7" {
		t.Errorf("merged lane: %+v", v)
	}
	it, _ := s.Store.NextItem(ctx, proposed.ID)
	if it.State != store.NextDone || it.DoneAt == nil {
		t.Errorf("done was not kept: %+v", it)
	}
	feed, _ := s.Store.Feed(ctx, 0, 200)
	told := 0
	for _, f := range feed {
		if f.Kind == store.FeedPlan && f.Ref == proposed.ID && strings.Contains(f.Text, "done") {
			told++
		}
	}
	if told != 1 {
		t.Errorf("a merged lane's item should be told done once on the feed, got %d", told)
	}
	call(t, ts, s.Token, "GET", api.PathNext, nil, &l)
	for _, x := range l.Items {
		if x.ID == proposed.ID {
			t.Error("a done item is listed without all=1")
		}
	}
	call(t, ts, s.Token, "GET", api.PathNext+"?all=1", nil, &l)
	if len(l.Items) != 2 {
		t.Errorf("all=1: %+v", l)
	}
}

func ptr[T any](v T) *T { return &v }
