package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func TestLaneFilesEndpoint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	origin := filepath.Join(root, "origin.git")
	gitA(t, root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	gitA(t, root, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("# hi\n"), 0o644)
	os.MkdirAll(filepath.Join(seed, "docs"), 0o755)
	os.WriteFile(filepath.Join(seed, "docs", "plan.md"), []byte("# plan\n"), 0o644)
	gitA(t, seed, "add", ".")
	gitA(t, seed, "commit", "-q", "-m", "init")
	gitA(t, seed, "push", "-q", "origin", "HEAD:main")

	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	app := filepath.Join(ws, "app")
	gitA(t, ws, "clone", "-q", origin, app)

	st, err := sqlite.Open(ctx, filepath.Join(root, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w, err := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: ws}, []store.Repo{{Name: "app", Path: app}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(ctx, w.ID)
	f := &fold.Fold{Store: st, Config: config.Default()}
	l, err := f.Open(ctx, fold.OpenRequest{RepoID: repos[0].ID, Name: "feat/x", Scope: []string{"docs/**"}})
	if err != nil {
		t.Fatal(err)
	}
	// Add an untracked file in the worktree root.
	os.WriteFile(filepath.Join(l.Worktree, "todo.txt"), []byte("do this\n"), 0o644)

	mux := http.NewServeMux()
	mux.Handle("GET "+PathLanes+"/{id}/files", LaneFiles(f))
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}

	// Root lists children.
	rec := get(PathLaneFiles(l.ID, ""))
	if rec.Code != 200 {
		t.Fatalf("root list = %d %q", rec.Code, rec.Body)
	}
	var rootNode LaneFileNode
	if err := json.Unmarshal(rec.Body.Bytes(), &rootNode); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	if rootNode.Kind != "dir" {
		t.Fatalf("root kind = %q", rootNode.Kind)
	}
	names := map[string]string{}
	for _, c := range rootNode.Children {
		names[c.Name] = c.Kind
	}
	if names["README.md"] != "file" || names["docs"] != "dir" || names["todo.txt"] != "file" {
		t.Fatalf("children = %v", names)
	}

	// List a subdirectory.
	rec = get(PathLaneFiles(l.ID, "path=docs"))
	if rec.Code != 200 {
		t.Fatalf("docs list = %d %q", rec.Code, rec.Body)
	}
	var docsNode LaneFileNode
	if err := json.Unmarshal(rec.Body.Bytes(), &docsNode); err != nil {
		t.Fatalf("unmarshal docs: %v", err)
	}
	if docsNode.Kind != "dir" || len(docsNode.Children) != 1 || docsNode.Children[0].Name != "plan.md" {
		t.Fatalf("docs children = %+v", docsNode.Children)
	}

	// Read a file from the worktree.
	rec = get(PathLaneFiles(l.ID, "path=docs/plan.md"))
	if rec.Code != 200 {
		t.Fatalf("worktree file = %d %q", rec.Code, rec.Body)
	}
	var fileNode LaneFileNode
	if err := json.Unmarshal(rec.Body.Bytes(), &fileNode); err != nil {
		t.Fatalf("unmarshal file: %v", err)
	}
	if fileNode.Kind != "file" || !strings.Contains(fileNode.Data, "# plan") {
		t.Fatalf("worktree file node = %+v", fileNode)
	}

	// Read a file from a ref.
	rec = get(PathLaneFiles(l.ID, "path=docs/plan.md&ref=HEAD"))
	if rec.Code != 200 {
		t.Fatalf("ref file = %d %q", rec.Code, rec.Body)
	}
	var refNode LaneFileNode
	if err := json.Unmarshal(rec.Body.Bytes(), &refNode); err != nil {
		t.Fatalf("unmarshal ref file: %v", err)
	}
	if refNode.Kind != "file" || !strings.Contains(refNode.Data, "# plan") {
		t.Fatalf("ref file node = %+v", refNode)
	}

	// Refuse paths outside the repo.
	for _, bad := range []string{"path=../README.md", "path=/etc/passwd"} {
		if rec := get(PathLaneFiles(l.ID, bad)); rec.Code != http.StatusConflict && rec.Code != http.StatusBadRequest {
			t.Errorf("bad path %q: %d", bad, rec.Code)
		}
	}

	// Unknown lane.
	if rec := get(PathLaneFiles(9999, "")); rec.Code != http.StatusNotFound {
		t.Errorf("unknown lane = %d", rec.Code)
	}
}