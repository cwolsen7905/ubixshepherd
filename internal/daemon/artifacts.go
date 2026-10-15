package daemon

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// declareArtifact serves POST /v1/runs/{id}/artifacts: an agent names a file of its
// lane for the person to read. The route is the run's own (ownRun), so a run cannot
// declare in another lane.
func (s *Server) declareArtifact(w http.ResponseWriter, r *http.Request) {
	run, ok := s.pathRun(w, r)
	if !ok {
		return
	}
	var req api.ArtifactDeclare
	if !decode(w, r, &req) {
		return
	}
	a, created, added, err := s.Runner.DeclareArtifact(r.Context(), run.ID, req.Path, req.Title, req.Intent)
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ArtifactDeclared{Artifact: a, New: created, Version: added})
}

// listArtifacts serves GET /v1/artifacts: the stored artifacts, newest first.
func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f store.ArtifactFilter
	for name, dst := range map[string]*int64{"workspace_id": &f.WorkspaceID, "lane_id": &f.LaneID, "run_id": &f.RunID,
		"topic_id": &f.TopicID, "decision_id": &f.DecisionID} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, errors.New("bad "+name))
				return
			}
			*dst = n
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, errors.New("bad limit"))
			return
		}
		f.Limit = n
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("bad since: want RFC 3339"))
			return
		}
		f.Since = t
	}
	f.State, f.Kind = q.Get("state"), q.Get("kind")
	list, err := s.Store.ListArtifacts(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// getArtifact serves GET /v1/artifacts/{id}: one artifact with versions and links.
func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad artifact id"))
		return
	}
	a, err := s.Store.Artifact(r.Context(), id)
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
