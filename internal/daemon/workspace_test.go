package daemon

import (
	"context"
	"net/http"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestRenameWorkspace(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	other, _ := paths.Canonical(t.TempDir())
	a, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "a", Path: root}, []store.Repo{{Name: "app", Path: root + "/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "b", Path: other}, nil); err != nil {
		t.Fatal(err)
	}
	path := api.PathWorkspaceRename(a.ID)

	var got api.WorkspaceDetail
	if code := call(t, ts, s.Token, "POST", path, api.RenameWorkspace{Name: "c"}, &got); code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
	if got.Name != "c" || got.Path != root || len(got.Repos) != 1 {
		t.Errorf("detail = %+v", got)
	}
	for _, c := range []struct {
		path string
		name string
		want int
	}{
		{path, "b", http.StatusConflict},
		{path, "", http.StatusBadRequest},
		{path, "x/y", http.StatusBadRequest},
		{api.PathWorkspaceRename(999), "z", http.StatusNotFound},
		{api.PathWorkspaces + "/abc/rename", "z", http.StatusBadRequest},
	} {
		if code := call(t, ts, s.Token, "POST", c.path, api.RenameWorkspace{Name: c.name}, nil); code != c.want {
			t.Errorf("%s name %q: %d, want %d", c.path, c.name, code, c.want)
		}
	}
	// A desk or a worker never renames a workspace.
	desk, _ := s.MintDesk(false, "")
	if code := call(t, ts, desk, "POST", path, api.RenameWorkspace{Name: "d"}, nil); code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Errorf("desk rename: %d", code)
	}
	wss, _ := s.Store.Workspaces(ctx)
	if len(wss) != 2 || wss[1].Name != "c" && wss[0].Name != "c" {
		t.Errorf("workspaces = %+v", wss)
	}
}
