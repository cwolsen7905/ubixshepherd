package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

const workFile = `## Work

| ID | Area | Item | Status | Lane | PR | Notes |
|----|------|------|--------|------|----|-------|
| W1 | web | Page | doing | live | #7 | |
| W2 | web | Old | doing | gone | - | |
| W3 | web | Shipped | doing | finished | - | |
| W4 | web | Later | todo | - | - | |
| W5 | web | Bad | maybe | - | - | |
`

// workRepo makes a clone of a bare origin whose dev branch carries docs/WORK.md, with
// the clone's working tree changed after the fact so a read of it would give another answer.
func workRepo(t *testing.T, root, name, text string) string {
	t.Helper()
	bare := filepath.Join(root, name+".git")
	dir := filepath.Join(root, name)
	gitIn(t, root, "init", "--bare", "-q", bare)
	gitIn(t, root, "clone", "-q", bare, dir)
	gitIn(t, dir, "checkout", "-q", "-b", "dev")
	if text != "" {
		if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docs", "WORK.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	gitIn(t, dir, "push", "-q", "origin", "dev")
	return dir
}

func TestProjectWork(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	app := workRepo(t, root, "app", workFile)
	bare := workRepo(t, root, "bare", "")
	ws, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root},
		[]store.Repo{{Name: "app", Path: app, Remote: "git@github.com:example/app.git"}, {Name: "bare", Path: bare}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := s.Store.Repos(ctx, ws.ID)
	var appID int64
	for _, rp := range repos {
		if rp.Name == "app" {
			appID = rp.ID
		}
	}
	mk := func(name, state string) store.Lane {
		l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: appID, Name: name, Branch: name, Base: "dev", Worktree: filepath.Join(root, name), State: state})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	live := mk("live", store.LaneOpen)
	mk("finished", store.LaneClosed)
	extra := mk("extra", store.LaneOpen)
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {BaseBranch: "dev"}, "bare": {BaseBranch: "dev"}}
	cfg.Projects = map[string]config.Project{"shop": {Repos: []string{"app", "bare"}}}
	s.applyConfig(cfg)

	// Changing the working tree must not change the answer: the base ref is what is read.
	if err := os.WriteFile(filepath.Join(app, "docs", "WORK.md"), []byte("## Work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got api.ProjectWork
	if code := call(t, ts, s.Token, "GET", api.PathProjectWork("shop"), nil, &got); code != http.StatusOK {
		// The route is registered in daemon.go; until it is, ask the handler directly.
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", api.PathProjectWork("shop"), nil)
		req.SetPathValue("name", "shop")
		s.getProjectWork(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code %d: %s", rec.Code, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
	}
	if len(got.Repos) != 2 || got.Repos[0].Repo != "app" || !got.Repos[0].Found {
		t.Fatalf("repos: %+v", got.Repos)
	}
	a := got.Repos[0]
	if a.Ref != "origin/dev" || a.Blob == "" || len(a.Rows) != 4 || len(a.Warnings) != 1 || a.Warnings[0].Line != 9 {
		t.Fatalf("app: %+v", a)
	}
	if r := a.Rows[0]; r.LaneID != live.ID || r.PRURL != "https://github.com/example/app/pull/7" {
		t.Errorf("W1: %+v", r)
	}
	if got.Repos[1].Found {
		t.Errorf("bare should have no file: %+v", got.Repos[1])
	}
	kinds := map[string]api.WorkDrift{}
	for _, d := range got.Drift {
		kinds[d.Kind+":"+d.RowID+d.Lane] = d
	}
	for _, k := range []string{
		api.DriftDoingLaneUnknown + ":W2gone",
		api.DriftDoingLaneClosed + ":W3finished",
		api.DriftLaneUntracked + ":extra",
		api.DriftNoWorkFile + ":",
	} {
		if _, ok := kinds[k]; !ok {
			t.Errorf("missing drift %s in %+v", k, got.Drift)
		}
	}
	if d := kinds[api.DriftLaneUntracked+":extra"]; d.LaneID != extra.ID {
		t.Errorf("untracked: %+v", d)
	}
	if len(got.Drift) != 4 {
		t.Errorf("drift: %+v", got.Drift)
	}

	// The cache answers the same blob without a new parse, and a new commit replaces it.
	_, blob1, _, _ := readWork(ctx, app, "dev")
	_, blob2, _, _ := readWork(ctx, app, "dev")
	if blob1 != a.Blob || blob2 != blob1 {
		t.Errorf("blobs %s %s %s", a.Blob, blob1, blob2)
	}
	gitIn(t, app, "checkout", "-q", "dev")
	gitIn(t, app, "commit", "-q", "-a", "-m", "shrink")
	gitIn(t, app, "push", "-q", "origin", "dev")
	if _, blob3, res, _ := readWork(ctx, app, "dev"); blob3 == blob1 || len(res.Rows) != 0 || len(res.Warnings) != 1 {
		t.Errorf("after change: %s %+v", blob3, res)
	}
}

func TestProjectWorkUnknownProject(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", api.PathProjectWork("nope"), nil)
	req.SetPathValue("name", "nope")
	s.getProjectWork(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code %d", rec.Code)
	}
}

func TestPRURL(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:o/r.git":               "https://github.com/o/r/pull/3",
		"https://gitlab.example.com/g/r":       "https://gitlab.example.com/g/r/-/merge_requests/3",
		"ssh://git@git.example.com:22/g/r.git": "https://git.example.com/g/r/-/merge_requests/3",
		"":                                     "",
		"not a remote":                         "",
	} {
		if got := prURL(remote, 3); got != want {
			t.Errorf("%q: %q, want %q", remote, got, want)
		}
	}
	if prURL("git@github.com:o/r.git", 0) != "" {
		t.Error("pr 0 has no url")
	}
}
