package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// gitRepo makes a real repo with an origin remote at dir.
func gitRepo(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		if remote == "" && args[0] == "remote" {
			continue
		}
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// repoWorkspace is a workspace with one managed repo, "app", and others on disk.
func repoWorkspace(t *testing.T) (*Server, *httptest.Server, string, store.Repo) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s, ts := newServer(t)
	root, _ := paths.Canonical(t.TempDir())
	gitRepo(t, filepath.Join(root, "app"), "https://example.com/t/app.git")
	gitRepo(t, filepath.Join(root, "my-service"), "https://example.com/t/my-service.git")
	gitRepo(t, filepath.Join(root, "g1", "lib"), "https://example.com/g1/lib.git")
	gitRepo(t, filepath.Join(root, "g2", "lib"), "https://example.com/g2/lib.git")
	gitRepo(t, filepath.Join(root, "scratch"), "")
	ws, err := s.Store.SaveWorkspace(context.Background(), store.Workspace{Name: "git", Path: root},
		[]store.Repo{{Name: "app", Path: filepath.Join(root, "app")}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := s.Store.Repos(context.Background(), ws.ID)
	return s, ts, root, repos[0]
}

func managedNames(t *testing.T, s *Server) []string {
	t.Helper()
	wss, _ := s.Store.Workspaces(context.Background())
	var names []string
	for _, ws := range wss {
		repos, _ := s.Store.Repos(context.Background(), ws.ID)
		for _, r := range repos {
			names = append(names, r.Name)
		}
	}
	return names
}

func TestAddRepoByNameAndPath(t *testing.T) {
	s, ts, root, _ := repoWorkspace(t)
	var out api.AddedRepos
	code := call(t, ts, s.Token, "POST", api.PathRepos, api.AddRepos{Repos: []string{"my-service", filepath.Join(root, "scratch")}}, &out)
	if code != http.StatusOK || len(out.Repos) != 2 {
		t.Fatalf("add: %d %+v", code, out)
	}
	if out.Repos[0].Name != "my-service" || out.Repos[0].Outcome != api.RepoAdded || out.Repos[0].Profile || out.Repos[0].ID == 0 {
		t.Errorf("my-service = %+v", out.Repos[0])
	}
	if r := out.Repos[1]; r.Name != "scratch" || r.Reason != "no origin remote" {
		t.Errorf("scratch = %+v", r)
	}
	if got := strings.Join(managedNames(t, s), ","); got != "app,my-service,scratch" {
		t.Errorf("managed = %s", got)
	}

	// Idempotent.
	out = api.AddedRepos{}
	code = call(t, ts, s.Token, "POST", api.PathRepos, api.AddRepos{Repos: []string{"my-service"}}, &out)
	if code != http.StatusOK || len(out.Repos) != 1 || out.Repos[0].Outcome != api.RepoAlreadyOn {
		t.Errorf("again: %d %+v", code, out)
	}
}

func TestAddRepoRefusals(t *testing.T) {
	s, ts, root, _ := repoWorkspace(t)
	outside, _ := paths.Canonical(t.TempDir())
	gitRepo(t, filepath.Join(outside, "elsewhere"), "https://example.com/e.git")
	os.MkdirAll(filepath.Join(root, "plain"), 0o755)

	cases := map[string]struct {
		repos []string
		want  string
	}{
		"ambiguous":         {[]string{"lib"}, "ambiguous"},
		"outside":           {[]string{filepath.Join(outside, "elsewhere")}, "shepherd init"},
		"not a repo":        {[]string{filepath.Join(root, "plain")}, "not a git repo"},
		"unknown":           {[]string{"nope"}, "no git repo"},
		"one bad adds none": {[]string{"my-service", "nope"}, "no git repo"},
	}
	for name, c := range cases {
		var e api.Error
		code := call(t, ts, s.Token, "POST", api.PathRepos, api.AddRepos{Repos: c.repos}, &e)
		if code != http.StatusBadRequest || !strings.Contains(e.Error, c.want) {
			t.Errorf("%s: %d %q, want %q", name, code, e.Error, c.want)
		}
	}
	if got := strings.Join(managedNames(t, s), ","); got != "app" {
		t.Errorf("a refused add changed the store: %s", got)
	}
	// An exact path in the workspace names one of the two libs.
	var out api.AddedRepos
	if code := call(t, ts, s.Token, "POST", api.PathRepos, api.AddRepos{Repos: []string{"g2/lib"}}, &out); code != 200 || out.Repos[0].Name != "g2/lib" {
		t.Errorf("g2/lib: %d %+v", code, out)
	}
}

func TestUnmanageRepo(t *testing.T) {
	s, ts, root, app := repoWorkspace(t)
	ctx := context.Background()
	url := func(id int64) string { return api.PathRepos + "/" + itoa(id) + "/unmanage" }

	lane, err := s.Store.CreateLane(ctx, store.Lane{RepoID: app.ID, Name: "feat/a", Branch: "feat/a", Worktree: filepath.Join(root, "wt"), State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	var e api.Error
	if code := call(t, ts, s.Token, "POST", url(app.ID), api.UnmanageRepo{}, &e); code != http.StatusConflict || !strings.Contains(e.Error, "feat/a") {
		t.Errorf("open lane: %d %q", code, e.Error)
	}

	// An active run on a lane that has been closed still blocks it.
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunRunning})
	if err != nil {
		t.Fatal(err)
	}
	s.Store.SetLaneState(ctx, lane.ID, store.LaneClosed)
	e = api.Error{}
	if code := call(t, ts, s.Token, "POST", url(app.ID), nil, &e); code != http.StatusConflict || !strings.Contains(e.Error, "run") {
		t.Errorf("active run: %d %q", code, e.Error)
	}
	run.State = store.RunSucceeded
	s.Store.UpdateRun(ctx, run)

	var got store.Repo
	if code := call(t, ts, s.Token, "POST", url(app.ID), api.UnmanageRepo{Why: "retired"}, &got); code != http.StatusOK || got.Removed == nil {
		t.Fatalf("unmanage: %d %+v", code, got)
	}
	if len(managedNames(t, s)) != 0 {
		t.Errorf("still managed: %v", managedNames(t, s))
	}
	// History stays readable.
	if r, err := s.Store.Repo(ctx, app.ID); err != nil || r.Removed == nil {
		t.Errorf("repo by id = %+v, %v", r, err)
	}
	if _, err := s.Store.Run(ctx, run.ID); err != nil {
		t.Errorf("run: %v", err)
	}

	// Not offered as managed; list shows it found, marked removed; adding restores it.
	var list api.RepoList
	call(t, ts, s.Token, "GET", api.PathRepos, nil, &list)
	var seen *api.RepoView
	for i, v := range list.Repos {
		if v.Name == "app" {
			seen = &list.Repos[i]
		}
	}
	if seen == nil || seen.State != api.RepoFound || seen.Removed == nil {
		t.Errorf("app in list = %+v", seen)
	}
	var out api.AddedRepos
	call(t, ts, s.Token, "POST", api.PathRepos, api.AddRepos{Repos: []string{"app"}}, &out)
	if len(out.Repos) != 1 || out.Repos[0].Outcome != api.RepoRestored || out.Repos[0].ID != app.ID || out.Repos[0].Removed != nil {
		t.Errorf("restore = %+v", out)
	}
	if code := call(t, ts, s.Token, "POST", url(9999), nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown repo: %d", code)
	}
}

func TestListReposStates(t *testing.T) {
	s, ts, root, _ := repoWorkspace(t)
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"my-service": {}, "ghost": {}}
	cfg.Projects = map[string]config.Project{"p": {Repos: []string{"scratch", "phantom"}}}
	s.Config = cfg
	os.RemoveAll(filepath.Join(root, "app", ".git"))

	var list api.RepoList
	if code := call(t, ts, s.Token, "GET", api.PathRepos, nil, &list); code != 200 {
		t.Fatalf("list: %d", code)
	}
	state := map[string]string{}
	for _, v := range list.Repos {
		state[v.Name] = v.State
	}
	want := map[string]string{
		"app": api.RepoMissing, "my-service": api.RepoProfileOnly, "ghost": api.RepoProfileOnly,
		"scratch": api.RepoProjectOnly, "phantom": api.RepoProjectOnly, "g1/lib": api.RepoFound,
	}
	for n, w := range want {
		if state[n] != w {
			t.Errorf("%s = %q, want %q", n, state[n], w)
		}
	}
}

func TestReposRoles(t *testing.T) {
	s, ts, _, app := repoWorkspace(t)
	worker, _ := s.MintWorker(1)
	for _, c := range []struct{ method, path string }{
		{"GET", api.PathRepos},
		{"POST", api.PathRepos},
		{"POST", api.PathRepos + "/" + itoa(app.ID) + "/unmanage"},
	} {
		if code := call(t, ts, worker, c.method, c.path, api.AddRepos{Repos: []string{"my-service"}}, nil); code != http.StatusForbidden {
			t.Errorf("worker %s %s: %d", c.method, c.path, code)
		}
	}
	if len(managedNames(t, s)) != 1 {
		t.Error("a worker changed the repos")
	}
	deskTok, _ := s.MintDesk(true, "s")
	if code := call(t, ts, deskTok, "POST", api.PathRepos, api.AddRepos{Repos: []string{"my-service"}}, nil); code != 200 {
		t.Errorf("desk add: %d", code)
	}
}
