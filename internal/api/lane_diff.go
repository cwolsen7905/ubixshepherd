package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PathLaneDiff is GET /v1/lanes/{id}/diff: what the lane holds against its base. Read-only;
// open to the operator, the desk and browser sessions, refused to an agent's worker
// token (an agent reads its own tree itself).
//
//	mode=stat|files|file   how much (default stat; file when file is set)
//	file=PATH              one file's patch, or an untracked file's start
//	since=SHA&until=SHA    narrow the commits and committed diff to a range
//
// The answer is a fold.LaneDiff, already bounded and redacted.
func PathLaneDiff(id int64, req fold.DiffRequest) string {
	q := url.Values{}
	if req.Mode != "" {
		q.Set("mode", req.Mode)
	}
	if req.File != "" {
		q.Set("file", req.File)
	}
	if req.Since != "" {
		q.Set("since", req.Since)
	}
	if req.Until != "" {
		q.Set("until", req.Until)
	}
	p := fmt.Sprintf("%s/%d/diff", PathLanes, id)
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p
}

// LaneDiff serves PathLaneDiff from f; the daemon registers it on
// "GET /v1/lanes/{id}/diff".
func LaneDiff(f *fold.Fold) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			conversationError(w, http.StatusBadRequest, errors.New("bad lane id"))
			return
		}
		q := r.URL.Query()
		d, err := f.Diff(r.Context(), id, fold.DiffRequest{
			Mode: q.Get("mode"), File: q.Get("file"), Since: q.Get("since"), Until: q.Get("until"),
		})
		if err != nil {
			code := http.StatusInternalServerError
			switch {
			case errors.Is(err, fold.ErrRefused):
				code = http.StatusConflict
			case errors.Is(err, store.ErrNotFound):
				code = http.StatusNotFound
			}
			conversationError(w, code, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(d)
	}
}
