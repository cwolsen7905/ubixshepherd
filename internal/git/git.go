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
	"path/filepath"
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

// EnsureExclude adds line to the repo's info/exclude, which lives in the git common
// directory and so covers every worktree of the repo without being committed or pushed.
// It reports whether it wrote anything (a line already there is left alone) and the
// file's path.
func EnsureExclude(ctx context.Context, repo, line string) (added bool, file string, err error) {
	common, err := Run(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return false, "", err
	}
	file = filepath.Join(common, "info", "exclude")
	cur, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return false, file, err
	}
	for _, l := range strings.Split(string(cur), "\n") {
		if strings.TrimSpace(l) == line {
			return false, file, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return false, file, err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, file, err
	}
	defer f.Close()
	prefix := ""
	if len(cur) > 0 && !strings.HasSuffix(string(cur), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + line + "\n")
	return err == nil, file, err
}

// DirtyPaths parses `git status --porcelain` output into the changed paths with their
// two-letter status codes ("??" is untracked). A rename lists its new path.
func DirtyPaths(porcelain string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(porcelain, "\n") {
		if len(l) < 4 {
			continue
		}
		// Run trims its output, which takes the first line's leading space: " M a" is "M a".
		code, p := l[:2], l[3:]
		if l[1] == ' ' && l[2] != ' ' {
			code, p = " "+l[:1], l[2:]
		}
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		out[strings.Trim(p, `"`)] = code
	}
	return out
}
