package daemon

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// checksRepo is a managed repo with a bare origin whose main holds a check file and an
// implementation file.
func checksRepo(t *testing.T, s *Server) (store.Workspace, store.Repo) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	origin := filepath.Join(root, "origin.git")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	app := filepath.Join(ws, "app")
	gitIn(t, ws, "clone", "-q", origin, app)
	os.MkdirAll(filepath.Join(app, "src"), 0o755)
	os.WriteFile(filepath.Join(app, "src", "a_test.go"), []byte("package src\n"), 0o644)
	os.WriteFile(filepath.Join(app, "src", "impl.go"), []byte("package src\n"), 0o644)
	gitIn(t, app, "add", ".")
	gitIn(t, app, "commit", "-q", "-m", "init")
	gitIn(t, app, "push", "-q", "origin", "HEAD:main")
	w, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: ws}, []store.Repo{{Name: "app", Path: app}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := s.Store.Repos(ctx, w.ID)
	return w, repos[0]
}

func TestChecksEndpoints(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	w, repo := checksRepo(t, s)
	run, err := func() (store.Run, error) {
		l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: repo.ID, Name: "other", Branch: "other", Base: "main", Worktree: filepath.Join(t.TempDir(), "o"), Scope: []string{"zzz/**"}, State: store.LaneOpen})
		if err != nil {
			return store.Run{}, err
		}
		return s.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: filepath.Join(t.TempDir(), "l")})
	}()
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "approve the checks?", State: store.DecisionOpen})
	answered, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "and now?", State: store.DecisionOpen})
	if _, err := s.Store.AnswerDecision(ctx, answered.ID, "yes, approve them"); err != nil {
		t.Fatal(err)
	}
	approve := func(token string, body api.ApproveChecks, out any) int {
		return call(t, ts, token, "POST", api.PathChecksApprove, body, out)
	}
	body := api.ApproveChecks{RepoID: repo.ID, Paths: []string{"src/a_test.go"}}

	// Who may approve: an agent never, the desk only in a person's turn with an answered
	// decision, the person always.
	worker, _ := s.MintWorker(run.ID)
	if code := approve(worker, body, nil); !denied(code) {
		t.Errorf("worker approve: %d", code)
	}
	if code := call(t, ts, worker, "POST", api.PathChecksRelease, api.ReleaseChecks{RepoID: repo.ID, Paths: []string{"src/a_test.go"}, Why: "x"}, nil); !denied(code) {
		t.Errorf("worker release: %d", code)
	}
	system, _ := s.MintDesk(false, "")
	if code := approve(system, api.ApproveChecks{RepoID: repo.ID, Paths: body.Paths, Decision: answered.ID}, nil); code != http.StatusForbidden {
		t.Errorf("desk outside a person's turn: %d", code)
	}
	human, _ := s.MintDesk(true, "")
	if code := approve(human, body, nil); code != http.StatusBadRequest {
		t.Errorf("desk without a decision: %d", code)
	}
	if code := approve(human, api.ApproveChecks{RepoID: repo.ID, Paths: body.Paths, Decision: dec.ID}, nil); code != http.StatusConflict {
		t.Errorf("desk with an unanswered decision: %d", code)
	}
	if live, _ := s.Store.CheckFiles(ctx, repo.ID, true); len(live) != 0 {
		t.Fatalf("a refused approval marked %+v", live)
	}

	// Input is validated.
	if code := approve(s.Token, api.ApproveChecks{RepoID: repo.ID}, nil); code != http.StatusBadRequest {
		t.Errorf("no paths: %d", code)
	}
	if code := approve(s.Token, api.ApproveChecks{RepoID: repo.ID, Paths: []string{"src/missing_test.go"}}, nil); code != http.StatusConflict {
		t.Errorf("a path not on the base branch: %d", code)
	}
	if code := approve(s.Token, api.ApproveChecks{RepoID: 9999, Paths: body.Paths}, nil); code != http.StatusNotFound {
		t.Errorf("unknown repo: %d", code)
	}
	if code := call(t, ts, s.Token, "GET", api.PathChecks, nil, nil); code != http.StatusBadRequest {
		t.Errorf("list without a repo: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathChecksRelease, api.ReleaseChecks{RepoID: repo.ID, Paths: body.Paths}, nil); code != http.StatusConflict {
		t.Errorf("release with no reason: %d", code)
	}

	// The desk, citing an answered decision, approves; the record says so.
	var done api.CheckChange
	if code := approve(human, api.ApproveChecks{RepoID: repo.ID, Paths: body.Paths, Set: "core", Decision: answered.ID}, &done); code != http.StatusOK ||
		len(done.Files) != 1 || done.Files[0].ApprovedBy != "person (desk, decision "+strconv.FormatInt(answered.ID, 10)+")" || done.Files[0].Set != "core" {
		t.Fatalf("desk approve: %d %+v", code, done)
	}

	// Every client may list, an agent included.
	var list api.Checks
	if code := call(t, ts, worker, "GET", api.PathChecks+"?repo_id="+strconv.FormatInt(repo.ID, 10), nil, &list); code != http.StatusOK ||
		list.Repo != "app" || len(list.Files) != 1 || list.Files[0].Path != "src/a_test.go" {
		t.Fatalf("worker list: %d %+v", code, list)
	}
	var status api.Checks
	call(t, ts, s.Token, "GET", api.PathChecks+"?status=1&repo_id="+strconv.FormatInt(repo.ID, 10), nil, &status)
	if len(status.Status) != 1 || status.Status[0].State != api.CheckCurrent {
		t.Errorf("status: %+v", status)
	}

	// The lane view carries what its scope may not change.
	if _, err := s.Fold.Open(ctx, fold.OpenRequest{RepoID: repo.ID, Name: "build", Scope: []string{"src/**"}}); err != nil {
		t.Fatal(err)
	}
	var lanes []api.LaneView
	call(t, ts, s.Token, "GET", api.PathLanes+"?workspace_id="+strconv.FormatInt(w.ID, 10), nil, &lanes)
	found := false
	for _, l := range lanes {
		if l.Name == "build" {
			found = true
			if len(l.Excluded) != 1 || l.Excluded[0] != "src/a_test.go" {
				t.Errorf("excluded = %v", l.Excluded)
			}
		} else if len(l.Excluded) != 0 {
			t.Errorf("lane %s excluded %v", l.Name, l.Excluded)
		}
	}
	if !found {
		t.Fatalf("lanes: %+v", lanes)
	}

	// A scope change reports the exclusion too.
	var build store.Lane
	for _, l := range lanes {
		if l.Name == "build" {
			build = l.Lane
		}
	}
	var rescoped api.Rescoped
	if code := call(t, ts, s.Token, "POST", api.PathLaneScope(build.ID), api.Rescope{Add: []string{"src/a_test.go"}}, nil); code != http.StatusConflict {
		t.Errorf("rescope onto a marked file: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathLaneScope(build.ID), api.Rescope{Add: []string{"src/*.go"}}, &rescoped); code != http.StatusOK ||
		len(rescoped.Excluded) != 1 || rescoped.Name != "build" {
		t.Errorf("rescope: %d %+v", code, rescoped)
	}

	// Release needs a reason, and the lane for the item being open is not in the way
	// here (no item). The feed records both acts.
	var rel api.CheckChange
	if code := call(t, ts, s.Token, "POST", api.PathChecksRelease, api.ReleaseChecks{RepoID: repo.ID, Paths: body.Paths, Why: "the check was wrong"}, &rel); code != http.StatusOK ||
		len(rel.Files) != 1 || rel.Files[0].Live() {
		t.Fatalf("release: %d %+v", code, rel)
	}
	if code := call(t, ts, s.Token, "POST", api.PathChecksRelease, api.ReleaseChecks{RepoID: repo.ID, Paths: body.Paths, Why: "again"}, nil); code != http.StatusConflict {
		t.Errorf("release of a released file: %d", code)
	}
	var all api.Checks
	call(t, ts, s.Token, "GET", api.PathChecks+"?all=1&repo_id="+strconv.FormatInt(repo.ID, 10), nil, &all)
	if len(all.Files) != 1 || all.Files[0].ReleaseWhy != "the check was wrong" {
		t.Errorf("history: %+v", all)
	}
	items, _ := s.Store.Feed(ctx, 0, 100)
	var approved, released bool
	for _, it := range items {
		approved = approved || (it.Kind == store.FeedChecksApproved && strings.Contains(it.Text, "src/a_test.go") && strings.Contains(it.Text, "person (desk"))
		released = released || (it.Kind == store.FeedChecksReleased && strings.Contains(it.Text, "the check was wrong"))
	}
	if !approved || !released {
		t.Errorf("feed: approved=%v released=%v in %+v", approved, released, items)
	}
}

func TestChecksApproveFromWebAndCLI(t *testing.T) {
	s, ts := newServer(t)
	_, repo := checksRepo(t, s)
	var out api.CheckChange
	if code := call(t, ts, s.Token, "POST", api.PathChecksApprove, api.ApproveChecks{RepoID: repo.ID, Paths: []string{"src/a_test.go"}}, &out); code != http.StatusOK ||
		len(out.Files) != 1 || out.Files[0].ApprovedBy != "person (cli)" {
		t.Fatalf("operator approve: %d %+v", code, out)
	}
}
