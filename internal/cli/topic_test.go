package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shepherd topic: make, tie, rename, merge, clear and reopen, by id or by title. The
// topic_* tools run the same commands.
func TestTopicCLIAndMCP(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root

	if code := h.run("topic"); code != 0 || !strings.Contains(h.out.String(), "No topics") {
		t.Fatalf("empty: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("topic", "new", "release v0.3.0", "--summary", "tag and notes"); code != 0 {
		t.Fatalf("new: %s", h.err)
	}
	if code := h.run("topic", "new", "release v0.3.0"); code == 0 {
		t.Error("a repeated title was accepted")
	}
	h.run("next", "add", "Write notes", "--repo", "app")
	if code := h.run("topic", "set", "release v0.3.0", "--item", "1"); code != 0 {
		t.Fatalf("set: %s", h.err)
	}
	if code := h.run("topic", "set", "1"); code == 0 {
		t.Error("set with nothing to tie was accepted")
	}
	h.run("topic", "show", "1", "--json")
	var v api.TopicView
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || len(v.Links) != 1 || v.Links[0].Kind != store.TopicNextItem || v.Summary != "tag and notes" {
		t.Fatalf("show: %v %+v", err, v)
	}
	if code := h.run("topic", "rename", "1", "Release 0.3"); code != 0 {
		t.Fatalf("rename: %s", h.err)
	}
	h.run("topic", "new", "post")
	if code := h.run("topic", "merge", "post", "Release 0.3"); code != 0 {
		t.Fatalf("merge: %s", h.err)
	}
	if code := h.run("topic", "clear", "Release 0.3"); code != 0 {
		t.Fatalf("clear: %s", h.err)
	}
	if code := h.run("topic", "list"); code != 0 || strings.Contains(h.out.String(), "Release 0.3") {
		t.Errorf("cleared topic listed:\n%s", h.out)
	}
	if code := h.run("topic", "list", "--all"); code != 0 || !strings.Contains(h.out.String(), "cleared") {
		t.Errorf("--all:\n%s", h.out)
	}
	if code := h.run("topic", "reopen", "1"); code != 0 {
		t.Fatalf("reopen: %s", h.err)
	}

	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"topic_set","arguments":{"title":"manual","item":1}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"topic_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"topic_set","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"topic_show","arguments":{"id":1}}}`,
	)
	// The item is already in another topic and the operator token is the person's.
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "added") {
		t.Errorf("topic_set: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); isErr || !strings.Contains(text, "Release 0.3") || !strings.Contains(text, "manual") {
		t.Errorf("topic_list: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[2]); !isErr {
		t.Error("topic_set with neither id nor title accepted")
	}
	if text, isErr := toolText(t, resps[3]); isErr || !strings.Contains(text, "Release 0.3") {
		t.Errorf("topic_show: %v %s", isErr, text)
	}
}

// topic_set ties a lane by its name (with repo) or its id, and a lane id of 0 is refused
// with a message that says so.
func TestTopicSetLaneByName(t *testing.T) {
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	if code := h.run("lane", "open", "feat/x", "--scope", "src/**"); code != 0 {
		t.Fatalf("lane open: %s", h.err)
	}
	h.env.Cwd = root
	h.run("topic", "new", "login")
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"topic_set","arguments":{"id":1,"lane":"feat/x","repo":"app"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"topic_set","arguments":{"id":1,"lane":0}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "updated") {
		t.Errorf("topic_set lane by name: %v %s", isErr, text)
	}
	if _, isErr := toolText(t, resps[1]); !isErr {
		t.Error("topic_set with lane 0 accepted")
	}
	h.run("topic", "show", "1", "--json")
	var v api.TopicView
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || len(v.Links) != 1 || v.Links[0].Kind != store.TopicLane || v.Links[0].Label != "feat/x" {
		t.Fatalf("show: %v %+v", err, v)
	}
}
