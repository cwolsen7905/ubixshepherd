package daemon

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// `shepherd run continue` names the run and sends no lane_id. The gate must find the
// run's lane and workspace, not read workspace 0's empty row ("keep-going is off" with
// no reason while the mode is on).
func TestKeepGateContinueByRunUsesRunsWorkspace(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
	run, err := s.Store.CreateRun(context.Background(), store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunFailed})
	if err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	body := `{"continue":` + itoa(run.ID) + `,"prompt":"go"}`
	code, out := gateCall(s.startRun, principal{Role: RoleDesk}, "/v1/runs", body, "")
	if code == http.StatusForbidden && strings.Contains(out, "keep-going") {
		t.Fatalf("mode on, continue by run gated: %d %s", code, out)
	}
	if n, _ := s.Store.LaneStarts(context.Background(), lane.ID, "2026-10-11"); n != 1 {
		t.Fatalf("lane starts = %d, want 1 counted on the run's lane", n)
	}
}

// A refusal always names its reason, including for an unknown workspace or a mode never
// turned on.
func TestKeepAllowRefusalAlwaysHasReason(t *testing.T) {
	s, _, _, ws, _ := keepSetup(t)
	k := s.KeepGoing()
	for _, id := range []int64{0, ws.ID} {
		d := k.Allow(context.Background(), id, KeepContinue, 0)
		if d.OK || d.Reason == "" || d.Reason == "keep-going is off" {
			t.Errorf("workspace %d: %+v, want a refusal with a reason", id, d)
		}
	}
}
