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

// filesFixture is a managed repo with a lane and the files route on a mux.
func filesFixture(t *testing.T) (fold.Opened, func(string) *httptest.ResponseRecorder, *http.ServeMux) {
	t.Helper()
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
	return l, func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}, mux
}

func TestLaneFilesEndpoint(t *testing.T) {
	l, get, _ := filesFixture(t)

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
func TestLaneFilesRefsAndLimits(t *testing.T) {
	l, get, mux := filesFixture(t)
	decode := func(rec *httptest.ResponseRecorder) LaneFileNode {
		t.Helper()
		if rec.Code != 200 {
			t.Fatalf("status = %d %q", rec.Code, rec.Body)
		}
		var n LaneFileNode
		if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// A ref lists its root and a subdirectory, and reads nothing from the worktree.
	root := decode(get(PathLaneFiles(l.ID, "ref=HEAD")))
	names := map[string]string{}
	for _, c := range root.Children {
		names[c.Name] = c.Kind
	}
	if names["README.md"] != "file" || names["docs"] != "dir" || names["todo.txt"] != "" {
		t.Fatalf("ref root children = %v", names)
	}
	docs := decode(get(PathLaneFiles(l.ID, "path=docs&ref=HEAD")))
	if len(docs.Children) != 1 || docs.Children[0].Path != "docs/plan.md" || docs.Children[0].Size == 0 {
		t.Fatalf("ref docs children = %+v", docs.Children)
	}

	// The .git directory is not listed.
	for _, c := range decode(get(PathLaneFiles(l.ID, ""))).Children {
		if c.Name == ".git" {
			t.Fatal(".git listed")
		}
	}

	// Binary and oversized files are listed but their bytes are not sent.
	os.WriteFile(filepath.Join(l.Worktree, "blob.bin"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644)
	os.WriteFile(filepath.Join(l.Worktree, "big.txt"), []byte(strings.Repeat("a", MaxFileBytes+1)), 0o644)
	for _, name := range []string{"blob.bin", "big.txt"} {
		n := decode(get(PathLaneFiles(l.ID, "path="+name)))
		if !n.Binary || n.Data != "" {
			t.Errorf("%s = %+v, want binary with no data", name, n)
		}
	}

	// A secret in a file is masked.
	os.WriteFile(filepath.Join(l.Worktree, "env.txt"), []byte("api_key=abcdef123456\n"), 0o644)
	n := decode(get(PathLaneFiles(l.ID, "path=env.txt")))
	if strings.Contains(n.Data, "abcdef123456") || n.Redacted == 0 {
		t.Errorf("secret not masked: %+v", n)
	}

	// A symlink out of the worktree is neither listed nor followed.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("outside\n"), 0o644)
	if err := os.Symlink(outside, filepath.Join(l.Worktree, "link.txt")); err == nil {
		for _, c := range decode(get(PathLaneFiles(l.ID, ""))).Children {
			if c.Name == "link.txt" {
				t.Error("symlink listed")
			}
		}
		if rec := get(PathLaneFiles(l.ID, "path=link.txt")); rec.Code != http.StatusNotFound {
			t.Errorf("symlink read = %d", rec.Code)
		}
	}

	// Paths and refs that leave the repo or look like options are refused.
	for _, bad := range []string{"path=docs/../../x", "path=%2e%2e%2fx", "ref=--output%3Dx", "ref=a..b", "ref=HEAD@%7B1%7D"} {
		if rec := get(PathLaneFiles(l.ID, bad)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, rec.Code)
		}
	}
	if rec := get(PathLaneFiles(l.ID, "ref=nope")); rec.Code != http.StatusConflict {
		t.Errorf("unknown ref = %d, want 409", rec.Code)
	}
	if rec := get(PathLaneFiles(l.ID, "path=missing.txt&ref=HEAD")); rec.Code != http.StatusNotFound {
		t.Errorf("missing file at a ref = %d, want 404", rec.Code)
	}

	// Read only: nothing but GET is routed.
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(m, PathLaneFiles(l.ID, "path=README.md"), nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", m, rec.Code)
		}
	}
}
