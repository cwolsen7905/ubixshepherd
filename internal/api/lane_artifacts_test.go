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

func gitA(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLaneArtifactEndpoints(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	origin := filepath.Join(root, "origin.git")
	gitA(t, root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	gitA(t, root, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644)
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
	os.MkdirAll(filepath.Join(l.Worktree, "docs"), 0o755)
	os.WriteFile(filepath.Join(l.Worktree, "docs", "plan.md"), []byte("# plan\n"), 0o644)

	mux := http.NewServeMux()
	mux.Handle("GET "+PathLanes+"/{id}/artifacts", LaneArtifacts(f))
	mux.Handle("GET "+PathLanes+"/{id}/artifacts/content", LaneArtifactContent(f))
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}

	rec := get(PathLaneArtifacts(l.ID))
	var list fold.LaneArtifacts
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != 200 ||
		len(list.Artifacts) != 1 || list.Artifacts[0].Path != "docs/plan.md" {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}

	rec = get(PathLaneArtifactContent(l.ID, "docs/plan.md"))
	if rec.Code != 200 || rec.Body.String() != "# plan\n" {
		t.Fatalf("content = %d %q", rec.Code, rec.Body)
	}
	for k, v := range map[string]string{
		"Content-Type": "text/plain; charset=utf-8", "X-Content-Type-Options": "nosniff",
		"Content-Security-Policy": "sandbox; default-src 'none'", "Content-Disposition": "inline",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, bad := range []string{"../secret.md", "/etc/passwd", ""} {
		if rec := get(PathLaneArtifactContent(l.ID, bad)); rec.Code != http.StatusConflict {
			t.Errorf("path %q: %d %s", bad, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
	if rec := get(PathLaneArtifacts(9999)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown lane = %d", rec.Code)
	}
}
