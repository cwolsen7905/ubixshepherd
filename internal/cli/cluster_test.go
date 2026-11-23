package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

// member add, list, revoke and remove on a hub, and cluster status, are clients of the
// daemon's cluster routes.
func TestMemberAndClusterCLI(t *testing.T) {
	h := newHarness(t, "", false)
	if code := h.run("cluster", "status"); code != 0 || !strings.Contains(h.out.String(), "alone") {
		t.Fatalf("status of a daemon on its own: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("member", "add", "work"); code == 0 {
		t.Fatal("member add worked on a daemon that is no hub")
	}
	h.srv.Config.Cluster.Role = config.ClusterRoleHub

	if code := h.run("member", "add", "work"); code != 0 {
		t.Fatalf("member add: %d %s%s", code, h.out, h.err)
	}
	first := strings.Fields(h.out.String())[0]
	if n := len(strings.Split(first, "-")); n != 8 {
		t.Fatalf("the code %q is not eight words", first)
	}
	if code := h.run("member", "add", "Bad Name"); code == 0 {
		t.Error("a bad name was accepted")
	}
	if code := h.run("member", "list"); code != 0 || !strings.Contains(h.out.String(), "work") || !strings.Contains(h.out.String(), "waiting to enrol") {
		t.Fatalf("list: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("cluster", "status", "--json"); code != 0 {
		t.Fatalf("status: %s", h.err)
	}
	var c api.Cluster
	if err := json.Unmarshal(h.out.Bytes(), &c); err != nil || c.Role != api.RoleHub || len(c.Members) != 1 || c.Members[0].Online || c.DaemonID == "" {
		t.Fatalf("status json: %v %+v", err, c)
	}
	if code := h.run("member", "remove", "work"); code != 0 {
		t.Fatalf("remove: %s", h.err)
	}
	if code := h.run("member", "remove", "work"); code == 0 {
		t.Error("removing a member twice worked")
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
		return
	}
	t.Fatal("no cluster_status tool")
}
