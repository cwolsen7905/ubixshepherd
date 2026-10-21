package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// listChecks answers GET /v1/checks: a repo's approved check files, for every client.
func (s *Server) listChecks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repoID, err := strconv.ParseInt(r.URL.Query().Get("repo_id"), 10, 64)
	if err != nil || repoID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("repo_id is required"))
		return
	}
	repo, err := s.Store.Repo(ctx, repoID)
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	out := api.Checks{RepoID: repo.ID, Repo: repo.Name, Files: []store.CheckFile{}}
	if q.Get("status") != "" {
		states, err := s.Fold.CheckStatus(ctx, repo.ID)
		if err != nil {
			s.foldError(w, err)
			return
		}
		for _, st := range states {
			out.Files = append(out.Files, st.CheckFile)
			out.Status = append(out.Status, api.CheckStatus{CheckFile: st.CheckFile, State: st.State, BaseBlob: st.BaseBlob})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	files, err := s.Store.CheckFiles(ctx, repo.ID, q.Get("all") == "")
	if err != nil {
		s.fail(w, err)
		return
	}
	if files != nil {
		out.Files = files
	}
	writeJSON(w, http.StatusOK, out)
}

// checksActor decides whether the caller may approve or release check files, and says
// how it is recorded: "person (cli)", "person (web)" or "person (desk)". The front desk
// must also cite a decision the person answered, as lane_widen does.
func (s *Server) checksActor(w http.ResponseWriter, r *http.Request, a crook.Action, decision, repoID int64) (by, cited string, ok bool) {
	if !s.personsOnly(w, r, a) {
		return "", "", false
	}
	switch principalOf(r.Context()).Role {
	case RoleDesk:
		if decision <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("decision is required: the answered decision in which the person said so"))
			return "", "", false
		}
		ctx := r.Context()
		dec, err := s.Store.Decision(ctx, decision)
		if err != nil {
			s.fail(w, err)
			return "", "", false
		}
		if dec.State != store.DecisionAnswered {
			writeError(w, http.StatusConflict, fmt.Errorf("decision %d is %s: the person has not answered it", dec.ID, dec.State))
			return "", "", false
		}
		// The decision must have been raised in this repo, so an answer about something
		// else cannot stand for an approval here.
		run, err := s.Store.Run(ctx, dec.RunID)
		if err != nil {
			s.fail(w, err)
			return "", "", false
		}
		lane, err := s.Store.Lane(ctx, run.LaneID)
		if err != nil {
			s.fail(w, err)
			return "", "", false
		}
		if lane.RepoID != repoID {
			writeError(w, http.StatusConflict, fmt.Errorf("decision %d was not raised in this repo", dec.ID))
			return "", "", false
		}
		return "person (desk, decision " + strconv.FormatInt(dec.ID, 10) + ")", fmt.Sprintf(` (decision %d: "%s")`, dec.ID, clipText(dec.Question, 120)), true
	case RoleWeb:
		return "person (web)", "", true
	}
	return "person (cli)", "", true
}

func cleanPaths(in []string) []string {
	var out []string
	for _, p := range in {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pathList(files []store.CheckFile) string {
	return strings.Join(fold.CheckPaths(files), ", ")
}

// approveChecks answers POST /v1/checks/approve: it marks files that are already on the
// repo's base branch, as the person's act.
func (s *Server) approveChecks(w http.ResponseWriter, r *http.Request) {
	var req api.ApproveChecks
	if !decode(w, r, &req) {
		return
	}
	by, cited, ok := s.checksActor(w, r, crook.ActionChecksApprove, req.Decision, req.RepoID)
	if !ok {
		return
	}
	paths := cleanPaths(req.Paths)
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("paths names no files"))
		return
	}
	files, err := s.Fold.ApproveChecks(r.Context(), fold.ApproveRequest{RepoID: req.RepoID, Paths: paths, ItemID: req.ItemID, Set: req.Set, By: by})
	if err != nil {
		s.foldError(w, err)
		return
	}
	s.Log.Info("check files approved", "repo_id", req.RepoID, "paths", paths, "by", by, "client", clientName(r))
	s.postFeed(r.Context(), store.FeedChecksApproved, req.ItemID,
		redact.String(fmt.Sprintf("%d check files approved by %s: %s%s", len(files), by, pathList(files), itemWords(req.ItemID)+cited)))
	s.refreshView(req.RepoID)
	writeJSON(w, http.StatusOK, api.CheckChange{Files: files})
}

// releaseChecks answers POST /v1/checks/release: it lifts marks, with a reason.
func (s *Server) releaseChecks(w http.ResponseWriter, r *http.Request) {
	var req api.ReleaseChecks
	if !decode(w, r, &req) {
		return
	}
	by, cited, ok := s.checksActor(w, r, crook.ActionChecksRelease, req.Decision, req.RepoID)
	if !ok {
		return
	}
	paths := cleanPaths(req.Paths)
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("paths names no files"))
		return
	}
	files, err := s.Fold.ReleaseChecks(r.Context(), fold.ReleaseRequest{RepoID: req.RepoID, Paths: paths, Why: req.Why, Force: req.Force})
	if err != nil {
		s.foldError(w, err)
		return
	}
	forced := ""
	if req.Force {
		forced = " (forced)"
	}
	s.Log.Info("check files released", "repo_id", req.RepoID, "paths", paths, "by", by, "force", req.Force, "client", clientName(r))
	var item int64
	if len(files) > 0 {
		item = files[0].ItemID
	}
	s.postFeed(r.Context(), store.FeedChecksReleased, item,
		redact.String(fmt.Sprintf("%d check files released by %s%s: %s. Why: %s", len(files), by, forced, pathList(files), strings.TrimSpace(req.Why))+cited))
	s.refreshView(req.RepoID)
	writeJSON(w, http.StatusOK, api.CheckChange{Files: files})
}

func itemWords(id int64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf(" (next-up item %d)", id)
}
