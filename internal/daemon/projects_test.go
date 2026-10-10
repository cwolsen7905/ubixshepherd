package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// projectWorld has repos app (in project shop) and docs (in none), with a lane each; the
// app lane has a running run and a decision, the docs lane a failed run.
func projectWorld(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root},
		[]store.Repo{{Name: "app", Path: filepath.Join(root, "app")}, {Name: "docs", Path: filepath.Join(root, "docs")}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := s.Store.Repos(ctx, ws.ID)
	for _, rp := range repos {
		l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: rp.ID, Name: rp.Name + "-work", Branch: rp.Name + "-work", Base: "main",
			Worktree: filepath.Join(root, rp.Name+"-work"), State: store.LaneOpen})
		if err != nil {
			t.Fatal(err)
		}
		state := store.RunRunning
		if rp.Name == "docs" {
			state = store.RunFailed
		}
		run, err := s.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Prompt: "x", State: state, Log: filepath.Join(root, rp.Name+".log")})
		if err != nil {
			t.Fatal(err)
		}
		if rp.Name == "app" {
			if _, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "Which one?", State: store.DecisionOpen}); err != nil {
				t.Fatal(err)
			}
		}
	}
	b := 10.0
	cfg := config.Default()
	cfg.Repos = map[string]config.Profile{"app": {}, "docs": {}}
	cfg.Projects = map[string]config.Project{"shop": {Repos: []string{"app"}, Budget: config.ProjectBudget{Amount: &b}}}
	s.applyConfig(cfg)
}

func TestProjectsListAndDetail(t *testing.T) {
	s, ts := newServer(t)
	projectWorld(t, s)

	var list api.ProjectList
	if code := call(t, ts, s.Token, "GET", api.PathProjects, nil, &list); code != http.StatusOK || len(list.Projects) != 2 {
		t.Fatalf("list: %d %+v", code, list)
	}
	shop, un := list.Projects[0], list.Projects[1]
	if shop.Name != "shop" || shop.Unassigned || shop.Lanes != 1 || shop.Running != 1 || shop.Decisions != 1 || shop.Waiting != 1 ||
		shop.Budget == nil || shop.Budget.Amount != 10 || shop.Budget.Period != config.PeriodDay {
		t.Errorf("shop: %+v", shop)
	}
	if !un.Unassigned || un.Name != "" || len(un.Repos) != 1 || un.Repos[0] != "docs" || un.Lanes != 1 || un.Running != 0 || un.Waiting != 0 || un.Budget != nil {
		t.Errorf("unassigned: %+v", un)
	}

	var d api.ProjectDetail
	if code := call(t, ts, s.Token, "GET", api.PathProject("shop"), nil, &d); code != http.StatusOK {
		t.Fatalf("detail: %d", code)
	}
	if len(d.OpenLanes) != 1 || d.OpenLanes[0].Repo != "app" || len(d.Runs) != 1 || d.Runs[0].Lane != "app-work" ||
		len(d.PendingDecisions) != 1 || d.PendingDecisions[0].Lane != "app-work" || d.Brief == nil || d.Brief.Project != "shop" {
		t.Errorf("detail: %+v", d)
	}
	if code := call(t, ts, s.Token, "GET", api.PathProject("nope"), nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown project: %d", code)
	}
	var u api.ProjectDetail
	call(t, ts, s.Token, "GET", api.PathProjectsUnassigned, nil, &u)
	if len(u.OpenLanes) != 1 || u.OpenLanes[0].Repo != "docs" || len(u.Runs) != 1 || u.Runs[0].State != store.RunFailed || u.Brief != nil || len(u.PendingDecisions) != 0 {
		t.Errorf("unassigned detail: %+v", u)
	}
}

func TestProjectsHideUnassignedWhenEmpty(t *testing.T) {
	s, ts := newServer(t)
	projectWorld(t, s)
	cfg := s.LiveConfig()
	cfg.Projects = map[string]config.Project{"all": {Repos: []string{"app", "docs"}}}
	s.applyConfig(cfg)
	var list api.ProjectList
	call(t, ts, s.Token, "GET", api.PathProjects, nil, &list)
	if len(list.Projects) != 1 || list.Projects[0].Name != "all" {
		t.Errorf("list: %+v", list)
	}
}

func TestProjectsForWebSessionsAndNotWorkers(t *testing.T) {
	s, base, _ := webDaemon(t)
	projectWorld(t, s)
	b := newBrowser(t, base)
	signInWith(t, s, b, false)
	for _, path := range []string{api.PathProjects, api.PathProject("shop"), api.PathProjectsUnassigned} {
		resp, body := b.do("GET", path, nil)
		var v map[string]any
		if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &v) != nil {
			t.Errorf("session GET %s: %d %s", path, resp.StatusCode, body)
		}
	}
	// A browser without a session is refused, and the routes are read-only.
	anon := newBrowser(t, base)
	if resp, _ := anon.do("GET", api.PathProjects, nil); resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf("no session: %d", resp.StatusCode)
	}
	if resp, _ := b.do("POST", api.PathProjects, map[string]any{}); resp.StatusCode == http.StatusOK {
		t.Errorf("POST %s was allowed", api.PathProjects)
	}
}
