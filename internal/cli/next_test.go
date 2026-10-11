package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shepherd next: add, approve and order items, link one to a lane at lane open (by
// --item, or by a name hint that matches exactly one), and see it done when the lane's
// request merged. The next_* tools run the same commands.
func TestNextCLIAndMCP(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root

	if code := h.run("next"); code != 0 || !strings.Contains(h.out.String(), "Nothing on the next-up list") {
		t.Fatalf("empty list: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("next", "add", "Step two", "--repo", "app", "--plan", "docs/x.md", "--lane", "feat/two", "--approved"); code != 0 {
		t.Fatalf("add: %s", h.err)
	}
	if code := h.run("next", "add", "Step three", "--repo", "app"); code != 0 {
		t.Fatalf("add: %s", h.err)
	}
	h.run("next", "move", "2", "top")
	h.run("next", "list", "--json")
	var l api.NextList
	if err := json.Unmarshal(h.out.Bytes(), &l); err != nil || len(l.Items) != 2 || l.Items[0].Title != "Step two" || l.Items[1].State != store.NextPlanned || l.NextUp != 1 {
		t.Fatalf("list: %v %+v", err, l)
	}
	if code := h.run("next", "add", "x", "--plan", "/abs"); code == 0 {
		t.Error("an absolute plan path was accepted")
	}
	if code := h.run("next", "move", "1", "sideways"); code == 0 {
		t.Error("a bad move was accepted")
	}
	if code := h.run("next", "show", "1"); code != 0 || !strings.Contains(h.out.String(), "docs/x.md") {
		t.Errorf("show: %s%s", h.out, h.err)
	}

	// Opened by name hint: exactly one item of the repo names the lane.
	if code := h.run("lane", "open", "feat/two", "--repo", "app", "--scope", "src/**"); code != 0 {
		t.Fatalf("open: %s", h.err)
	}
	if !strings.Contains(h.out.String(), "linked to next-up item 1") {
		t.Errorf("no link note:\n%s", h.out)
	}
	h.run("next", "show", "1", "--json")
	var v api.NextItemView
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || v.State != store.NextStarted || v.LaneName != "feat/two" {
		t.Fatalf("started: %v %+v", err, v)
	}
	// An item whose lane is open takes no second lane.
	if code := h.run("lane", "open", "other", "--repo", "app", "--scope", "docs/**", "--item", "1"); code == 0 {
		t.Error("a second lane opened for a started item")
	}
	if code := h.run("lane", "open", "other", "--repo", "app", "--scope", "docs/**", "--item", "99"); code == 0 {
		t.Error("a lane opened for an unknown item")
	}
	// --item links an item whatever the lane is called.
	if code := h.run("lane", "open", "feat/three", "--repo", "app", "--scope", "lib/**", "--item", "2"); code != 0 {
		t.Fatalf("open --item: %s", h.err)
	}
	h.run("next", "show", "2", "--json")
	v = api.NextItemView{}
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || v.State != store.NextStarted || v.LaneName != "feat/three" || v.Repo != "app" {
		t.Fatalf("--item: %v %+v", err, v)
	}

	// The merge closes the lane; the list says done and keeps the MR.
	ctx := context.Background()
	st := h.srv.Store
	items, _ := st.NextItems(ctx)
	for _, it := range items {
		if it.ID == 1 {
			st.PutLaneForge(ctx, store.LaneForge{LaneID: it.LaneID, MR: 12, MRState: "merged"})
			st.SetLaneState(ctx, it.LaneID, store.LaneClosed)
		}
	}
	h.run("next", "show", "1", "--json")
	v = api.NextItemView{}
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || v.State != store.NextDone || v.MR != "!12" || v.DoneAt == nil {
		t.Fatalf("done: %v %+v", err, v)
	}
	if code := h.run("next", "list"); code != 0 || strings.Contains(h.out.String(), "Step two") {
		t.Errorf("done item listed:\n%s", h.out)
	}

	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"next_add","arguments":{"title":"From the desk","note":"n"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"next_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"next_update","arguments":{"id":3,"state":"deferred","move":"top"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"next_update","arguments":{"id":3}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "added") {
		t.Errorf("next_add: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "From the desk") || !strings.Contains(text, "Step three") {
		t.Errorf("next_list: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[2]); isErr || !strings.Contains(text, "updated") {
		t.Errorf("next_update: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[3]); !isErr {
		t.Error("next_update with nothing to change accepted")
	}

	// Seeding adds the plan's items once.
	if code := h.run("next", "seed", "--dry-run"); code != 0 || !strings.Contains(h.out.String(), "would add 19 items") {
		t.Errorf("seed --dry-run: %s%s", h.out, h.err)
	}
	if code := h.run("next", "seed"); code != 0 {
		t.Fatalf("seed: %s", h.err)
	}
	if code := h.run("next", "seed"); code != 0 || !strings.Contains(h.out.String(), "added 0 items; 19 already") {
		t.Errorf("seed again: %s", h.out)
	}
}
