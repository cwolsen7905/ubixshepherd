package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PathLaneArtifacts is GET /v1/lanes/{id}/artifacts: the files of an open lane that match
// the repo's artifact globs (artifacts.discover), from its untracked, uncommitted and
// committed lists. Read-only; for the desk and browser sessions. The answer is a
// fold.LaneArtifacts.
func PathLaneArtifacts(id int64) string {
	return PathLanes + "/" + strconv.FormatInt(id, 10) + "/artifacts"
}

// PathLaneArtifactContent is GET /v1/lanes/{id}/artifacts/content?path=PATH: a live file's
// bytes, text redacted and capped, with headers that keep it from running in the app's
// origin.
func PathLaneArtifactContent(id int64, rel string) string {
	return PathLaneArtifacts(id) + "/content?path=" + url.QueryEscape(rel)
}

func laneArtifactError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, fold.ErrRefused):
		code = http.StatusConflict
	case errors.Is(err, store.ErrNotFound):
		code = http.StatusNotFound
	}
	conversationError(w, code, err)
}

// LaneArtifacts serves PathLaneArtifacts from f.
func LaneArtifacts(f *fold.Fold) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			conversationError(w, http.StatusBadRequest, errors.New("bad lane id"))
			return
		}
		a, err := f.Artifacts(r.Context(), id)
		if err != nil {
			laneArtifactError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(a)
	}
}

// LaneArtifactContent serves PathLaneArtifactContent from f.
func LaneArtifactContent(f *fold.Fold) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			conversationError(w, http.StatusBadRequest, errors.New("bad lane id"))
			return
		}
		c, err := f.ArtifactContent(r.Context(), id, r.URL.Query().Get("path"))
		if err != nil {
			laneArtifactError(w, err)
			return
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
}
