package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// shepherd keep-going on, status and off are a client of the daemon's endpoint.
func TestKeepGoingCLI(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root

	if code := h.run("keep-going", "status"); code != 0 || !strings.Contains(h.out.String(), "keep-going: off") {
		t.Fatalf("status off: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("keep-going", "on", "--cap", "3", "--runs", "1", "--idle", "2"); code != 0 || !strings.Contains(h.out.String(), "keep-going: on") {
		t.Fatalf("on: %d %s%s", code, h.out, h.err)
	}
	h.run("keep-going", "status", "--json")
	var st api.KeepGoingStatus
	if err := json.Unmarshal(h.out.Bytes(), &st); err != nil || !st.On || st.CapUSD != 3 || st.MaxRuns != 1 || st.IdleHours != 2 || st.By != "cli" {
		t.Fatalf("status: %v %+v", err, st)
	}
	if code := h.run("keep-going", "off"); code != 0 || !strings.Contains(h.out.String(), "keep-going: off") || !strings.Contains(h.out.String(), "turned off by the person") {
		t.Fatalf("off: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("keep-going", "off", "--cap", "3"); code == 0 {
		t.Error("--cap with off was accepted")
	}
	if code := h.run("keep-going", "on", "--cap", "-1"); code == 0 {
		t.Error("a negative cap was accepted")
	}
	if code := h.run("keep-going", "sideways"); code == 0 {
		t.Error("an unknown subcommand was accepted")
	}
}
