package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func newServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := NewServer(st, config.Default(), "/cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func call(t *testing.T, ts *httptest.Server, token, method, path string, body any, out any) int {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestAuth(t *testing.T) {
	s, ts := newServer(t)
	if code := call(t, ts, "wrong", "GET", api.PathStatus, nil, nil); code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", code)
	}
	if code := call(t, ts, s.Token, "GET", api.PathStatus, nil, nil); code != http.StatusOK {
		t.Errorf("right token: %d", code)
	}
}

func TestSaveAndStatusAndResolve(t *testing.T) {
	s, ts := newServer(t)
	root, _ := paths.Canonical(t.TempDir())
	app := filepath.Join(root, "app")
	os.MkdirAll(filepath.Join(app, "src"), 0o755)

	var saved api.WorkspaceDetail
	code := call(t, ts, s.Token, "POST", api.PathWorkspaces, api.SaveWorkspace{
		Name: "git", Path: root, Repos: []store.Repo{{Name: "app", Path: app, Stacks: []string{"go"}}},
	}, &saved)
	if code != http.StatusOK || len(saved.Repos) != 1 {
		t.Fatalf("save: %d %+v", code, saved)
	}

	var st api.Status
	call(t, ts, s.Token, "GET", api.PathStatus, nil, &st)
	if len(st.Workspaces) != 1 || st.Workspaces[0].Repos != 1 || st.Store != "sqlite" {
		t.Errorf("status = %+v", st)
	}

	var res api.Resolution
	call(t, ts, s.Token, "GET", api.PathResolve+"?path="+filepath.Join(app, "src"), nil, &res)
	if res.Workspace == nil || res.Repo == nil || res.Repo.Name != "app" || res.Profile == nil || res.Profile.BaseBranch != "main" {
		t.Errorf("resolve in repo = %+v", res)
	}

	res = api.Resolution{}
	call(t, ts, s.Token, "GET", api.PathResolve+"?path="+root, nil, &res)
	if res.Workspace == nil || res.Repo != nil {
		t.Errorf("resolve at root = %+v", res)
	}

	if code := call(t, ts, s.Token, "GET", api.PathResolve+"?path=relative", nil, nil); code != http.StatusBadRequest {
		t.Errorf("relative path: %d", code)
	}
}

func TestSaveRejects(t *testing.T) {
	s, ts := newServer(t)
	root, _ := paths.Canonical(t.TempDir())
	other, _ := paths.Canonical(t.TempDir())
	cases := map[string]api.SaveWorkspace{
		"repo outside":  {Name: "w", Path: root, Repos: []store.Repo{{Name: "x", Path: other}}},
		"repo is root":  {Name: "w", Path: root, Repos: []store.Repo{{Name: ".", Path: root}}},
		"wrong name":    {Name: "w", Path: root, Repos: []store.Repo{{Name: "y", Path: filepath.Join(root, "x")}}},
		"relative":      {Name: "w", Path: "git"},
		"slash in name": {Name: "a/b", Path: root},
		"empty name":    {Path: root},
	}
	for name, req := range cases {
		var e api.Error
		if code := call(t, ts, s.Token, "POST", api.PathWorkspaces, req, &e); code != http.StatusBadRequest || e.Error == "" {
			t.Errorf("%s: %d %q", name, code, e.Error)
		}
	}
}

// laneStore adds a lane to an otherwise empty store, since lanes are opened in M2.
type laneStore struct {
	store.Store
	ws    store.Workspace
	repos []store.Repo
	lanes []store.Lane
}

func (l laneStore) Workspaces(context.Context) ([]store.Workspace, error) {
	return []store.Workspace{l.ws}, nil
}
func (l laneStore) Repos(context.Context, int64) ([]store.Repo, error) { return l.repos, nil }
func (l laneStore) Lanes(_ context.Context, repoID int64) ([]store.Lane, error) {
	var out []store.Lane
	for _, ln := range l.lanes {
		if ln.RepoID == repoID {
			out = append(out, ln)
		}
	}
	return out, nil
}

func TestResolveLaneAndNesting(t *testing.T) {
	root, _ := paths.Canonical(t.TempDir())
	j := func(p ...string) string { return filepath.Join(append([]string{root}, p...)...) }
	st := laneStore{
		ws: store.Workspace{ID: 1, Name: "git", Path: root},
		repos: []store.Repo{
			{ID: 1, Name: "group", Path: j("group")},
			{ID: 2, Name: "group/lib", Path: j("group", "lib")},
		},
		lanes: []store.Lane{{ID: 1, RepoID: 2, Name: "fix", Branch: "fix/x", Worktree: j("group", "lib-worktrees", "fix"), State: "open"}},
	}
	cfg, err := config.Parse([]byte("repos:\n  group/lib:\n    base_branch: dev\n"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	res, _ := Resolve(ctx, st, cfg, j("group", "lib", "pkg"))
	if res.Repo == nil || res.Repo.Name != "group/lib" || res.Profile.BaseBranch != "dev" {
		t.Errorf("deepest repo should win: %+v", res.Repo)
	}
	res, _ = Resolve(ctx, st, cfg, j("group", "lib-worktrees", "fix", "src"))
	if res.Lane == nil || res.Lane.Name != "fix" || res.Repo.Name != "group/lib" {
		t.Errorf("lane worktree: %+v %+v", res.Lane, res.Repo)
	}
	res, _ = Resolve(ctx, st, cfg, filepath.Dir(root))
	if res.Workspace != nil {
		t.Errorf("outside the workspace resolved to %+v", res.Workspace)
	}
}

func TestRunWritesAndRemovesRuntime(t *testing.T) {
	s, _ := newServer(t)
	rtPath := filepath.Join(t.TempDir(), "daemon.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, rtPath) }()

	var rt api.Runtime
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		if rt, err = ReadRuntime(rtPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime file never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rt.Token != s.Token || !strings.HasPrefix(rt.Addr, "127.0.0.1:") {
		t.Errorf("runtime = %+v", rt)
	}
	if fi, _ := os.Stat(rtPath); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("runtime file mode %v is readable by others", fi.Mode().Perm())
	}

	// A second daemon on the same home refuses to start.
	s2, _ := newServer(t)
	if err := s2.Run(context.Background(), rtPath); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second daemon: %v", err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
	if _, err := os.Stat(rtPath); !os.IsNotExist(err) {
		t.Error("runtime file left behind")
	}
}
