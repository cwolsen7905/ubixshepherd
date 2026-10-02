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
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Adapter describes one agent CLI.
type Adapter struct {
	Name string
	// Bin is the executable looked up on PATH.
	Bin string
	// Args builds the headless command line for a prompt. session is the agent's
	// conversation id; resume says whether it already exists (continue it) or is new
	// (start it under that id, where the CLI allows choosing one).
	Args func(prompt, model, gate, worktree, session string, resume bool) []string
	// NewSession returns the id a new conversation will have, or "" when the CLI only
	// reveals it in its output (see SessionIn).
	NewSession func(ctx context.Context, bin, worktree string) (string, error)
	// SessionIn finds a session id in a line of output, for CLIs that print it.
	SessionIn func(line string) string
	// Attach is the interactive command line that resumes a session with a person at
	// the keyboard.
	Attach func(session, worktree string) []string
}

var copilotResume = regexp.MustCompile(`copilot --resume=([A-Za-z0-9-]+)`)

var adapters = map[string]Adapter{
	"claude": {
		Name: "claude", Bin: "claude",
		Args: func(prompt, model, gate, _ string, session string, resume bool) []string {
			// The prompt comes right after -p: --allowedTools takes a list and would
			// swallow a prompt placed after it.
			a := []string{"-p", prompt, "--output-format", "text", "--permission-mode", "acceptEdits"}
			if resume {
				a = append(a, "--resume", session)
			} else if session != "" {
				a = append(a, "--session-id", session)
			}
			a = append(a, "--allowedTools", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)",
				"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)")
			if gate != "" {
				a = append(a, "Bash("+gate+":*)")
			}
			a = append(a, "--disallowedTools", "Bash(git push:*)")
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
		// Claude Code takes a session id chosen up front.
		NewSession: func(context.Context, string, string) (string, error) { return newUUID() },
		SessionIn:  func(string) string { return "" },
		Attach:     func(session, _ string) []string { return []string{"--resume", session} },
	},
	"copilot": {
		Name: "copilot", Bin: "copilot",
		Args: func(prompt, model, gate, _ string, session string, resume bool) []string {
			a := []string{"-p", prompt, "--allow-tool", "write", "--allow-tool", "shell(git:*)",
				"--deny-tool", "shell(git push)"}
			if stem := firstWord(gate); stem != "" {
				a = append(a, "--allow-tool", "shell("+stem+")")
			}
			if resume {
				a = append(a, "--resume="+session)
			}
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
		// Copilot names the session itself and prints "copilot --resume=<id>" at the end.
		NewSession: func(context.Context, string, string) (string, error) { return "", nil },
		SessionIn: func(line string) string {
			if m := copilotResume.FindStringSubmatch(line); m != nil {
				return m[1]
			}
			return ""
		},
		Attach: func(session, _ string) []string { return []string{"--resume=" + session} },
	},
	"cursor": {
		Name: "cursor", Bin: "cursor-agent",
		Args: func(prompt, model, _, worktree, session string, _ bool) []string {
			// cursor-agent has no per-command deny on the command line: --force lets it
			// run the gate and commit, and the runner's push block holds the line. Its
			// chat is created first (NewSession), so every run resumes one.
			a := []string{"-p", prompt, "--output-format", "text", "--force", "--trust", "--workspace", worktree}
			if session != "" {
				a = append(a, "--resume", session)
			}
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
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
func Brief(task, lane, repo, branch, base, worktree string, scope []string, gate string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are working for uBixShepherd in lane %s of repo %s.\n", lane, repo)
	fmt.Fprintf(&b, "Work only inside this directory: %s (branch %s, cut from %s).\n", worktree, branch, base)
	fmt.Fprintf(&b, "Your scope, the only paths you may change: %s. Changes outside it are refused.\n", strings.Join(scope, ", "))
	if gate != "" {
		fmt.Fprintf(&b, "Before committing, run the repo's gate, `%s`, and make it pass.\n", gate)
	}
	b.WriteString("When the work is done, commit it with clear commit messages. Do not push: pushing is blocked, and the person reviews and pushes.\n")
	b.WriteString("If the task cannot be done inside the scope, stop and say why instead of working around it.\n\n")
	b.WriteString("Task:\n")
	b.WriteString(task)
	b.WriteString("\n")
	return b.String()
}
