package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// gate is the keep-going gate (docs/keep-going.md, section 7). It reports whether the
// request may go ahead; Crook decides. Only the front desk in a turn the person did not
// start (an event turn) is held to keep-going mode: a person-started desk turn, the CLI
// and the web are the person's own, and pass. When the gate refuses it has written a 403
// with Allow's reason, which the desk relays as the tool's result.
func (s *Server) gate(w http.ResponseWriter, r *http.Request, ws int64, action string, lane int64) bool {
	v := s.gateVerdict(r.Context(), ws, action, lane)
	if v.OK() {
		return true
	}
	writeError(w, http.StatusForbidden, errors.New(v.Reason))
	return false
}

// gateVerdict is Crook's answer for the caller in ctx.
func (s *Server) gateVerdict(ctx context.Context, ws int64, action string, lane int64) crook.Verdict {
	return crook.Check(ctx, crook.Request{
		Action: crook.KeepGoingAction(action),
		Actor:  principalOf(ctx).actor(),
		Facts: crook.Facts{KeepGoing: func(ctx context.Context) (bool, string) {
			d := s.KeepGoing().Allow(ctx, ws, action, lane)
			return d.OK, d.Reason
		}},
	})
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
