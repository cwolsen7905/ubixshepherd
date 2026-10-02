package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/version"
)

// shepherd mcp serves Shepherd's operator tools over MCP on stdio (newline-delimited
// JSON-RPC 2.0). Each tool runs the CLI command of the same name, so the two never
// drift: the MCP server is one more client of the daemon, like the CLI.

// mcpVersions are the protocol revisions this server speaks; it answers with the
// client's when it is one of these, else the newest. The tools use only base features.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = `Shepherd keeps parallel work in a workspace of git repos apart. A lane is one stream of work in one repo: its own branch, cut from a fresh fetch of the repo's base branch, its own worktree, and a scope (globs relative to the repo) that its changes stay inside. Open a lane before changing a repo, and work only in the lane's worktree. Scopes are leases: an overlapping scope is refused, naming the lane that holds it. A pre-push hook refuses pushes outside the scope. Close a lane once its MR is merged. Never pass force to lane_close unless the person has confirmed the MR is merged (a squash merge looks unmerged to git).`

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// args turns the call's arguments into CLI arguments.
	args func(map[string]any) ([]string, error) `json:"-"`
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	propRepo = map[string]any{"type": "string", "description": "Repo, by its path in the workspace as lane_list and shepherd_status show it (for example \"ubixshepherd\")."}
	propPath = map[string]any{"type": "string", "description": "Absolute directory to resolve. Default: where the MCP server runs."}
)

func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "shepherd_status",
			Description: "The daemon, its workspaces with repo and lane counts, and what the current directory resolves to.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"status"}, nil },
		},
		{
			Name:        "shepherd_where",
			Description: "Which workspace, repo and lane a directory belongs to, with the repo's profile (base branch, branch model, gate, shared paths, autonomy).",
			InputSchema: obj(map[string]any{"path": propPath}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"where"}
				if p := str(a, "path"); p != "" {
					out = append(out, p)
				}
				return out, nil
			},
		},
		{
			Name:        "lane_list",
			Description: "Open lanes: repo, name, state, age and scope. Without repo, every repo in the workspace.",
			InputSchema: obj(map[string]any{"repo": propRepo}),
			args: func(a map[string]any) ([]string, error) {
				if r := str(a, "repo"); r != "" {
					return []string{"lane", "list", "--repo", r}, nil
				}
				return []string{"lane", "list", "--all"}, nil
			},
		},
		{
			Name:        "lane_open",
			Description: "Open a lane: fetch, cut the branch from origin/<base>, add the worktree, record the scope. Returns the worktree path to work in. Refused if the scope overlaps an open lane's.",
			InputSchema: obj(map[string]any{
				"repo":   propRepo,
				"name":   map[string]any{"type": "string", "description": "Lane and branch name: lowercase, at most one slash (feat/login, fix-crash)."},
				"scope":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Globs relative to the repo the work stays inside: \"src/auth/**\", \"docs/*.md\", \"README.md\"."},
				"branch": map[string]any{"type": "string", "description": "Branch name if it should differ from the lane name."},
			}, "repo", "name", "scope"),
			args: func(a map[string]any) ([]string, error) {
				scope, err := strs(a, "scope")
				if err != nil {
					return nil, err
				}
				out := []string{"lane", "open", str(a, "name"), "--repo", str(a, "repo")}
				for _, s := range scope {
					out = append(out, "--scope", s)
				}
				if b := str(a, "branch"); b != "" {
					out = append(out, "--branch", b)
				}
				return out, nil
			},
		},
		{
			Name:        "lane_close",
			Description: "Close a lane: remove its worktree and delete its branch if git sees it merged. Refuses uncommitted changes or an unmerged branch unless force is set.",
			InputSchema: obj(map[string]any{
				"repo":  propRepo,
				"name":  map[string]any{"type": "string", "description": "The lane's name."},
				"force": map[string]any{"type": "boolean", "description": "Close anyway, discarding uncommitted changes and keeping an unmerged branch. Only after the person confirms the MR is merged."},
			}, "repo", "name"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"lane", "close", str(a, "name"), "--repo", str(a, "repo")}
				if f, _ := a["force"].(bool); f {
					out = append(out, "--force")
				}
				return out, nil
			},
		},
		{
			Name:        "fold_gc",
			Description: "List worktrees across the workspace that look finished (merged, branch gone, missing) and lanes whose worktree is gone. Changes nothing.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"fold", "gc"}, nil },
		},
	}
}

func str(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}

func strs(a map[string]any, k string) ([]string, error) {
	raw, ok := a[k].([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", k)
	}
	var out []string
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", k)
		}
		out = append(out, s)
	}
	return out, nil
}

func runMCP(ctx context.Context, env Env, args []string) error {
	if len(args) > 0 {
		return errUsage
	}
	return serveMCP(ctx, env, env.Stdin, env.Stdout)
}

func serveMCP(ctx context.Context, env Env, in io.Reader, out io.Writer) error {
	tools := map[string]mcpTool{}
	var list []mcpTool
	for _, t := range mcpTools() {
		tools[t.Name] = t
		list = append(list, t)
	}
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(msg.ID) == 0 {
			continue // a notification (initialized, cancelled): nothing to answer
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: msg.ID}
		switch msg.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(msg.Params, &p)
			v := mcpVersions[0]
			for _, s := range mcpVersions {
				if s == p.ProtocolVersion {
					v = s
				}
			}
			resp.Result = map[string]any{
				"protocolVersion": v,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "shepherd", "version": version.Version},
				"instructions":    mcpInstructions,
			}
		case "ping":
			resp.Result = map[string]any{}
		case "tools/list":
			resp.Result = map[string]any{"tools": list}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				resp.Error = &rpcError{-32602, err.Error()}
				break
			}
			t, ok := tools[p.Name]
			if !ok {
				resp.Error = &rpcError{-32602, "unknown tool " + p.Name}
				break
			}
			resp.Result = callTool(ctx, env, t, p.Arguments)
		default:
			resp.Error = &rpcError{-32601, "method not found: " + msg.Method}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// callTool runs the tool's CLI command with output captured. A failing command is a
// tool error the model can read and act on, not a protocol error.
func callTool(ctx context.Context, env Env, t mcpTool, a map[string]any) map[string]any {
	if a == nil {
		a = map[string]any{}
	}
	text, isErr := "", false
	args, err := t.args(a)
	if err != nil {
		text, isErr = err.Error(), true
	} else {
		var stdout, stderr bytes.Buffer
		cenv := env
		cenv.Stdin, cenv.Stdout, cenv.Stderr = strings.NewReader(""), &stdout, &stderr
		cenv.Interactive = false
		cenv.Client = "mcp"
		if p := str(a, "path"); p != "" && filepath.IsAbs(p) {
			cenv.Cwd = p
		}
		code := Run(ctx, cenv, args)
		text = strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		isErr = code != 0
	}
	if text == "" {
		text = "done"
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}
