package daemon

import (
	"context"
	"net/http"
	"slices"
	"sort"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// recentProjectRuns is how many runs a project's detail carries.
const recentProjectRuns = 20

// activeRequests are the states of a request that is not over.
var activeRequests = []string{store.RequestPending, store.RequestNeedsRouting, store.RequestRouted, store.RequestReplyReady}

// projectData is what the project views are cut from: the daemon's own reads, once.
type projectData struct {
	cfg       config.Config
	repos     []string // every repo in the store, by name
	lanes     []api.LaneView
	laneRepo  map[int64]string
	laneName  map[int64]string
	running   map[int64]bool // lanes with a run going
	runs      []api.RunView  // newest first
	decisions []api.DecisionView
	requests  []api.RequestView
	budgets   map[string]dispatch.BudgetLine
	today     map[string]float64 // dollars today by repo
}

func (s *Server) loadProjects(ctx context.Context) (*projectData, error) {
	d := &projectData{cfg: s.LiveConfig(), laneRepo: map[int64]string{}, laneName: map[int64]string{}, running: map[int64]bool{},
		budgets: map[string]dispatch.BudgetLine{}, today: map[string]float64{}}
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, ws := range wss {
		repos, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		for _, rp := range repos {
			d.repos = append(d.repos, rp.Name)
			lanes, err := s.Store.Lanes(ctx, rp.ID)
			if err != nil {
				return nil, err
			}
			for _, l := range lanes {
				d.laneRepo[l.ID], d.laneName[l.ID] = rp.Name, l.Name
				if l.State == store.LaneClosed {
					continue
				}
				v := api.LaneView{Lane: l, Repo: rp.Name}
				if lf, err := s.Store.LaneForge(ctx, l.ID); err == nil && lf.MR != 0 {
					v.MR, v.MRURL, v.MRState = lf.MR, lf.MRURL, api.MRState(lf.MRState)
					v.Pipeline, v.PipelineStatus = lf.Pipeline, api.PipelineStatus(lf.PipelineStatus)
				}
				d.lanes = append(d.lanes, v)
			}
		}
	}
	runs, err := s.Store.Runs(ctx, 0, "", 200)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		repo, ok := d.laneRepo[run.LaneID]
		if !ok {
			continue
		}
		if run.State == store.RunRunning {
			d.running[run.LaneID] = true
		}
		d.runs = append(d.runs, api.RunView{Run: run, Lane: d.laneName[run.LaneID], Repo: repo})
	}
	sort.SliceStable(d.runs, func(i, j int) bool { return d.runs[i].Started.After(d.runs[j].Started) })
	decs, err := s.Store.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		return nil, err
	}
	for _, dec := range decs {
		run, err := s.Store.Run(ctx, dec.RunID)
		if err != nil {
			return nil, err
		}
		v, err := s.runView(ctx, run)
		if err != nil {
			return nil, err
		}
		d.decisions = append(d.decisions, api.DecisionView{Decision: dec, Agent: run.Agent, Lane: v.Lane, Repo: v.Repo})
	}
	reqs, err := s.Store.Requests(ctx, activeRequests...)
	if err != nil {
		return nil, err
	}
	for _, q := range reqs {
		run, err := s.Store.Run(ctx, q.FromRun)
		if err != nil {
			return nil, err
		}
		v, err := s.runView(ctx, run)
		if err != nil {
			return nil, err
		}
		d.requests = append(d.requests, api.RequestView{Request: q, FromAgent: run.Agent, FromLane: v.Lane, Repo: v.Repo})
	}
	lines, err := dispatch.BudgetLines(ctx, s.Store, d.cfg)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		if l.Kind == dispatch.LineProject {
			d.budgets[l.Name] = l
		}
	}
	today := dispatch.Today()
	roll, err := s.Store.SpendRollup(ctx, today, today)
	if err != nil {
		return nil, err
	}
	var credit float64
	if d.cfg.Daemon.CreditUSD != nil {
		credit = *d.cfg.Daemon.CreditUSD
	}
	for _, rs := range roll.Repos {
		d.today[rs.Repo] = rs.USD + rs.Credits*credit
	}
	return d, nil
}

