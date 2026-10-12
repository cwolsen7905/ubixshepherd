package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// widenLane adds globs to an open lane's scope when the person approved them in a
// decision. It is the front desk's counterpart to `lane scope`: the person's own call,
// so the desk does it only in a turn they started and cites the answered decision, one
// raised by a run of this lane. The Fold judges the overlap with other open lanes.
func (s *Server) widenLane(w http.ResponseWriter, r *http.Request) {
	if p := principalOf(r.Context()); p.Role == RoleDesk && !p.Human {
		writeError(w, http.StatusForbidden, errors.New("the front desk may widen a lane's scope only in a turn the person started; bring the request to them"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req api.WidenLane
	if !decode(w, r, &req) {
		return
	}
	var add []string
	for _, g := range req.Add {
		if g = strings.TrimSpace(g); g != "" {
			add = append(add, g)
		}
	}
	if len(add) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("add names no globs"))
		return
	}
	ctx := r.Context()
	lane, err := s.Store.Lane(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if req.Decision <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("decision is required: the answered decision in which the person approved these paths"))
		return
	}
	dec, err := s.Store.Decision(ctx, req.Decision)
	if err != nil {
		s.fail(w, err)
		return
	}
	if dec.State != store.DecisionAnswered {
		writeError(w, http.StatusConflict, fmt.Errorf("decision %d is %s: the person has not answered it", dec.ID, dec.State))
		return
	}
	run, err := s.Store.Run(ctx, dec.RunID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if run.LaneID != lane.ID {
		writeError(w, http.StatusConflict, fmt.Errorf("decision %d was not raised in lane %s", dec.ID, lane.Name))
		return
	}
	lane, err = s.Fold.Rescope(ctx, lane.ID, add, nil)
	if err != nil {
		s.foldError(w, err)
		return
	}
	by := "person"
	if principalOf(ctx).Role == RoleDesk {
		by = "desk"
	}
	s.Log.Info("lane scope widened", "lane", lane.Name, "add", add, "decision", dec.ID, "by", by, "client", clientName(r))
	s.Store.AddFeed(ctx, store.FeedLaneOpened, fmt.Sprintf("lane %s scope widened by %s: +%s (decision %d); scope is now %s",
		lane.Name, by, strings.Join(add, ", "), dec.ID, strings.Join(lane.Scope, ", ")), lane.ID)
	s.refreshView(lane.RepoID)
	writeJSON(w, http.StatusOK, lane)
}
