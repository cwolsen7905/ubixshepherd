package daemon

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/work"
)

// workCache holds each repo's parsed matrix keyed by the blob hash of docs/WORK.md on
// its base ref, so git is asked for the hash (cheap) and the file is read and parsed
// only when it changed.
var workCache = struct {
	sync.Mutex
	byRepo map[string]cachedWork
}{byRepo: map[string]cachedWork{}}

type cachedWork struct {
	blob string
	res  work.Result
}

// readWork reads docs/WORK.md as of origin/<base> in the repo at dir, from git's object
// store and not the working tree, so it is the merged truth as of the last fetch. A
// missing ref or file is found == false, not an error.
func readWork(ctx context.Context, dir, base string) (ref, blob string, res work.Result, found bool) {
	ref = "origin/" + base
	blob, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+":"+work.Path)
	if err != nil || blob == "" {
		return ref, "", work.Result{Rows: []work.Row{}, Warnings: []work.Warning{}}, false
	}
	workCache.Lock()
	c, ok := workCache.byRepo[dir]
	workCache.Unlock()
	if ok && c.blob == blob {
		return ref, blob, c.res, true
	}
	text, err := git.Run(ctx, dir, "cat-file", "blob", blob)
	if err != nil {
		return ref, "", work.Result{Rows: []work.Row{}, Warnings: []work.Warning{}}, false
	}
	res = work.Parse(text)
	workCache.Lock()
	workCache.byRepo[dir] = cachedWork{blob: blob, res: res}
	workCache.Unlock()
	return ref, blob, res, true
}

// prURL is the forge's page for PR or MR n of a remote, or "" when the remote cannot be
// read. github.com is GitHub; any other host is taken to be GitLab, as forge.For does.
func prURL(remote string, n int) string {
	if n <= 0 {
		return ""
	}
	r, err := forge.ParseRemote(remote)
	if err != nil {
		return ""
	}
	if r.Host == "github.com" {
		return fmt.Sprintf("https://%s/%s/pull/%d", r.Host, r.Path, n)
	}
	return fmt.Sprintf("https://%s/%s/-/merge_requests/%d", r.Host, r.Path, n)
}

// closedLanesRead is how many of a repo's most recently closed lanes are read, to tell a
// doing row's closed lane from one that never existed.
const closedLanesRead = 500

// getProjectWork answers GET /v1/projects/{name}/work.
func (s *Server) getProjectWork(w http.ResponseWriter, r *http.Request) {
	name, _, ok := s.pathProject(w, r)
	if !ok {
		return
	}
	s.writeWork(w, r, name)
}

// getUnassignedWork answers GET /v1/unassigned/work.
func (s *Server) getUnassignedWork(w http.ResponseWriter, r *http.Request) {
	s.writeWork(w, r, "")
}

func (s *Server) writeWork(w http.ResponseWriter, r *http.Request, project string) {
	out, err := s.projectWork(r.Context(), project)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// projectWork reads the matrix of each of a project's repos (project "" is the repos in
// no project) and compares it with the repos' lanes.
func (s *Server) projectWork(ctx context.Context, project string) (api.ProjectWork, error) {
	cfg := s.LiveConfig()
	out := api.ProjectWork{Project: project, Repos: []api.RepoWork{}, Drift: []api.WorkDrift{}}
	byName := map[string]store.Repo{}
	var all []string
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return out, err
	}
	for _, ws := range wss {
		repos, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			return out, err
		}
		for _, rp := range repos {
			byName[rp.Name] = rp
			all = append(all, rp.Name)
		}
	}
	var members []string
	if project != "" {
		members = append(members, cfg.Projects[project].Repos...)
	} else {
		for _, n := range all {
			if _, in := cfg.ProjectOf(n); !in {
				members = append(members, n)
			}
		}
		sort.Strings(members)
	}
	for _, name := range members {
		rw := api.RepoWork{Repo: name, Rows: []api.WorkRow{}, Warnings: []work.Warning{}}
		rp, known := byName[name]
		if !known {
			out.Repos = append(out.Repos, rw)
			out.Drift = append(out.Drift, noWorkFile(name, "the repo is not in the store"))
			continue
		}
		base := cfg.Profile(name).BaseBranch
		ref, blob, res, found := readWork(ctx, rp.Path, base)
		rw.Ref, rw.Blob, rw.Found, rw.Warnings = ref, blob, found, res.Warnings
		lanes, err := s.Store.Lanes(ctx, rp.ID) // open lanes only
		if err != nil {
			return out, err
		}
		closed, err := s.Store.LaneHistory(ctx, store.LaneHistoryFilter{RepoID: rp.ID, State: store.HistoryClosed, Limit: closedLanesRead})
		if err != nil {
			return out, err
		}
		for _, c := range closed {
			lanes = append(lanes, c.Lane)
		}
		remote := rp.Remote
		if remote == "" {
			remote, _ = git.Run(ctx, rp.Path, "remote", "get-url", "origin")
		}
		if !found {
			out.Repos = append(out.Repos, rw)
			out.Drift = append(out.Drift, noWorkFile(name, fmt.Sprintf("%s has no %s on %s", name, work.Path, ref)))
			continue
		}
		rows, drift := compareWork(name, res.Rows, lanes, remote)
		rw.Rows = rows
		out.Repos = append(out.Repos, rw)
		out.Drift = append(out.Drift, drift...)
	}
	return out, nil
}

