package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestRenameWorkspace(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := db.SaveWorkspace(ctx, store.Workspace{Name: "a", Path: "/a"}, []store.Repo{{Name: "app", Path: "/a/app"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.SaveWorkspace(ctx, store.Workspace{Name: "b", Path: "/b"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.RenameWorkspace(ctx, a.ID, " c ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "c" || got.ID != a.ID || got.Path != "/a" || !got.Created.Equal(a.Created) {
		t.Errorf("renamed = %+v, was %+v", got, a)
	}
	if repos, _ := db.Repos(ctx, a.ID); len(repos) != 1 {
		t.Errorf("repos after rename = %v", repos)
	}
	// Renaming to its own name is fine.
	if _, err := db.RenameWorkspace(ctx, a.ID, "c"); err != nil {
		t.Errorf("same name: %v", err)
	}
	if _, err := db.RenameWorkspace(ctx, a.ID, "b"); !errors.Is(err, store.ErrConflict) {
		t.Errorf("taken name: %v", err)
	}
	if _, err := db.RenameWorkspace(ctx, a.ID, "  "); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := db.RenameWorkspace(ctx, 999, "z"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	wss, _ := db.Workspaces(ctx)
	if len(wss) != 2 || wss[0].Name != "b" || wss[1].Name != "c" || wss[0].ID != b.ID {
		t.Errorf("workspaces = %+v", wss)
	}
}
