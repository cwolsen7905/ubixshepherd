// Package dispatch starts agents in lanes and records what came of each run.
//
// An adapter turns a task into one agent CLI's headless command line. Every adapter
// runs with the same default autonomy: edit files and commit inside the lane's
// worktree, run the repo's gate, never push. Where a CLI can deny `git push` itself,
// the adapter says so; the runner also breaks pushing for every agent, so the rule
// holds even for a CLI that cannot express it.
package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Adapter describes one agent CLI.
type Adapter struct {
	Name string
	// Bin is the executable looked up on PATH.
	Bin string
	// Args builds the headless command line for a run.
	Args func(o Opts) []string
	// NewSession returns the id a new conversation will have, or "" when the CLI only
	// reveals it in its output (see SessionIn).
	NewSession func(ctx context.Context, bin, worktree string) (string, error)
	// SessionIn finds a session id in a line of output, for CLIs that print it.
	SessionIn func(line string) string
	// Attach is the interactive command line that resumes a session with a person at
	// the keyboard.
	Attach func(session, worktree string) []string
	// WorkerReady reports whether the agent can be given Shepherd's worker tools. For
	// CLIs configured by flag it is always true; Cursor needs a one-time setup.
	WorkerReady func() bool
	// Note is added to the brief: what this CLI's permissions need the agent to know.
	Note string
}

// Opts are what a run's command line is built from.
type Opts struct {
	Prompt, Model, Gate, Worktree string
	// Session is the agent's conversation id; Resume says whether it already exists
	// (continue it) or is new (start it under that id, where the CLI allows choosing).
	Session string
	Resume  bool
	// Worker is the shepherd binary that serves the worker tools ("" for none).
	Worker string
}

// workerServer is the MCP server entry for the worker tools. The server finds its run
// from SHEPHERD_RUN, which the runner sets for the agent and the agent passes on.
func workerServer(exe string, extra map[string]any) map[string]any {
	srv := map[string]any{"command": exe, "args": []string{"mcp", "--worker"}}
	for k, v := range extra {
		srv[k] = v
	}
	return map[string]any{"mcpServers": map[string]any{"shepherd": srv}}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func always() bool { return true }

var copilotResume = regexp.MustCompile(`copilot --resume=([A-Za-z0-9-]+)`)

var adapters = map[string]Adapter{
	"claude": {
		Name: "claude", Bin: "claude",
		Args: func(o Opts) []string {
			// The prompt comes right after -p: --allowedTools and --mcp-config take lists
			// and would swallow a prompt placed after them.
			a := []string{"-p", o.Prompt, "--output-format", "text", "--permission-mode", "acceptEdits"}
			if o.Resume {
				a = append(a, "--resume", o.Session)
			} else if o.Session != "" {
				a = append(a, "--session-id", o.Session)
			}
			if o.Worker != "" {
				// Strict: the agent sees Shepherd's worker tools and no other MCP server,
				// so an operator server registered for the person cannot leak into it.
				a = append(a, "--strict-mcp-config", "--mcp-config", mustJSON(workerServer(o.Worker, nil)))
			}
			a = append(a, "--allowedTools", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)",
				"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)")
			if o.Gate != "" {
				a = append(a, "Bash("+o.Gate+":*)")
			}
			if o.Worker != "" {
				a = append(a, "mcp__shepherd")
			}
			a = append(a, "--disallowedTools", "Bash(git push:*)")
			if o.Model != "" {
				a = append(a, "--model", o.Model)
			}
			return a
		},
		WorkerReady: always,
		// Claude Code takes a session id chosen up front.
		NewSession: func(context.Context, string, string) (string, error) { return newUUID() },
		SessionIn:  func(string) string { return "" },
		Attach:     func(session, _ string) []string { return []string{"--resume", session} },
	},
	"copilot": {
		Name: "copilot", Bin: "copilot",
		Args: func(o Opts) []string {
			a := []string{"-p", o.Prompt, "--allow-tool", "write", "--allow-tool", "shell(git:*)",
				"--deny-tool", "shell(git push)"}
			if stem := firstWord(o.Gate); stem != "" {
				a = append(a, "--allow-tool", "shell("+stem+")")
			}
			if o.Worker != "" {
				a = append(a, "--additional-mcp-config", mustJSON(workerServer(o.Worker, map[string]any{"type": "local", "tools": []string{"*"}})),
					"--allow-tool", "shepherd")
			}
			if o.Resume {
				a = append(a, "--resume="+o.Session)
			}
			if o.Model != "" {
				a = append(a, "--model", o.Model)
			}
			return a
		},
		WorkerReady: always,
		// Copilot names the session itself and prints "copilot --resume=<id>" at the end.
		NewSession: func(context.Context, string, string) (string, error) { return "", nil },
		SessionIn: func(line string) string {
			if m := copilotResume.FindStringSubmatch(line); m != nil {
				return m[1]
			}
			return ""
		},
		Attach: func(session, _ string) []string { return []string{"--resume=" + session} },
		// Copilot approves each part of a chained command, and nobody can approve
		// `exit` or `true` in a headless run.
		Note: "Run each git command on its own (git add, then git commit), not chained with &&, || or ;. Chained commands need an approval nobody can give in this run.",
	},
	"cursor": {
		Name: "cursor", Bin: "cursor-agent",
		Args: func(o Opts) []string {
			// cursor-agent has no per-command deny on the command line: --force lets it
			// run the gate and commit, and the runner's push block holds the line. Its
			// chat is created first (NewSession), so every run resumes one.
			a := []string{"-p", o.Prompt, "--output-format", "text", "--force", "--trust", "--workspace", o.Worktree}
			if o.Session != "" {
				a = append(a, "--resume", o.Session)
			}
			if o.Worker != "" {
				// The worker server comes from ~/.cursor/mcp.json (shepherd agents setup
				// cursor); approve it without a prompt.
				a = append(a, "--approve-mcps")
			}
			if o.Model != "" {
				a = append(a, "--model", o.Model)
			}
			return a
		},
		WorkerReady: CursorWorkerReady,
		NewSession: func(ctx context.Context, bin, worktree string) (string, error) {
			cmd := exec.CommandContext(ctx, bin, "create-chat")
			cmd.Dir = worktree
			out, err := cmd.Output()
			if err != nil {
				return "", fmt.Errorf("cursor-agent create-chat: %w", err)
			}
			lines := strings.Fields(strings.TrimSpace(string(out)))
			if len(lines) == 0 {
				return "", errors.New("cursor-agent create-chat printed no chat id")
			}
			return lines[len(lines)-1], nil
		},
		SessionIn: func(string) string { return "" },
		Attach: func(session, worktree string) []string {
			return []string{"--resume", session, "--workspace", worktree}
		},
	},
}

