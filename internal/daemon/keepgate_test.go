package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// gateCall runs a handler as a principal and returns the status and body.
func gateCall(h http.HandlerFunc, p principal, path, body string, runID string) (int, string) {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	if runID != "" {
		r.SetPathValue("id", runID)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w.Code, w.Body.String()
}

func TestKeepGateHoldsOnlyEventTurns(t *testing.T) {
	s, _, _, ws, lane := keepSetup(t)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: filepath.Join(t.TempDir(), "runs"), Log: s.Log}
	run, err := s.Store.CreateRun(context.Background(), store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunFailed})
	if err != nil {
		t.Fatal(err)
	}
	startBody := `{"lane_id":` + itoa(lane.ID) + `,"prompt":"go"}`
	fixBody := `{"action":"nope"}`
	event := principal{Role: RoleDesk}

	// Mode off: an event turn is refused with Allow's reason, at both call sites.
	for name, call := range map[string]func(principal) (int, string){
		"start": func(p principal) (int, string) { return gateCall(s.startRun, p, "/v1/runs", startBody, "") },
		"fix":   func(p principal) (int, string) { return gateCall(s.fixRun, p, "/v1/runs/1/fix", fixBody, itoa(run.ID)) },
	} {
		code, out := call(event)
		if code != http.StatusForbidden || !strings.Contains(out, "keep-going is off") {
			t.Errorf("%s event turn off: %d %s", name, code, out)
		}
		// A person-started desk turn, the operator and the web are not gated.
		for _, p := range []principal{{Role: RoleDesk, Human: true}, {Role: RoleOperator}} {
			if code, out := call(p); code == http.StatusForbidden && strings.Contains(out, "keep-going") {
				t.Errorf("%s %s gated: %d %s", name, p.Role, code, out)
			}
		}
	}

	// Mode on: the event turn passes the gate, and a start counts against the lane.
	keepOn(t, s, ws, api.KeepGoingSet{})
	before, _ := s.Store.LaneStarts(context.Background(), lane.ID, "2026-10-11")
	code, out := gateCall(s.startRun, event, "/v1/runs", startBody, "")
	if code == http.StatusForbidden && strings.Contains(out, "keep-going") {
		t.Fatalf("on: gated: %d %s", code, out)
	}
	after, _ := s.Store.LaneStarts(context.Background(), lane.ID, "2026-10-11")
	if after != before+1 {
		t.Fatalf("lane starts %d -> %d, want one more", before, after)
	}
}

func TestKeepGateOpenLaneItemOnly(t *testing.T) {
	s, _, _, _, lane := keepSetup(t)
	event := principal{Role: RoleDesk}
	// With an item, an event turn is gated (mode off).
	body := `{"repo_id":` + itoa(lane.RepoID) + `,"name":"x","item":1}`
	code, out := gateCall(s.openLane, event, "/v1/lanes", body, "")
	if code != http.StatusForbidden || !strings.Contains(out, "keep-going is off") {
		t.Fatalf("item: %d %s", code, out)
	}
	// Without an item it is not the gate's: the desk opens a lane as before.
	body = `{"repo_id":` + itoa(lane.RepoID) + `,"name":"x"}`
	if code, out := gateCall(s.openLane, event, "/v1/lanes", body, ""); code == http.StatusForbidden && strings.Contains(out, "keep-going") {
		t.Fatalf("no item gated: %d %s", code, out)
	}
}
