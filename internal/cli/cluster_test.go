package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

// worker add, list, revoke and remove on a controller, and cluster status, are clients
// of the daemon's cluster routes.
func TestWorkerAndClusterCLI(t *testing.T) {
	h := newHarness(t, "", false)
	if code := h.run("cluster", "status"); code != 0 || !strings.Contains(h.out.String(), "alone") {
		t.Fatalf("status of a daemon on its own: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("worker", "add", "work"); code == 0 {
		t.Fatal("worker add worked on a daemon that is no controller")
	}
	h.srv.Config.Cluster.Role = config.ClusterRoleController

	if code := h.run("worker", "add", "work"); code != 0 {
		t.Fatalf("worker add: %d %s%s", code, h.out, h.err)
	}
	first := strings.Fields(h.out.String())[0]
	if n := len(strings.Split(first, "-")); n != 8 {
		t.Fatalf("the code %q is not eight words", first)
	}
	if code := h.run("worker", "add", "Bad Name"); code == 0 {
		t.Error("a bad name was accepted")
	}
	if code := h.run("worker", "list"); code != 0 || !strings.Contains(h.out.String(), "work") || !strings.Contains(h.out.String(), "waiting to enrol") {
		t.Fatalf("list: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("cluster", "status"); code != 0 || !strings.HasPrefix(h.out.String(), "controller ") {
		t.Fatalf("status: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("cluster", "status", "--json"); code != 0 {
		t.Fatalf("status: %s", h.err)
	}
	var c api.Cluster
	if err := json.Unmarshal(h.out.Bytes(), &c); err != nil || c.Role != api.RoleController || len(c.Workers) != 1 || c.Workers[0].Online || c.DaemonID == "" {
		t.Fatalf("status json: %v %+v", err, c)
	}
	if !strings.Contains(h.out.String(), `"workers"`) {
		t.Fatalf("status json has no workers key: %s", h.out)
	}
	if code := h.run("worker", "remove", "work"); code != 0 {
		t.Fatalf("remove: %s", h.err)
	}
	if code := h.run("worker", "remove", "work"); code == 0 {
		t.Error("removing a worker twice worked")
	}
}

// shepherd member is the old name of the machine subcommands: hidden, it says so on
// stderr and runs the same thing.
func TestMemberIsAnAliasOfWorker(t *testing.T) {
	h := newHarness(t, "", false)
	h.srv.Config.Cluster.Role = config.ClusterRoleController
	if code := h.run("member", "add", "home"); code != 0 {
		t.Fatalf("member add: %d %s%s", code, h.out, h.err)
	}
	if !strings.Contains(h.err.String(), "member is now worker: shepherd worker add home") {
		t.Fatalf("no note on stderr: %q", h.err)
	}
	if code := h.run("worker", "list"); code != 0 || !strings.Contains(h.out.String(), "home") {
		t.Fatalf("the worker added as a member is not listed: %d %s", code, h.out)
	}
	if code := h.run("member", "list"); code != 0 || !strings.Contains(h.out.String(), "home") {
		t.Fatalf("member list: %d %s%s", code, h.out, h.err)
	}
}

// Help lists worker with its machine subcommands, and neither member nor the agents'
// subcommands of worker.
func TestHelpShowsWorkerMachinesOnly(t *testing.T) {
	h := newHarness(t, "", false)
	if code := h.run("help"); code != 0 {
		t.Fatal(code)
	}
	out := h.out.String()
	if !strings.Contains(out, "  worker    On a controller: enrol the machines") {
		t.Fatalf("help has no worker line:\n%s", out)
	}
	for _, hidden := range []string{"  member ", "ask-human", "ask-shepherd", "hub"} {
		if strings.Contains(out, hidden) {
			t.Errorf("help shows %q:\n%s", hidden, out)
		}
	}
	if code := h.run("worker"); code != 2 || !strings.Contains(h.err.String(), "shepherd worker add <name>") || strings.Contains(h.err.String(), "report") {
		t.Fatalf("worker usage: %d %s", code, h.err)
	}
}

func TestClusterStatusTool(t *testing.T) {
	for _, tl := range mcpTools() {
		if tl.Name != "cluster_status" {
			continue
		}
		args, err := tl.args(map[string]any{})
		if err != nil || strings.Join(args, " ") != "cluster status --json" {
			t.Fatalf("args: %v %v", args, err)
		}
		if strings.Contains(tl.Description, "hub") || strings.Contains(tl.Description, "member") {
			t.Fatalf("description uses the old names: %s", tl.Description)
		}
		return
	}
	t.Fatal("no cluster_status tool")
}
