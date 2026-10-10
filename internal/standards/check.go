package standards

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
)

func statPath(p string) (os.FileInfo, error) { return os.Stat(p) }

// Result is one rule's check, run.
type Result struct {
	Rule   Rule   `json:"rule"`
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
}

// Blocks reports whether a failed check holds a ship back: only a must rule's does.
func (r Result) Blocks() bool { return !r.OK && r.Rule.Level == Must }

// Checked are the rules whose check runs for a diff: those with a check, and with no
// applies_to or one that matches a changed file. A rule's when tags do not matter here.
func Checked(s Set, changed []string) []Rule {
	var out []Rule
	for _, r := range s.Rules {
		if r.Check == "" {
			continue
		}
		if len(r.AppliesTo) > 0 {
			hit := false
			for _, f := range changed {
				if scope.Any(r.AppliesTo, f) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// BaseEnv names the variable a check finds the lane's base ref in: the diff it looks at
// is $SHEPHERD_BASE_REF...HEAD.
const BaseEnv = "SHEPHERD_BASE_REF"

// RunChecks runs each rule's check in dir, one after another, each bounded by timeout.
// A check passes by exiting 0; its output (redacted, the last lines) says why not.
// base is the ref the lane's changes are counted from, given to checks as $SHEPHERD_BASE_REF.
func RunChecks(ctx context.Context, dir, base string, rules []Rule, timeout time.Duration) []Result {
	var out []Result
	for _, r := range rules {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(cctx, "cmd", "/C", r.Check)
		} else {
			cmd = exec.CommandContext(cctx, "sh", "-c", r.Check)
		}
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), BaseEnv+"="+base)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		text := redact.String(buf.String())
		if cctx.Err() != nil {
			text += "\n(the check timed out after " + timeout.String() + ")"
			err = cctx.Err()
		}
		cancel()
		lines := strings.Split(strings.TrimSpace(text), "\n")
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		out = append(out, Result{Rule: r, OK: err == nil, Output: strings.Join(lines, "\n")})
	}
	return out
}
