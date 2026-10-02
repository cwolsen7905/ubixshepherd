// Package git runs git for Shepherd. Shepherd drives the git CLI rather than a library,
// so it behaves exactly like the git its users and agents run, hooks and config included.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/redact"
)

// Run runs git in dir and returns its trimmed stdout. A failure's error carries git's
// stderr, redacted, since remotes and credential helpers can print tokens.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	// Never wait on a prompt for credentials: the daemon has no terminal.
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		// Nor on ssh asking for a passphrase or a host key: keys come from the agent.
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), redact.String(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

// ClearEnvConfig drops git configuration passed in through the environment
// (GIT_CONFIG_COUNT with its keys and values, and GIT_CONFIG_PARAMETERS). Test suites
// call it from TestMain: their git works on repos they make, and must not inherit the
// config of whatever started them, such as the push block of an agent run.
func ClearEnvConfig() {
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GIT_CONFIG_COUNT" || k == "GIT_CONFIG_PARAMETERS" ||
			strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			os.Unsetenv(k)
		}
	}
}

// Ok runs git and reports only whether it exited 0 (for --verify, --is-ancestor and the
// like, where a non-zero exit is an answer, not a failure).
func Ok(ctx context.Context, dir string, args ...string) bool {
	_, err := Run(ctx, dir, args...)
	return err == nil
}

// HasRemote reports whether the repo has a remote by that name.
func HasRemote(ctx context.Context, dir, name string) bool {
	return Ok(ctx, dir, "remote", "get-url", name)
}

// RefExists reports whether a full ref (refs/heads/x, refs/remotes/origin/x) exists.
func RefExists(ctx context.Context, dir, ref string) bool {
	return Ok(ctx, dir, "rev-parse", "--verify", "--quiet", ref)
}

// Dirty returns the porcelain status of a worktree, empty when it is clean.
func Dirty(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "status", "--porcelain")
}

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path   string
	Branch string // short name, empty when detached
	Bare   bool
}

// Worktrees lists the repo's worktrees; the first is the main one.
func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []Worktree
	for _, block := range strings.Split(out, "\n\n") {
		var w Worktree
		for _, line := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(line, " ")
			switch k {
			case "worktree":
				w.Path = v
			case "branch":
				w.Branch = strings.TrimPrefix(v, "refs/heads/")
			case "bare":
				w.Bare = true
			}
		}
		if w.Path != "" {
			wts = append(wts, w)
		}
	}
	if len(wts) == 0 {
		return nil, errors.New("git worktree list returned nothing")
	}
	return wts, nil
}