// newUUID returns a random (version 4) UUID.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// AdapterFor returns the adapter by name.
func AdapterFor(name string) (Adapter, error) {
	a, ok := adapters[name]
	if !ok {
		return a, fmt.Errorf("unknown agent %q; Shepherd knows %s", name, strings.Join(AgentNames(), ", "))
	}
	return a, nil
}

// AgentNames lists the adapters, sorted.
func AgentNames() []string {
	var out []string
	for n := range adapters {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func firstWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// Brief wraps a task with what every agent needs to know about its lane, so the same
// task means the same thing to every provider.
func Brief(task, lane, repo, branch, base, worktree string, scope []string, gate string, tools bool, note string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are working for uBixShepherd in lane %s of repo %s.\n", lane, repo)
	fmt.Fprintf(&b, "Work only inside this directory: %s (branch %s, cut from %s).\n", worktree, branch, base)
	fmt.Fprintf(&b, "Your scope, the only paths you may change: %s. Changes outside it are refused.\n", strings.Join(scope, ", "))
	if gate != "" {
		fmt.Fprintf(&b, "Before committing, run the repo's gate, `%s`, and make it pass.\n", gate)
	}
	b.WriteString("When the work is done, commit it with clear commit messages. Do not push: pushing is blocked, and the person reviews and pushes.\n")
	if note != "" {
		b.WriteString(note + "\n")
	}
	if tools {
		b.WriteString(`You have Shepherd's tools. Use them instead of guessing or stopping silently:
- ask_human: anything that is the person's call (money or pricing, published or user-facing text, deleting or overwriting data, anything in production, a change to scope or design with no clear default). Give options and your recommendation, then end your turn: you will be continued with the answer.
- ask_shepherd: you need another lane (an answer from it, a change outside your scope, a review). Then end your turn: you will be continued with the reply.
- report: say progress, done (with what you did and how you checked it) or blocked (and why).
Everything else, keep working without asking.
`)
	} else {
		b.WriteString("If the task cannot be done inside the scope, stop and say why instead of working around it.\n")
	}
	b.WriteString("\n")
	b.WriteString("Task:\n")
	b.WriteString(task)
	b.WriteString("\n")
	return b.String()
}

// CursorConfig is where Cursor reads MCP servers for every workspace.
func CursorConfig() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor", "mcp.json")
}

// CursorWorkerReady reports whether ~/.cursor/mcp.json has Shepherd's worker entry.
func CursorWorkerReady() bool {
	b, err := os.ReadFile(CursorConfig())
	if err != nil {
		return false
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	return json.Unmarshal(b, &cfg) == nil && cfg.MCPServers[cursorWorker] != nil
}

// cursorWorker names Shepherd's entry in Cursor's config.
const cursorWorker = "shepherd-worker"

// SetupCursor adds Shepherd's worker server to ~/.cursor/mcp.json, keeping every other
// server and setting as they are. It reports whether it changed the file.
func SetupCursor(exe string) (bool, error) {
	path := CursorConfig()
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return false, fmt.Errorf("%s is not valid JSON, so Shepherd leaves it alone: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	want := map[string]any{"command": exe, "args": []any{"mcp", "--worker"}}
	if cur, ok := servers[cursorWorker]; ok && mustJSON(cur) == mustJSON(want) {
		return false, nil
	}
	servers[cursorWorker] = want
	cfg["mcpServers"] = servers
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(b, '\n'), 0o644)
}
