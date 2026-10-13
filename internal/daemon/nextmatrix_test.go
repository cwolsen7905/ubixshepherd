package daemon

import (
	"context"
	"net/http"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

const matrixFile = `## Work

| ID | Area | Item | Status | Lane | PR | Notes |
|----|------|------|--------|------|----|-------|
| W1 | web | Doing now | doing | live | #7 | |
| W2 | core | Typed work orders | todo | - | - | needs design |
| W3 | core | Waits on the forge | blocked | - | #9 | |
| W4 | core | Has a stored item | todo | feat/stored | - | |
| W5 | web | Shipped | done | - | - | |
`

// Matrix rows with status todo or blocked read through into the list: read only, from
// the base ref, hidden when a stored item names their lane, and not waiting on anyone.
func TestNextListReadsMatrixRowsThrough(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	app := workRepo(t, root, "app", matrixFile)
	if _, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root},
		[]store.Repo{{Name: "app", Path: app, Remote: "git@github.com:example/app.git"}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {BaseBranch: "dev"}}
	s.applyConfig(cfg)

	var l api.NextList
	if code := call(t, ts, s.Token, "GET", api.PathNext, nil, &l); code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	got := map[string]api.NextItemView{}
	for _, v := range l.Matrix {
		got[v.RowID] = v
	}
	if len(l.Matrix) != 3 || got["W2"].State != store.NextPlanned || got["W3"].State != "blocked" || got["W4"].RowID == "" {
		t.Fatalf("matrix rows: %+v", l.Matrix)
	}
	w3 := got["W3"]
	if w3.ID != 0 || !w3.ReadOnly || w3.Source != api.NextSourceMatrix || w3.Key != "matrix:app:W3" ||
		w3.PRURL != "https://github.com/example/app/pull/9" || w3.Area != "core" || w3.Repo != "app" {
		t.Errorf("row view: %+v", w3)
	}
	if len(l.Items) != 0 || l.NextUp != 0 {
		t.Errorf("rows must not enter items or next_up: %+v", l)
	}

	// A stored item that hints W4's lane in the same repo shows the work once.
	var it api.NextItemView
	if code := call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "Stored", Repo: "app", Lane: "feat/stored", State: store.NextApproved}, &it); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	l = api.NextList{}
	call(t, ts, s.Token, "GET", api.PathNext, nil, &l)
	if len(l.Matrix) != 2 || len(l.Items) != 1 {
		t.Errorf("after the stored item: %d rows, %d items", len(l.Matrix), len(l.Items))
	}
	l = api.NextList{}
	call(t, ts, s.Token, "GET", api.PathNext+"?matrix=0", nil, &l)
	if len(l.Matrix) != 0 {
		t.Errorf("matrix=0 still listed %d rows", len(l.Matrix))
	}

	// Rows are not proposals: nothing waits on the person for them.
	var w api.Waiting
	if code := call(t, ts, s.Token, "GET", "/v1/waiting", nil, &w); code != http.StatusOK || w.Counts.Next != 0 {
		t.Errorf("waiting: %d %+v", code, w.Counts)
	}
}

// The plan kind: an item added, approved, deferred, dropped or done is told once on the
// feed, and a rank move or note edit is not.
func TestNextWritesPlanFeedItems(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	var a, b api.NextItemView
	call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "First"}, &a)
	call(t, ts, s.Token, "POST", api.PathNext, api.NextAdd{Title: "Second"}, &b)
	approved, note := store.NextApproved, "why"
	call(t, ts, s.Token, "PATCH", api.PathNextItem(a.ID), api.NextUpdate{State: &approved}, nil)
	call(t, ts, s.Token, "PATCH", api.PathNextItem(a.ID), api.NextUpdate{State: &approved}, nil) // no change
	call(t, ts, s.Token, "PATCH", api.PathNextItem(b.ID), api.NextUpdate{Note: &note, Move: "top"}, nil)
	done := store.NextDone
	call(t, ts, s.Token, "PATCH", api.PathNextItem(a.ID), api.NextUpdate{State: &done}, nil)

	items, err := s.Store.Feed(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var plans []store.FeedItem
	for _, f := range items {
		if f.Kind == store.FeedPlan {
			plans = append(plans, f)
		}
	}
	if len(plans) != 4 { // two adds, one approval, one done
		t.Fatalf("plan items: %d %+v", len(plans), plans)
	}
	if plans[2].Ref != a.ID || plans[3].Ref != a.ID {
		t.Errorf("refs: %+v", plans)
	}
}
