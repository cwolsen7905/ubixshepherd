package daemon

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/fold"
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
	// The declared file is copied at once: the agent may rewrite it before the run ends.
	s.Fold.SnapshotKnown(r.Context(), run.LaneID)
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

func pathArtifactID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad artifact id"))
		return 0, false
	}
	return id, true
}

// artifactContent serves GET /v1/artifacts/{id}/content?version=SHA: the bytes of a
// kept artifact, from its snapshot, or live from the worktree while the lane is open
// and no version is asked for. The headers are the lane read's: nothing here runs in
// the app's origin.
func (s *Server) artifactContent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathArtifactID(w, r)
	if !ok {
		return
	}
	a, err := s.Store.Artifact(r.Context(), id)
	if err != nil {
		s.foldError(w, err)
		return
	}
	version := r.URL.Query().Get("version")
	var c fold.ArtifactContent
	if version == "" && a.State == store.ArtifactLive {
		c, err = s.Fold.ArtifactContent(r.Context(), a.LaneID, a.Path)
	}
	if version != "" || a.State != store.ArtifactLive || err != nil {
		var serr error
		c, serr = s.Fold.ArtifactSnapshot(r.Context(), id, version)
		if serr != nil {
			if err == nil || !errors.Is(serr, store.ErrNotFound) {
				err = serr
			}
			s.foldError(w, err)
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", c.Mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Content-Disposition", "inline")
	h.Set("Cache-Control", "no-store")
	if c.Redacted > 0 {
		h.Set("X-Shepherd-Redacted", strconv.Itoa(c.Redacted))
	}
	w.Write(c.Data)
}

// ArtifactPatchBody is PATCH /v1/artifacts/{id}: the person's changes. Move "keep" moves
// an untracked file into the Shepherd directory.
type ArtifactPatchBody struct {
	Intent *string `json:"intent,omitempty"`
	Pinned *bool   `json:"pinned,omitempty"`
	Title  *string `json:"title,omitempty"`
	Move   string  `json:"move,omitempty"`
}

func (s *Server) patchArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathArtifactID(w, r)
	if !ok {
		return
	}
	var req ArtifactPatchBody
	if !decode(w, r, &req) {
		return
	}
	a, err := s.Fold.PatchArtifact(r.Context(), id, fold.ArtifactPatch{Intent: req.Intent, Pinned: req.Pinned, Title: req.Title, Move: req.Move})
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// discardArtifact serves POST /v1/artifacts/{id}/discard: the person's own call, never
// the desk's or an agent's.
func (s *Server) discardArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathArtifactID(w, r)
	if !ok {
		return
	}
	a, err := s.Fold.DiscardArtifact(r.Context(), id)
	if err != nil {
		s.foldError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// ArtifactGCBody is POST /v1/artifacts/gc; zero fields take the defaults.
type ArtifactGCBody struct {
	KeepDays int   `json:"keep_days,omitempty"`
	CapBytes int64 `json:"cap_bytes,omitempty"`
	DryRun   bool  `json:"dry_run,omitempty"`
}

func (s *Server) artifactGC(w http.ResponseWriter, r *http.Request) {
	var req ArtifactGCBody
	if !decode(w, r, &req) {
		return
	}
	res, err := s.Fold.ArtifactGC(r.Context(), req.KeepDays, req.CapBytes, req.DryRun, time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
