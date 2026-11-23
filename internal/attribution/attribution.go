// Package attribution marks the commits and merge requests an agent made in a Shepherd
// run: trailers on the commit, one line under the merge request's description.
package attribution

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Who is the run that did the work.
type Who struct {
	Run   int64
	Agent string
	Model string
}

var known = map[string]struct{ name, email string }{
	"claude":   {"Claude", "noreply@anthropic.com"},
	"copilot":  {"GitHub Copilot", "noreply@github.com"},
	"cursor":   {"Cursor Agent", "cursoragent@cursor.com"},
	"opencode": {"OpenCode", "noreply@opencode.ai"},
}

// Coauthor is the Co-Authored-By value for an agent: its name and a noreply address.
func Coauthor(agent string) string {
	if k, ok := known[strings.ToLower(agent)]; ok {
		return k.name + " <" + k.email + ">"
	}
	name := strings.TrimSpace(agent)
	if name == "" {
		name = "AI agent"
	}
	return name + " <noreply@invalid>"
}

// Trailers are the "Key: value" lines for a commit made in the run. A run of 0 has none.
func (w Who) Trailers() []string {
	if w.Run <= 0 {
		return nil
	}
	t := []string{"Co-Authored-By: " + Coauthor(w.Agent), "Shepherd-Run: " + strconv.FormatInt(w.Run, 10)}
	if w.Model != "" {
		t = append(t, "Shepherd-Model: "+w.Model)
	}
	return t
}

// Footer is the one line that ends a merge request's description.
func (w Who) Footer() string {
	agent := w.Agent
	if agent == "" {
		agent = "an agent"
	}
	s := "Made by " + agent
	if w.Model != "" {
		s += " (" + w.Model + ")"
	}
	return s + " in Shepherd run " + strconv.FormatInt(w.Run, 10) + "."
}

// AddFooter ends description with the footer, once: a description that already has it is
// returned as it is.
func AddFooter(description string, w Who) string {
	if w.Run <= 0 {
		return description
	}
	f := w.Footer()
	if strings.Contains(description, f) {
		return description
	}
	return strings.TrimRight(description, "\n") + "\n\n" + f + "\n"
}

// Apply adds w's trailers to the commit message in file with git interpret-trailers, so
// a trailer already there is not added again: Shepherd-* keys are left alone if present,
// Co-Authored-By is added unless that exact line exists.
func Apply(ctx context.Context, file string, w Who) error {
	ts := w.Trailers()
	if len(ts) == 0 {
		return nil
	}
	args := []string{"interpret-trailers", "--in-place"}
	for _, t := range ts {
		mode := "doNothing"
		if strings.HasPrefix(t, "Co-Authored-By:") {
			mode = "addIfDifferent"
		}
		args = append(args, "--if-exists", mode, "--if-missing", "add", "--trailer", t)
	}
	args = append(args, file)
	if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git interpret-trailers: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ParseRun reads a run id from SHEPHERD_RUN's value; 0 when it is unset or not a number.
func ParseRun(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// EnvRun is read here so the hook and its tests agree on what "no run" means.
func RunFromEnv() int64 { return ParseRun(os.Getenv("SHEPHERD_RUN")) }
