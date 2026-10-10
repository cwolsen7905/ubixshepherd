package daemon

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// failedRun gives run a a diagnosis with one fix of each kind of standing: the
// person's, the desk's and open to anyone.
func failedRun(t *testing.T, s *Server, run store.Run) {
	t.Helper()
	ctx := context.Background()
	run.State = store.RunFailed
	if err := s.Store.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	_, err := s.Store.AddDiagnosis(ctx, store.Diagnosis{
		RunID: run.ID, Kind: store.DiagQuota, Summary: "claude hit its usage limit", Evidence: "limit reached\nreset at noon",
		Fixes: []store.Fix{
			{ID: "wait", Label: "Wait for the quota", Op: store.FixWaitQuota},
			{ID: "other", Label: "Retry with another agent", Op: store.FixRetryOther, NeedsPerson: true, Args: map[string]string{"agent": "cursor"}},
			{ID: "log", Label: "Open the log", Op: store.FixOpenLog, DeskOK: true, Args: map[string]string{"path": run.Log}},
		},
		Created: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunDiagnosisOnViewsAndFixAuthorization(t *testing.T) {
	s, ts := newServer(t)
	a, b, la, _ := twoRuns(t, s)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
	failedRun(t, s, a)

	// The latest diagnosis rides on the run's view, with its evidence on a single run's
	// and not in a list.
	var one api.RunView
	if code := call(t, ts, s.Token, "GET", api.PathRun(a.ID), nil, &one); code != http.StatusOK || one.Diagnosis == nil {
		t.Fatalf("run view: %d %+v", code, one.Diagnosis)
	}
	if one.Diagnosis.Kind != store.DiagQuota || !strings.Contains(one.Diagnosis.Evidence, "limit reached") || len(one.Diagnosis.Fixes) != 3 {
		t.Errorf("single run's diagnosis = %+v", one.Diagnosis)
	}
	var list []api.RunView
	call(t, ts, s.Token, "GET", api.PathRuns, nil, &list)
	for _, r := range list {
		switch {
		case r.ID == a.ID && (r.Diagnosis == nil || r.Diagnosis.Evidence != "" || !r.Diagnosis.EvidenceOmitted):
			t.Errorf("listed run %d: %+v, want a diagnosis without its evidence", r.ID, r.Diagnosis)
		case r.ID == b.ID && r.Diagnosis != nil:
			t.Errorf("run %d has no diagnosis, got %+v", r.ID, r.Diagnosis)
		}
	}
	var lanes []api.LaneView
	call(t, ts, s.Token, "GET", api.PathLanes+"?workspace_id=1", nil, &lanes)
	for _, l := range lanes {
		if (l.ID == la.ID) != (l.Diagnosis != nil) {
			t.Errorf("lane %s diagnosis = %+v", l.Name, l.Diagnosis)
		}
	}
	var all []api.Diagnosis
	if code := call(t, ts, s.Token, "GET", api.PathRunDiagnoses(a.ID), nil, &all); code != http.StatusOK || len(all) != 1 || all[0].Evidence == "" {
		t.Errorf("diagnoses: %d %+v", code, all)
	}

	worker, _ := s.MintWorker(a.ID)
	desk, _ := s.MintDesk(true, "")
	fix := func(tok, action string) int {
		return call(t, ts, tok, "POST", api.PathRunFix(a.ID), api.RunFix{Action: action}, nil)
	}
	// An agent's token has no part in this, not even on its own run.
	if code := fix(worker, "wait"); code != http.StatusForbidden {
		t.Errorf("worker takes a fix: %d, want 403", code)
	}
	if code := call(t, ts, worker, "GET", api.PathRunDiagnoses(a.ID), nil, nil); code != http.StatusForbidden {
		t.Errorf("worker reads diagnoses: %d, want 403", code)
	}
	// The desk takes only what is marked for it; the person's fixes are refused.
	if code := fix(desk, "other"); code != http.StatusConflict {
		t.Errorf("desk takes a needs_person fix: %d, want 409", code)
	}
	if code := fix(desk, "wait"); code != http.StatusConflict {
		t.Errorf("desk takes a fix not marked desk_ok: %d, want 409", code)
	}
	if code := call(t, ts, desk, "POST", api.PathRunFix(a.ID), api.RunFix{Action: "log", Overrides: map[string]string{"path": "/etc/passwd"}}, nil); code != http.StatusConflict {
		t.Errorf("desk edits a fix: %d, want 409", code)
	}
	var res dispatch.FixResult
	if code := call(t, ts, desk, "POST", api.PathRunFix(a.ID), api.RunFix{Action: "log"}, &res); code != http.StatusOK || !res.Done {
		t.Errorf("desk takes its own fix: %d %+v", code, res)
	}
	// The operator is the person; an unknown fix is a refusal, not a crash.
	if code := call(t, ts, s.Token, "POST", api.PathRunFix(a.ID), api.RunFix{Action: "wait"}, &res); code != http.StatusOK || !res.Done {
		t.Errorf("operator takes a fix: %d %+v", code, res)
	}
	if code := fix(s.Token, "nope"); code != http.StatusConflict {
		t.Errorf("unknown fix: %d, want 409", code)
	}
	if code := fix(s.Token, ""); code != http.StatusBadRequest {
		t.Errorf("no action: %d, want 400", code)
	}
}

// A browser session is the person: it may take a needs_person fix.
func TestWebSessionTakesPersonsFix(t *testing.T) {
	var run store.Run
	s, base, _ := webDaemon(t, func(s *Server) {
		run, _, _, _ = twoRuns(t, s)
		s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
		failedRun(t, s, run)
	})
	b := newBrowser(t, base)
	signIn(t, s, b)
	resp, body := b.do("GET", api.PathRunDiagnoses(run.ID), nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "usage limit") {
		t.Fatalf("diagnoses: %d %s", resp.StatusCode, body)
	}
	// "other" starts a run, which fails here for want of a lane worktree: past the
	// authorization all the same, which is what this checks.
	resp, body = b.do("POST", api.PathRunFix(run.ID), api.RunFix{Action: "wait"})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"done":true`) {
		t.Errorf("web takes a fix: %d %s", resp.StatusCode, body)
	}
	resp, body = b.do("POST", api.PathRunFix(run.ID), api.RunFix{Action: "other"})
	if denied(resp.StatusCode) {
		t.Errorf("web takes a needs_person fix: %d %s", resp.StatusCode, body)
	}
}
