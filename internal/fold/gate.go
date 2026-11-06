package fold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// GateTimeout bounds one gate run for an agent's push.
const GateTimeout = 20 * time.Minute

// GateRun is the outcome of running a repo's gate on a lane's head.
type GateRun struct {
	Lane    string `json:"lane"`
	SHA     string `json:"sha"`
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	// Tail is the end of the gate's output, redacted; Log is where all of it is.
	Tail string `json:"tail,omitempty"`
	Log  string `json:"log,omitempty"`
}

// gateCommand is the repo's configured gate, or "" for none.
func gateCommand(repo *store.Repo, f *Fold) string {
	if repo == nil {
		return ""
	}
	return f.Conf().Profile(repo.Name).Gate
}

// RunGate runs the repo's configured gate in the lane's worktree and records the result
// for the commit it ran on, as the agent's own. The command comes from the repo's
// profile, never from the caller, so a recorded pass means the configured gate passed.
// The worktree must be clean and the gate is judged on its HEAD: a run over uncommitted
// changes says nothing about the commit that will be pushed. logPath receives the whole
// output.
func (f *Fold) RunGate(ctx context.Context, laneID int64, logPath string) (GateRun, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return GateRun{}, err
	}
	repo, err := f.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return GateRun{}, err
	}
	gate := gateCommand(&repo, f)
	if gate == "" {
		return GateRun{}, refuse("%s has no gate configured, so there is nothing to record", repo.Name)
	}
	head, err := git.Run(ctx, lane.Worktree, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return GateRun{}, fmt.Errorf("lane %s: cannot read its head: %v", lane.Name, err)
	}
	dirty, err := git.Dirty(ctx, lane.Worktree)
	if err != nil {
		return GateRun{}, err
	}
	if dirty != "" {
		return GateRun{}, refuse("lane %s has uncommitted changes; commit or discard them first, so the gate checks the commit you push", lane.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, GateTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", gate)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", gate)
	}
	cmd.Dir = lane.Worktree
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	timedOut := ctx.Err() != nil
	text := redact.String(out.String())
	if logPath != "" {
		os.MkdirAll(filepath.Dir(logPath), 0o700)
		os.WriteFile(logPath, []byte(text), 0o600)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	res := GateRun{Lane: lane.Name, SHA: head, Command: redact.String(gate), Passed: runErr == nil && !timedOut,
		Tail: strings.Join(lines, "\n"), Log: logPath}
	if timedOut {
		res.Tail += "\n(the gate timed out after " + GateTimeout.String() + ")"
	}
	if _, err := f.Store.AddGateResult(context.WithoutCancel(ctx), store.GateResult{LaneID: lane.ID, SHA: head, Gate: res.Command,
		Passed: res.Passed, Source: store.GateSourceAgent}); err != nil {
		return res, err
	}
	return res, nil
}

// gateProblem is why a push of sha may not go ahead for want of a recorded gate pass, or
// "". It fails closed: a store error counts as no record.
func (f *Fold) gateProblem(ctx context.Context, lane *store.Lane, gate, sha string) string {
	g, err := f.Store.GateResultFor(ctx, lane.ID, sha)
	short := sha
	if len(short) > 12 {
		short = short[:12]
	}
	switch {
	case err != nil || g == nil:
		return fmt.Sprintf("the repo's gate, `%s`, has not passed on commit %s. Run `shepherd lane gate` in this worktree (with your work committed), fix what it reports, then push again", gate, short)
	case !g.Passed:
		return fmt.Sprintf("the repo's gate, `%s`, failed on commit %s. Fix it, commit, run `shepherd lane gate` again, then push", gate, short)
	case g.Gate != redact.String(gate):
		return fmt.Sprintf("the gate recorded for commit %s ran `%s`, not the repo's current `%s`. Run `shepherd lane gate` again, then push", short, g.Gate, gate)
	}
	return ""
}
