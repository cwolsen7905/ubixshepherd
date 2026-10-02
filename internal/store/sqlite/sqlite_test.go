package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func open(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shepherd.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestSaveWorkspaceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)

	repos := []store.Repo{
		{Name: "alpha", Path: "/w/alpha", Remote: "git@example.com:g/alpha.git", Stacks: []string{"go"}},
		{Name: "beta", Path: "/w/beta"},
	}
	ws, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, repos)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID == 0 || ws.Created.IsZero() {
		t.Errorf("workspace not filled in: %+v", ws)
	}

	// Running init again with one more repo and a new name keeps the id and the old repos.
	again, err := db.SaveWorkspace(ctx, store.Workspace{Name: "git", Path: "/w"},
		[]store.Repo{{Name: "gamma", Path: "/w/gamma", Stacks: []string{"node", "php"}}})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != ws.ID || again.Name != "git" {
		t.Errorf("second save = %+v, first = %+v", again, ws)
	}

	got, err := db.Repos(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("repos = %+v", got)
	}
	if got[0].Name != "alpha" || got[0].Stacks[0] != "go" || got[0].Remote == "" {
		t.Errorf("alpha = %+v", got[0])
	}
	if got[1].Stacks == nil {
		t.Error("stacks should decode to an empty slice, not nil")
	}

	all, err := db.Workspaces(ctx)
	if err != nil || len(all) != 1 {
		t.Errorf("workspaces = %+v, %v", all, err)
	}
}

func TestReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	db, path := open(t)
	if _, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	ws, err := db2.Workspaces(ctx)
	if err != nil || len(ws) != 1 {
		t.Errorf("after reopen: %+v, %v", ws, err)
	}
}

func TestLanesEmpty(t *testing.T) {
	db, _ := open(t)
	l, err := db.Lanes(context.Background(), 1)
	if err != nil || len(l) != 0 {
		t.Errorf("lanes = %+v, %v", l, err)
	}
}