// members are a project's repos, or for "" the store's repos that no project names.
func (d *projectData) members(name string) []string {
	if name != "" {
		return slices.Clone(d.cfg.Projects[name].Repos)
	}
	var out []string
	for _, r := range d.repos {
		if _, in := d.cfg.ProjectOf(r); !in {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// detail cuts one project's view; name "" is the Unassigned group.
func (d *projectData) detail(name string) api.ProjectDetail {
	repos := d.members(name)
	in := func(repo string) bool { return slices.Contains(repos, repo) }
	out := api.ProjectDetail{
		ProjectSummary: api.ProjectSummary{Name: name, Unassigned: name == "", Repos: repos},
		OpenLanes:      []api.LaneView{}, Runs: []api.RunView{},
		PendingDecisions: []api.DecisionView{}, PendingRequests: []api.RequestView{},
	}
	if out.Repos == nil {
		out.Repos = []string{}
	}
	for _, l := range d.lanes {
		if !in(l.Repo) {
			continue
		}
		out.OpenLanes = append(out.OpenLanes, l)
		if d.running[l.ID] {
			out.Running++
		}
	}
	out.Lanes = len(out.OpenLanes)
	for _, run := range d.runs {
		if in(run.Repo) && len(out.Runs) < recentProjectRuns {
			out.Runs = append(out.Runs, run)
		}
	}
	for _, dec := range d.decisions {
		if in(dec.Repo) {
			out.PendingDecisions = append(out.PendingDecisions, dec)
		}
	}
	for _, q := range d.requests {
		if in(q.Repo) || (name != "" && q.Project == name) {
			out.PendingRequests = append(out.PendingRequests, q)
			if q.State == store.RequestNeedsRouting {
				out.Waiting++
			}
		}
	}
	// Oldest first: the one that has waited longest leads.
	sort.SliceStable(out.PendingDecisions, func(i, j int) bool { return out.PendingDecisions[i].Created.Before(out.PendingDecisions[j].Created) })
	sort.SliceStable(out.PendingRequests, func(i, j int) bool { return out.PendingRequests[i].Created.Before(out.PendingRequests[j].Created) })
	out.Decisions, out.Requests = len(out.PendingDecisions), len(out.PendingRequests)
	out.Waiting += out.Decisions
	for _, r := range repos {
		out.SpendToday += d.today[r]
	}
	if name != "" {
		l := d.budgets[name]
		out.Budget = &api.ProjectBudgetView{Amount: l.Amount, Cap: l.Cap, Period: l.Period, Since: l.Since, Spent: l.Spent}
	}
	return out
}

// listProjects answers GET /v1/projects.
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadProjects(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	names := make([]string, 0, len(d.cfg.Projects))
	for n := range d.cfg.Projects {
		names = append(names, n)
	}
	sort.Strings(names)
	out := api.ProjectList{Projects: []api.ProjectSummary{}}
	for _, n := range names {
		out.Projects = append(out.Projects, d.detail(n).ProjectSummary)
	}
	if len(d.members("")) > 0 {
		out.Projects = append(out.Projects, d.detail("").ProjectSummary)
	}
	writeJSON(w, http.StatusOK, out)
}

// getProject answers GET /v1/projects/{name}.
func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	name, _, ok := s.pathProject(w, r)
	if !ok {
		return
	}
	d, err := s.loadProjects(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := d.detail(name)
	brief, err := s.briefView(r.Context(), name, d.cfg.Project(name))
	if err != nil {
		s.fail(w, err)
		return
	}
	out.Brief = &brief
	writeJSON(w, http.StatusOK, out)
}

// getUnassigned answers GET /v1/unassigned.
func (s *Server) getUnassigned(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadProjects(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d.detail(""))
}