func noWorkFile(repo, msg string) api.WorkDrift {
	return api.WorkDrift{Kind: api.DriftNoWorkFile, Repo: repo, Message: msg}
}

// compareWork links each row to its lane and PR, and lists where the matrix and the
// lanes disagree: an open lane no live row names, and a doing row whose lane is closed
// or not there. A row that is done or dropped does not track an open lane.
func compareWork(repo string, rows []work.Row, lanes []store.Lane, remote string) ([]api.WorkRow, []api.WorkDrift) {
	// A name may be a lane's name or its branch; an open lane wins over a closed one of
	// the same name.
	find := func(name string) (store.Lane, bool) {
		var got store.Lane
		ok := false
		for _, l := range lanes {
			if l.Name != name && l.Branch != name {
				continue
			}
			if !ok || (got.State == store.LaneClosed && l.State != store.LaneClosed) {
				got, ok = l, true
			}
		}
		return got, ok
	}
	out := make([]api.WorkRow, 0, len(rows))
	drift := []api.WorkDrift{}
	tracked := map[int64]bool{}
	for _, r := range rows {
		wr := api.WorkRow{Row: r, Repo: repo, PRURL: prURL(remote, r.PR)}
		l, has := store.Lane{}, false
		if r.Lane != "" {
			l, has = find(r.Lane)
		}
		if has {
			wr.LaneID = l.ID
			if r.Status != work.Done && r.Status != work.Dropped {
				tracked[l.ID] = true
			}
		}
		if r.Status == work.Doing {
			switch {
			case r.Lane == "":
				drift = append(drift, api.WorkDrift{Kind: api.DriftDoingLaneUnknown, Repo: repo, RowID: r.ID,
					Message: fmt.Sprintf("%s is doing but names no lane", r.ID)})
			case !has:
				drift = append(drift, api.WorkDrift{Kind: api.DriftDoingLaneUnknown, Repo: repo, Lane: r.Lane, RowID: r.ID,
					Message: fmt.Sprintf("%s is doing but %s has no lane %s", r.ID, repo, r.Lane)})
			case l.State == store.LaneClosed:
				drift = append(drift, api.WorkDrift{Kind: api.DriftDoingLaneClosed, Repo: repo, Lane: l.Name, LaneID: l.ID, RowID: r.ID,
					Message: fmt.Sprintf("%s is doing but lane %s is closed", r.ID, l.Name)})
			}
		}
		out = append(out, wr)
	}
	for _, l := range lanes {
		if l.State == store.LaneClosed || tracked[l.ID] {
			continue
		}
		drift = append(drift, api.WorkDrift{Kind: api.DriftLaneUntracked, Repo: repo, Lane: l.Name, LaneID: l.ID,
			Message: fmt.Sprintf("lane %s is open and no %s row names it", l.Name, strings.TrimPrefix(work.Path, "docs/"))})
	}
	return out, drift
}
