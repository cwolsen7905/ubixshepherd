// Package dispatch starts agents in lanes and records what came of each run.
//
// An adapter turns a task into one agent CLI's headless command line. Every adapter
// runs with the same default autonomy: edit files and commit inside the lane's
// worktree, run the repo's gate, never push. Where a CLI can deny `git push` itself,
// the adapter says so; the runner also breaks pushing for every agent, so the rule
// holds even for a CLI that cannot express it.
package dispatch

import (
	"fmt"
	"sort"
	"strings"
)

// Adapter describes one agent CLI.
type Adapter struct {
	Name string
	// Bin is the executable looked up on PATH.
	Bin string
	// Args builds the command line for a briefed prompt in the worktree.
	Args func(prompt, model, gate, worktree string) []string
}

var adapters = map[string]Adapter{
	"claude": {
		Name: "claude", Bin: "claude",
		Args: func(prompt, model, gate, _ string) []string {
			// The prompt comes right after -p: --allowedTools takes a list and would
			// swallow a prompt placed after it.
			a := []string{"-p", prompt, "--output-format", "text", "--permission-mode", "acceptEdits",
				"--allowedTools", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)",
				"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)"}
			if gate != "" {
				a = append(a, "Bash("+gate+":*)")
			}
			a = append(a, "--disallowedTools", "Bash(git push:*)")
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
	},
	"copilot": {
		Name: "copilot", Bin: "copilot",
		Args: func(prompt, model, gate, _ string) []string {
			a := []string{"-p", prompt, "--allow-tool", "write", "--allow-tool", "shell(git:*)",
				"--deny-tool", "shell(git push)"}
			if stem := firstWord(gate); stem != "" {
				a = append(a, "--allow-tool", "shell("+stem+")")
			}
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
	},
	"cursor": {
		Name: "cursor", Bin: "cursor-agent",
		Args: func(prompt, model, _, worktree string) []string {
			// cursor-agent has no per-command deny on the command line: --force lets it
			// run the gate and commit, and the runner's push block holds the line.
			a := []string{"-p", prompt, "--output-format", "text", "--force", "--trust", "--workspace", worktree}
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
	},
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
