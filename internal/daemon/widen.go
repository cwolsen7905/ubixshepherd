package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// widenLane adds globs to an open lane's scope when the person approved them in a
// decision. It is the front desk's counterpart to `lane scope`: the person's own call,
// so the desk does it only in a turn they started and cites the answered decision, one
// raised by a run of this lane. The Fold judges the overlap with other open lanes.
func (s *Server) widenLane(w http.ResponseWriter, r *http.Request) {
	if !s.personsOnly(w, r, crook.ActionWidenScope) {
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
	by := "person"
	if principalOf(ctx).Role == RoleDesk {
		by = "desk"
	}
	lane, err = s.applyWiden(ctx, lane, add, dec.ID, by, clientName(r))
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lane)
}

// applyWiden widens the lane through the Fold, which judges the overlap with other open
// lanes, and records the feed line that ties the change to the decision.
func (s *Server) applyWiden(ctx context.Context, lane store.Lane, add []string, decision int64, by, client string) (store.Lane, error) {
	lane, err := s.Fold.Rescope(ctx, lane.ID, add, nil)
	if err != nil {
		return lane, err
	}
	s.Log.Info("lane scope widened", "lane", lane.Name, "add", add, "decision", decision, "by", by, "client", client)
	s.Store.AddFeed(ctx, store.FeedLaneOpened, fmt.Sprintf("lane %s scope widened by %s: +%s (decision %d); scope is now %s",
		lane.Name, by, strings.Join(add, ", "), decision, strings.Join(lane.Scope, ", ")), lane.ID)
	s.refreshView(lane.RepoID)
	return lane, nil
}

// widenOnAnswer widens the asking lane by the paths the person just approved, as part of
// the answer. A refusal (another open lane holds an overlapping glob) leaves the answer
// recorded and says why in the feed. Only a person's answer reaches it: answerDecision
// sits behind personsAnswer.
func (s *Server) widenOnAnswer(r *http.Request, d store.Decision) {
	ctx := r.Context()
	run, err := s.Store.Run(ctx, d.RunID)
	if err != nil {
		return
	}
	lane, err := s.Store.Lane(ctx, run.LaneID)
	if err != nil || lane.State != store.LaneOpen {
		return
	}
	add := waiting.ApprovedWiden(lane, d)
	if len(add) == 0 {
		return
	}
	if _, err := s.applyWiden(ctx, lane, add, d.ID, "person", clientName(r)); err != nil {
		s.Log.Warn("scope not widened on approval", "lane", lane.Name, "decision", d.ID, "err", err)
		s.Store.AddFeed(ctx, store.FeedLaneOpened, fmt.Sprintf("lane %s scope not widened by the answer to decision %d (+%s): %v; the answer stands",
			lane.Name, d.ID, strings.Join(add, ", "), err), lane.ID)
	}
}

// wireAutoWiden lets the Runner widen a lane's scope by rule through the Fold, which
// judges the overlap with other open lanes. It does so only when the config in force
// has a repo (or the defaults) set to `widen: auto`; otherwise the Runner is left as it
// was and no widening happens. The setting is read when the daemon starts.
func (s *Server) wireAutoWiden() {
	if s.Runner == nil || s.Fold == nil || s.Runner.Rescope != nil {
		return
	}
	cfg := s.LiveConfig()
	on := cfg.Defaults.Widen == config.WidenAuto
	for _, p := range cfg.Repos {
		on = on || p.Widen == config.WidenAuto
	}
	if on {
		s.Runner.Rescope = s.Fold.Rescope
	}
}
