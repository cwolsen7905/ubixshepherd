package daemon

import (
	"context"
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

func (s *Server) patchArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathArtifactID(w, r)
	if !ok {
		return
	}
	var req api.ArtifactPatch
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

// artifactGC serves POST /v1/artifacts/gc. A field left zero takes the configured
// retention.
func (s *Server) artifactGC(w http.ResponseWriter, r *http.Request) {
	var req api.ArtifactGC
	if !decode(w, r, &req) {
		return
	}
	days, capBytes := s.LiveConfig().ArtifactRetention()
	if req.KeepDays > 0 {
		days = req.KeepDays
	}
	if req.CapBytes > 0 {
		capBytes = req.CapBytes
	}
	res, err := s.Fold.ArtifactGC(r.Context(), days, capBytes, req.DryRun, time.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ArtifactRetention applies the snapshots' retention at start and then once a day, with
// the configured keep_days and cap (read afresh each time, so a reload counts). The
// process that runs the daemon starts it in a goroutine.
func (s *Server) ArtifactRetention(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		days, capBytes := s.LiveConfig().ArtifactRetention()
		if res, err := s.Fold.ArtifactGC(ctx, days, capBytes, false, time.Now()); err != nil {
			s.Log.Warn("artifact retention", "err", err)
		} else if res.Expired+res.Evicted+res.Orphans > 0 {
			s.Log.Info("artifact retention", "expired", res.Expired, "evicted", res.Evicted, "orphans", res.Orphans, "freed_bytes", res.FreedByte)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
