package daemon

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// listDiagnoses answers GET /v1/runs/{id}/diagnoses with every diagnosis of the run,
// newest first, evidence and fixes included.
func (s *Server) listDiagnoses(w http.ResponseWriter, r *http.Request) {
	run, ok := s.pathRun(w, r)
	if !ok {
		return
	}
	ds, err := s.Store.Diagnoses(r.Context(), run.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]api.Diagnosis, 0, len(ds))
	for _, d := range ds {
		out = append(out, api.Diagnosis{Diagnosis: d})
	}
	writeJSON(w, http.StatusOK, out)
}

// fixRun answers POST /v1/runs/{id}/fix. Who takes the fix follows the token: the front
// desk takes only what the diagnosis marks desk_ok, as suggested; the operator and a
// browser session are the person. A worker's token never reaches here (the route is not
// its), so an agent cannot fix its own run.
func (s *Server) fixRun(w http.ResponseWriter, r *http.Request) {
	run, ok := s.pathRun(w, r)
	if !ok {
		return
	}
	var req api.RunFix
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Action) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("action is required: one of the run's suggested fixes"))
		return
	}
	by := dispatch.ByPerson
	if principalOf(r.Context()).Role == RoleDesk {
		by = dispatch.ByDesk
	}
	res, err := s.Runner.ApplyFix(r.Context(), run.ID, req.Action, by, req.Overrides)
	if err != nil {
		s.foldError(w, err)
		return
	}
	// Widening a scope is the Fold's, which owns scope leases; ApplyFix leaves it to us.
	// Only the person gets here with it, since the desk is refused a needs_person fix.
	if res.Fix.Op == store.FixWidenScope && !res.Done {
		if err := s.widenScope(r.Context(), run, &res); err != nil {
			s.foldError(w, err)
			return
		}
	}
	s.Log.Info("run fix taken", "run", run.ID, "fix", req.Action, "op", res.Fix.Op, "by", by, "client", clientName(r))
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) widenScope(ctx context.Context, run store.Run, res *dispatch.FixResult) error {
	var add []string
	for _, p := range strings.Split(res.Fix.Args["paths"], "\n") {
		if p = strings.TrimSpace(p); p != "" {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return fmt.Errorf("the fix names no paths to add to the scope")
	}
	lane, err := s.Fold.Rescope(ctx, run.LaneID, add, nil)
	if err != nil {
		return err
	}
	s.Store.AddFeed(ctx, store.FeedLaneOpened, fmt.Sprintf("lane %s scope is now %s", lane.Name, strings.Join(lane.Scope, ", ")), lane.ID)
	s.refreshView(lane.RepoID)
	res.Done, res.Message = true, "added "+strings.Join(add, ", ")+" to the lane's scope; continue the run to carry on"
	return nil
}

// attachDiagnoses sets Diagnosis on each run view from the store's newest of each run,
// and Progress on each run still going. full keeps the evidence; a list leaves it out.
func (s *Server) attachDiagnoses(ctx context.Context, views []api.RunView, full bool) error {
	ids := make([]int64, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	latest, err := s.Store.LatestDiagnoses(ctx, ids...)
	if err != nil {
		return err
	}
	for i := range views {
		if d, ok := latest[views[i].ID]; ok {
			views[i].Diagnosis = diagnosisView(d, full)
		}
		views[i].Progress = s.RunProgress(ctx, views[i].Run, views[i].Worktree)
	}
	return nil
}

func diagnosisView(d store.Diagnosis, full bool) *api.Diagnosis {
	v := &api.Diagnosis{Diagnosis: d}
	if !full && d.Evidence != "" {
		v.Evidence, v.EvidenceOmitted = "", true
	}
	return v
}

// attachLaneDiagnoses sets Diagnosis on each lane view to the newest diagnosis of the
// lane's latest run, and Progress when that run is still going.
func (s *Server) attachLaneDiagnoses(ctx context.Context, lanes []api.LaneView) error {
	byRun := map[int64][]int{}
	var ids []int64
	for i, l := range lanes {
		runs, err := s.Store.Runs(ctx, l.ID, "", 1)
		if err != nil {
			return err
		}
		if len(runs) == 1 {
			ids = append(ids, runs[0].ID)
			byRun[runs[0].ID] = append(byRun[runs[0].ID], i)
			lanes[i].Progress = s.RunProgress(ctx, runs[0], l.Worktree)
		}
	}
	latest, err := s.Store.LatestDiagnoses(ctx, ids...)
	if err != nil {
		return err
	}
	for id, d := range latest {
		for _, i := range byRun[id] {
			lanes[i].Diagnosis = diagnosisView(d, false)
		}
	}
	return nil
}
