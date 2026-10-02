package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func mcpExchange(t *testing.T, env Env, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := serveMCP(context.Background(), env, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		resps = append(resps, m)
	}
	return resps
}

func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", resp)
	}
	content := res["content"].([]any)[0].(map[string]any)
	return content["text"].(string), res["isError"].(bool)
}

func TestMCPHandshakeAndList(t *testing.T) {
	h := newHarness(t, "", false)
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
		`not json`,
	)
	if len(resps) != 4 {
		t.Fatalf("got %d responses: %v", len(resps), resps)
	}
	init := resps[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["instructions"] == "" {
		t.Errorf("initialize = %v", init)
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"shepherd_status", "lane_open", "lane_close", "lane_list", "fold_gc", "shepherd_where"} {
		if !names[n] {
			t.Errorf("tool %s missing", n)
		}
	}
	if resps[2]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown method: %v", resps[2])
	}
	if resps[3]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Errorf("parse error: %v", resps[3])
	}
}

func TestMCPLaneTools(t *testing.T) {
	h := newHarness(t, "", false)
	root, _ := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = root
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"app","name":"feat/mcp","scope":["src/**"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"app","name":"feat/other","scope":["src/x/**"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lane_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"lane_open","arguments":{"repo":"app","name":"bad","scope":"src/**"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"lane_close","arguments":{"repo":"app","name":"feat/mcp"}}}`,
	)
	text, isErr := toolText(t, resps[0])
	if isErr || !strings.Contains(text, filepath.Join("app-worktrees", "feat-mcp")) {
		t.Errorf("lane_open: %v %s", isErr, text)
	}
	text, isErr = toolText(t, resps[1])
	if !isErr || !strings.Contains(text, "overlaps lane feat/mcp") {
		t.Errorf("overlapping lane_open: %v %s", isErr, text)
	}
	text, _ = toolText(t, resps[2])
	if !strings.Contains(text, "feat/mcp") {
		t.Errorf("lane_list: %s", text)
	}
	if _, isErr = toolText(t, resps[3]); !isErr {
		t.Error("scope as a string accepted")
	}
	text, isErr = toolText(t, resps[4])
	if isErr || !strings.Contains(text, "closed lane feat/mcp") {
		t.Errorf("lane_close: %v %s", isErr, text)
	}
}
