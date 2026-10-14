package daemon

import (
	"errors"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// gate is the keep-going gate (docs/keep-going.md, section 7). It reports whether the
// request may go ahead. Only the front desk in a turn the person did not start (an event
// turn) is held to keep-going mode: a person-started desk turn, the CLI and the web are
// the person's own, and pass. When the gate refuses it has written a 403 with Allow's
// reason, which the desk relays as the tool's result.
func (s *Server) gate(w http.ResponseWriter, r *http.Request, ws int64, action string, lane int64) bool {
	p := principalOf(r.Context())
	if p.Role != RoleDesk || p.Human {
		return true
	}
	d := s.KeepGoing().Allow(r.Context(), ws, action, lane)
	if d.OK {
		return true
	}
	writeError(w, http.StatusForbidden, errors.New(d.Reason))
	return false
}

// workspaceOfRepo is the workspace a repo is in, 0 when the repo is unknown.
func (s *Server) workspaceOfRepo(r *http.Request, repoID int64) int64 {
	repo, err := s.Store.Repo(r.Context(), repoID)
	if err != nil {
		return 0
	}
	return repo.WorkspaceID
}

// fixAction is the keep-going action a suggested fix is: a retry starts the run over
// (rerun), anything else follows it up (fix).
func (s *Server) fixAction(r *http.Request, runID int64, fixID string) string {
	ds, err := s.Store.Diagnoses(r.Context(), runID)
	if err == nil {
		for _, d := range ds {
			for _, f := range d.Fixes {
				if f.ID == fixID && (f.Op == store.FixRetry || f.Op == store.FixRetryOther) {
					return KeepRerun
				}
			}
		}
	}
	return KeepFix
}
