package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// renameWorkspace answers POST /v1/workspaces/{id}/rename: it changes the name and
// nothing else. The person's call, so the operator's only (a desk or a worker never
// renames what it works in).
func (s *Server) renameWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("workspace id %q: not a number", r.PathValue("id")))
		return
	}
	var req api.RenameWorkspace
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || strings.ContainsAny(req.Name, `/\`) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("workspace name %q: must be non-empty with no slashes", req.Name))
		return
	}
	ws, err := s.Store.RenameWorkspace(r.Context(), id, req.Name)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err)
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
		return
	case err != nil:
		s.fail(w, err)
		return
	}
	repos, err := s.Store.Repos(r.Context(), ws.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("workspace renamed", "id", ws.ID, "name", ws.Name, "path", ws.Path)
	writeJSON(w, http.StatusOK, api.WorkspaceDetail{Workspace: ws, Repos: repos})
}
