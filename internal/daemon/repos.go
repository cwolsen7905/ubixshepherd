package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/workspace"
)

// listRepos answers GET /v1/repos: the managed repos, the repos found on disk under the
// workspace roots, and the names the config knows that nothing manages. Every call
// reads the disk, so a repo cloned a minute ago is there.
func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	views, err := s.repoViews(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.RepoList{Repos: views})
}

func (s *Server) repoViews(ctx context.Context) ([]api.RepoView, error) {
	cfg := s.LiveConfig()
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{} // names listed, so the config's are not repeated
	var out []api.RepoView
	for _, ws := range wss {
		all, err := s.Store.AllRepos(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		byPath := map[string]store.Repo{}
		for _, rp := range all {
			byPath[rp.Path] = rp
		}
		for _, rp := range all {
			if rp.Removed != nil {
				continue
			}
			v := api.RepoView{Repo: rp, Workspace: ws.Name, State: api.RepoManaged}
			if fi, err := os.Lstat(filepath.Join(rp.Path, ".git")); err != nil || !fi.IsDir() {
				v.State = api.RepoMissing
			}
			lanes, err := s.Store.Lanes(ctx, rp.ID)
			if err != nil {
				return nil, err
			}
			v.OpenLanes = len(lanes)
			_, v.Profile = cfg.Repos[rp.Name]
			named[rp.Name] = true
			out = append(out, v)
		}
		found, err := workspace.Scan(ctx, ws.Path)
		if err != nil {
			s.Log.Warn("repo scan", "workspace", ws.Name, "err", err)
			continue
		}
		for _, f := range found {
			rp, known := byPath[f.Path]
			if known && rp.Removed == nil {
				continue
			}
			v := api.RepoView{
				Repo:      store.Repo{Name: f.Name, Path: f.Path, Remote: f.Remote, Stacks: f.Stacks},
				Workspace: ws.Name, State: api.RepoFound, Reason: f.Reason,
			}
			if known {
				v.ID, v.WorkspaceID, v.Removed = rp.ID, rp.WorkspaceID, rp.Removed
			}
			_, v.Profile = cfg.Repos[f.Name]
			switch {
			case v.Profile:
				v.State = api.RepoProfileOnly
			case projectOf(cfg.Projects, f.Name):
				v.State = api.RepoProjectOnly
			}
			named[f.Name] = true
			out = append(out, v)
		}
	}
	// Names the config knows that are neither managed nor on disk.
	var rest []api.RepoView
	for name := range cfg.Repos {
		if !named[name] {
			named[name] = true
			rest = append(rest, api.RepoView{Repo: store.Repo{Name: name}, State: api.RepoProfileOnly, Profile: true})
		}
	}
	for _, p := range cfg.Projects {
		for _, name := range p.Repos {
			if !named[name] {
				named[name] = true
				rest = append(rest, api.RepoView{Repo: store.Repo{Name: name}, State: api.RepoProjectOnly})
			}
		}
	}
	slices.SortFunc(rest, func(a, b api.RepoView) int { return strings.Compare(a.Name, b.Name) })
	out = append(out, rest...)
	if out == nil {
		out = []api.RepoView{}
	}
	return out, nil
}

// projectOf says whether a project names the repo.
func projectOf(projects map[string]config.Project, name string) bool {
	for _, p := range projects {
		if slices.Contains(p.Repos, name) {
			return true
		}
	}
	return false
}

// addPlan is one repo an AddRepos names, resolved and checked.
type addPlan struct {
	ws    store.Workspace
	found workspace.Found
}

// addRepos answers POST /v1/repos. Every argument is resolved and checked before any
// is added, so one bad one adds none. It never creates a workspace and never writes
// config.yaml; a repo already managed is reported, not an error.
func (s *Server) addRepos(w http.ResponseWriter, r *http.Request) {
	var req api.AddRepos
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Repos) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("name at least one repo to add"))
		return
	}
	s.repoMu.Lock()
	defer s.repoMu.Unlock()
	ctx := r.Context()
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	if req.Workspace != "" {
		i := slices.IndexFunc(wss, func(ws store.Workspace) bool { return ws.Name == req.Workspace })
		if i < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no workspace %q", req.Workspace))
			return
		}
		wss = wss[i : i+1]
	}
	if len(wss) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no workspace yet: run `shepherd init <dir>` to create one"))
		return
	}
	var plans []addPlan
	seen := map[string]bool{}
	for _, ref := range req.Repos {
		p, err := s.planAdd(ctx, wss, ref)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !seen[p.found.Path] {
			seen[p.found.Path] = true
			plans = append(plans, p)
		}
	}

	cfg := s.LiveConfig()
	var out api.AddedRepos
	for _, p := range plans {
		all, err := s.Store.AllRepos(ctx, p.ws.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		got := api.AddedRepo{Workspace: p.ws.Name, Outcome: api.RepoAdded}
		got.Repo = store.Repo{WorkspaceID: p.ws.ID, Name: p.found.Name, Path: p.found.Path, Remote: p.found.Remote, Stacks: p.found.Stacks}
		_, got.Profile = cfg.Repos[p.found.Name]
		if i := slices.IndexFunc(all, func(rp store.Repo) bool { return rp.Path == p.found.Path }); i >= 0 {
			if all[i].Removed == nil {
				got.Repo, got.Outcome = all[i], api.RepoAlreadyOn
				out.Repos = append(out.Repos, got)
				continue
			}
			got.Outcome = api.RepoRestored
		}
		if !p.found.Suggest {
			got.Reason = p.found.Reason
		}
		if _, err := s.Store.SaveWorkspace(ctx, p.ws, []store.Repo{got.Repo}); err != nil {
			s.fail(w, err)
			return
		}
		repos, err := s.Store.Repos(ctx, p.ws.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		if i := slices.IndexFunc(repos, func(rp store.Repo) bool { return rp.Path == p.found.Path }); i >= 0 {
			got.Repo = repos[i]
		}
		s.Log.Info("repo managed", "workspace", p.ws.Name, "repo", got.Name, "outcome", got.Outcome)
		// Files agents keep out of git live in the Shepherd directory; excluding it is
		// a local change to the repo's configuration, so it is said in the feed.
		if note, err := s.Fold.EnsureExclude(ctx, got.Repo.ID); err != nil {
			s.Log.Warn("exclude the artifact directory", "repo", got.Name, "err", err)
		} else if note != "" {
			s.Log.Info(note, "repo", got.Name)
		}
		out.Repos = append(out.Repos, got)
	}
	writeJSON(w, http.StatusOK, out)
}

// planAdd resolves one argument to a repo under one of the workspaces' roots: an
// absolute path, or a name looked up in each root.
func (s *Server) planAdd(ctx context.Context, wss []store.Workspace, ref string) (addPlan, error) {
	if filepath.IsAbs(ref) {
		p, err := paths.Canonical(ref)
		if err != nil {
			return addPlan{}, err
		}
		var best *store.Workspace
		for i := range wss {
			if wss[i].Path != p && paths.Within(wss[i].Path, p) && (best == nil || len(wss[i].Path) > len(best.Path)) {
				best = &wss[i]
			}
		}
		if best == nil {
			return addPlan{}, fmt.Errorf("%s is not inside a workspace: run `shepherd init <dir>` to create one", p)
		}
		rel, _ := filepath.Rel(best.Path, p)
		f, err := workspace.Resolve(ctx, best.Path, filepath.ToSlash(rel))
		if err != nil {
			if errors.Is(err, workspace.ErrNoRepo) {
				return addPlan{}, fmt.Errorf("%s is not a git repo (it has no .git directory)", p)
			}
			return addPlan{}, err
		}
		return addPlan{ws: *best, found: f}, nil
	}
	var hits []addPlan
	for _, ws := range wss {
		f, err := workspace.Resolve(ctx, ws.Path, ref)
		var amb *workspace.AmbiguousError
		switch {
		case err == nil:
			hits = append(hits, addPlan{ws: ws, found: f})
		case errors.As(err, &amb):
			return addPlan{}, err
		case errors.Is(err, workspace.ErrNoRepo):
		default:
			return addPlan{}, err
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return addPlan{}, fmt.Errorf("no git repo %q under a workspace root: see `shepherd repo scan`, or give its path", ref)
	}
	var names []string
	for _, h := range hits {
		names = append(names, h.ws.Name+":"+h.found.Name)
	}
	return addPlan{}, fmt.Errorf("%q is in more than one workspace (%s): pass --workspace", ref, strings.Join(names, ", "))
}

// unmanageRepo answers POST /v1/repos/{id}/unmanage. It marks the repo removed and
// touches nothing else: not the directory, the remote, config.yaml or the history. It
// refuses while the repo has open lanes or a running agent.
func (s *Server) unmanageRepo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req api.UnmanageRepo
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	s.repoMu.Lock()
	defer s.repoMu.Unlock()
	repo, err := s.Store.Repo(ctx, pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no repo %d", pathID(r)))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if repo.Removed != nil {
		writeJSON(w, http.StatusOK, repo)
		return
	}
	lanes, err := s.Store.Lanes(ctx, repo.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(lanes) > 0 {
		names := make([]string, len(lanes))
		for i, l := range lanes {
			names[i] = l.Name
		}
		writeError(w, http.StatusConflict, fmt.Errorf("%s has %s: %s: close them first", repo.Name,
			countOf(len(lanes), "open lane", "open lanes"), strings.Join(names, ", ")))
		return
	}
	// A run can outlive its lane's close, so look at every running run, not only the
	// open lanes'.
	runs, err := s.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, run := range runs {
		l, err := s.Store.Lane(ctx, run.LaneID)
		if err == nil && l.RepoID == repo.ID {
			writeError(w, http.StatusConflict, fmt.Errorf("%s has run %d active in lane %s: stop it first", repo.Name, run.ID, l.Name))
			return
		}
	}
	at := time.Now().UTC()
	if err := s.Store.SetRepoRemoved(ctx, repo.ID, &at); err != nil {
		s.fail(w, err)
		return
	}
	repo.Removed = &at
	s.Log.Info("repo unmanaged", "repo", repo.Name, "why", redactWhy(req.Why))
	writeJSON(w, http.StatusOK, repo)
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func redactWhy(s string) string { return words(s) }

func (s *Server) describeAddRepos(ctx context.Context, _ *http.Request, _, resp []byte) (int64, string) {
	var out api.AddedRepos
	if json.Unmarshal(resp, &out) != nil {
		return 0, ""
	}
	var names []string
	var ws int64
	for _, a := range out.Repos {
		if a.Outcome != api.RepoAlreadyOn {
			names = append(names, a.Name)
			ws = a.WorkspaceID
		}
	}
	if len(names) == 0 {
		return 0, ""
	}
	return ws, "added to managed repos: " + strings.Join(names, ", ")
}

func (s *Server) describeUnmanage(ctx context.Context, r *http.Request, _, _ []byte) (int64, string) {
	repo, err := s.Store.Repo(ctx, pathID(r))
	if err != nil {
		return 0, ""
	}
	return repo.WorkspaceID, "stopped managing repo " + repo.Name
}
