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

// The keep_going tool maps onto the CLI command, and keeps off allowed and the flags to on.
func TestKeepGoingTool(t *testing.T) {
	var tool mcpTool
	for _, tl := range mcpTools() {
		if tl.Name == "keep_going" {
			tool = tl
		}
	}
	if tool.args == nil {
		t.Fatal("no keep_going tool")
	}
	for _, c := range []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"action": "status"}, "keep-going status"},
		{map[string]any{"action": "off", "cap": 9.0}, "keep-going off"},
		{map[string]any{"action": "on", "cap": 2.5, "runs": 1.0, "idle": 3.0}, "keep-going on --cap 2.5 --runs 1 --idle 3"},
	} {
		got, err := tool.args(c.in)
		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("args(%v) = %v, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := tool.args(map[string]any{"action": "sideways"}); err == nil {
		t.Error("an unknown action was accepted")
	}

	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"keep_going","arguments":{"action":"status"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"keep_going","arguments":{"action":"off"}}}`,
	)
	for i, r := range resps {
		if text, isErr := toolText(t, r); isErr || !strings.Contains(text, "keep-going: off") {
			t.Errorf("call %d: %v %s", i, isErr, text)
		}
	}
}

func TestWriteKeepGoingListsRecent(t *testing.T) {
	var b strings.Builder
	writeKeepGoing(&b, api.KeepGoingStatus{Recent: []api.KeepGoingAction{{Text: "keep-going is off: x"}}})
	if !strings.Contains(b.String(), "recent:") || !strings.Contains(b.String(), "keep-going is off: x") {
		t.Errorf("output: %q", b.String())
	}
}
