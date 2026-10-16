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

// done_when and depends_on go through the CLI flags and the tools that map onto them; an
// item is blocked on a dependency that is not done, and a suggestion is dismissed with
// --dismiss.
func TestNextGuardFieldsCLIAndMCP(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	item := func(id string) api.NextItemView {
		t.Helper()
		if code := h.run("next", "show", id, "--json"); code != 0 {
			t.Fatalf("show %s: %s", id, h.err)
		}
		var v api.NextItemView
		if err := json.Unmarshal(h.out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if code := h.run("next", "add", "First", "--repo", "app", "--approved", "--done-when", "it lists as done"); code != 0 {
		t.Fatalf("add: %s", h.err)
	}
	if code := h.run("next", "add", "Second", "--repo", "app", "--approved", "--depends", "1"); code != 0 {
		t.Fatalf("add: %s", h.err)
	}
	if v := item("1"); v.DoneWhen != "it lists as done" || len(v.BlockedOn) != 0 {
		t.Errorf("first: %+v", v)
	}
	if v := item("2"); len(v.DependsOn) != 1 || len(v.BlockedOn) != 1 || v.BlockedOn[0].Title != "First" {
		t.Errorf("second: %+v", v)
	}
	h.run("next", "list", "--json")
	var l api.NextList
	if err := json.Unmarshal(h.out.Bytes(), &l); err != nil || l.NextUp != 1 {
		t.Fatalf("list: %v %+v", err, l)
	}
	if code := h.run("next", "list"); code != 0 || !strings.Contains(h.out.String(), "blocked on #1") {
		t.Errorf("list does not say what blocks: %s", h.out)
	}
	if code := h.run("next", "update", "1", "--depends", "2"); code == 0 {
		t.Error("a dependency cycle was accepted")
	}
	if code := h.run("next", "update", "2", "--depends", "bad"); code == 0 {
		t.Error("a bad --depends was accepted")
	}
	if code := h.run("next", "done", "1"); code != 0 {
		t.Fatalf("done: %s", h.err)
	}
	if v := item("2"); len(v.BlockedOn) != 0 {
		t.Errorf("still blocked after the dependency is done: %+v", v)
	}

	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"next_add","arguments":{"title":"Third","done_when":"works","depends_on":[2]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"next_update","arguments":{"id":3,"done_when":"","depends_on":[]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"next_update","arguments":{"id":3,"depends_on":["x"]}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "added") {
		t.Fatalf("next_add: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "updated") {
		t.Fatalf("next_update: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[2]); !isErr {
		t.Error("next_update with a non-numeric dependency accepted")
	}
	if v := item("3"); v.DoneWhen != "" || len(v.DependsOn) != 0 {
		t.Errorf("cleared: %+v", v)
	}

	sug := "done?: ALREADY DONE"
	if _, err := h.srv.Store.UpdateNextItem(context.Background(), 2, store.NextPatch{Suggestion: &sug}); err != nil {
		t.Fatal(err)
	}
	if v := item("2"); v.Suggestion != sug {
		t.Errorf("suggestion not shown: %+v", v)
	}
	if code := h.run("next", "update", "2", "--dismiss"); code != 0 {
		t.Fatalf("dismiss: %s", h.err)
	}
	if v := item("2"); v.Suggestion != "" {
		t.Errorf("suggestion survived --dismiss: %+v", v)
	}
}
