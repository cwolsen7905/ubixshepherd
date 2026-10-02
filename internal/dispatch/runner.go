package dispatch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ErrRefused is wrapped by errors that are the caller's to fix.
var ErrRefused = errors.New("refused")

type refusal struct{ msg string }

func (r refusal) Error() string        { return r.msg }
func (r refusal) Is(target error) bool { return target == ErrRefused }

func refuse(format string, a ...any) error { return refusal{fmt.Sprintf(format, a...)} }

// noPush is the push URL git uses for origin while an agent runs: it cannot connect,
// so `git push` fails whatever flags the agent passes, --no-verify included.
const noPush = "shepherd-run-blocks-push://"

// StartRequest asks for an agent to be started in a lane.
type StartRequest struct {
	LaneID int64  `json:"lane_id"`
	Agent  string `json:"agent"`
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt"`
}

// Runner starts agents and watches them until they exit.
type Runner struct {
	Store  store.Store
	Config config.Config
	// Dir holds the run logs.
	Dir string
	Log *slog.Logger
	// lookPath finds an agent's executable; tests replace it.
	lookPath func(string) (string, error)

	mu    sync.Mutex
	procs map[int64]*proc
	wg    sync.WaitGroup
}

type proc struct {
	cmd     *exec.Cmd
	stopped bool
}

// Recover marks runs a previous daemon left running as interrupted. Call it once at
// start, before any new run.
func (r *Runner) Recover(ctx context.Context) error {
	runs, err := r.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		return err
	}
	for _, run := range runs {
		now := time.Now().UTC()
		run.State, run.Ended, run.Error = store.RunInterrupted, &now, "the daemon stopped while this run was going"
		if err := r.Store.UpdateRun(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

// Start starts an agent in a lane and returns at once; the run is watched in the
// background and recorded when it exits.
func (r *Runner) Start(ctx context.Context, req StartRequest) (store.Run, error) {
	ad, err := AdapterFor(req.Agent)
	if err != nil {
		return store.Run{}, refuse("%v", err)
	}
	look := r.lookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(ad.Bin)
	if err != nil {
		return store.Run{}, refuse("%s is not installed on this machine (%s not found on the daemon's PATH)", ad.Name, ad.Bin)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return store.Run{}, refuse("a run needs a task")
	}
	lane, err := r.Store.Lane(ctx, req.LaneID)
	if err != nil {
		return store.Run{}, err
	}
	if lane.State != store.LaneOpen {
		return store.Run{}, refuse("lane %s is %s", lane.Name, lane.State)
	}
	if _, err := os.Stat(lane.Worktree); err != nil {
		return store.Run{}, refuse("lane %s's worktree is gone: %s", lane.Name, lane.Worktree)
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return store.Run{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.procs == nil {
		r.procs = map[int64]*proc{}
	}
	running, err := r.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		return store.Run{}, err
	}
	for _, other := range running {
		if other.LaneID == lane.ID {
			return store.Run{}, refuse("run %d (%s) is already going in lane %s; one agent per lane", other.ID, other.Agent, lane.Name)
		}
	}
	if max := r.Config.Daemon.MaxRuns; len(running) >= max {
		return store.Run{}, refuse("%d agents are already running, the limit on this machine (daemon.max_runs)", max)
	}

	startSHA, err := git.Run(ctx, lane.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return store.Run{}, err
	}
	run, err := r.Store.CreateRun(ctx, store.Run{
		LaneID: lane.ID, Agent: ad.Name, Model: req.Model, Prompt: req.Prompt,
		State: store.RunRunning, StartSHA: startSHA, Log: "pending",
	})
	if err != nil {
		return run, err
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return run, r.fail(ctx, run, err)
	}
	run.Log = filepath.Join(r.Dir, fmt.Sprintf("%d.log", run.ID))
	logf, err := os.OpenFile(run.Log, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return run, r.fail(ctx, run, err)
	}

	gate := r.Config.Profile(repo.Name).Gate
	prompt := Brief(req.Prompt, lane.Name, repo.Name, lane.Branch, lane.Base, lane.Worktree, lane.Scope, gate)
	cmd := exec.Command(bin, ad.Args(prompt, req.Model, gate, lane.Worktree)...)
	cmd.Dir = lane.Worktree
	cmd.Stdin = nil // reads from the null device: headless
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=remote.origin.pushurl", "GIT_CONFIG_VALUE_0="+noPush,
		fmt.Sprintf("SHEPHERD_RUN=%d", run.ID), "SHEPHERD_LANE="+lane.Name,
	)
	cmd.SysProcAttr = groupAttr()
	out, err := cmd.StdoutPipe()
	if err != nil {
		logf.Close()
		return run, r.fail(ctx, run, err)
	}
	cmd.Stderr = cmd.Stdout // one ordered stream

	fmt.Fprintf(logf, "# shepherd run %d: %s in lane %s (%s), started %s\n# task: %s\n\n",
		run.ID, ad.Name, lane.Name, repo.Name, time.Now().Format(time.RFC3339), redact.String(firstLine(req.Prompt)))
	if err := cmd.Start(); err != nil {
		logf.Close()
		return run, r.fail(ctx, run, err)
	}
	run.PID = cmd.Process.Pid
	if err := r.Store.UpdateRun(ctx, run); err != nil {
		r.Log.Error("record run pid", "run", run.ID, "err", err)
	}
	p := &proc{cmd: cmd}
	r.procs[run.ID] = p
	r.wg.Add(1)
	go r.watch(run, lane, p, out, logf)
	r.Log.Info("run started", "run", run.ID, "agent", ad.Name, "lane", lane.Name, "pid", run.PID)
	return run, nil
}

// watch copies the agent's output to the log line by line, redacted, then records the
// outcome when the agent exits.
func (r *Runner) watch(run store.Run, lane store.Lane, p *proc, out io.Reader, logf *os.File) {
	defer r.wg.Done()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		fmt.Fprintln(logf, redact.String(sc.Text()))
	}
	err := p.cmd.Wait()

	ctx := context.Background()
	now := time.Now().UTC()
	run.Ended = &now
	code := p.cmd.ProcessState.ExitCode()
	run.ExitCode = &code
	r.mu.Lock()
	stopped := p.stopped
	delete(r.procs, run.ID)
	r.mu.Unlock()
	switch {
	case stopped:
		run.State = store.RunStopped
	case err == nil && code == 0:
		run.State = store.RunSucceeded
	default:
		run.State = store.RunFailed
		if err != nil {
			run.Error = redact.String(err.Error())
		}
	}
	if end, err := git.Run(ctx, lane.Worktree, "rev-parse", "HEAD"); err == nil {
		run.EndSHA = end
		if n, err := git.Run(ctx, lane.Worktree, "rev-list", "--count", run.StartSHA+".."+end); err == nil {
			fmt.Sscan(n, &run.Commits)
		}
		if files, err := git.Run(ctx, lane.Worktree, "diff", "--name-only", "--no-renames", run.StartSHA, end); err == nil {
			for _, f := range strings.Split(files, "\n") {
				if f != "" && !scope.Any(lane.Scope, f) {
					run.Outside = append(run.Outside, f)
				}
			}
		}
	}
	fmt.Fprintf(logf, "\n# shepherd: run %d %s (exit %d) with %d commit(s)", run.ID, run.State, code, run.Commits)
	if len(run.Outside) > 0 {
		fmt.Fprintf(logf, "; outside the scope: %s", strings.Join(run.Outside, ", "))
	}
	fmt.Fprintln(logf)
	logf.Close()
	if err := r.Store.UpdateRun(ctx, run); err != nil {
		r.Log.Error("record run outcome", "run", run.ID, "err", err)
	}
	r.Log.Info("run ended", "run", run.ID, "state", run.State, "exit", code, "commits", run.Commits, "outside", len(run.Outside))
}

// Stop asks a running agent to stop, and kills it if it has not after a grace period.
func (r *Runner) Stop(ctx context.Context, id int64) error {
	r.mu.Lock()
	p, ok := r.procs[id]
	if ok {
		p.stopped = true
	}
	r.mu.Unlock()
	if !ok {
		return refuse("run %d is not running", id)
	}
	terminate(p.cmd)
	go func() {
		time.Sleep(10 * time.Second)
		r.mu.Lock()
		_, still := r.procs[id]
		r.mu.Unlock()
		if still {
			kill(p.cmd)
		}
	}()
	return nil
}

// Shutdown stops every running agent and waits for their outcomes to be recorded, up
// to the context's deadline. Runs cut short this way are recorded as interrupted.
func (r *Runner) Shutdown(ctx context.Context) {
	r.mu.Lock()
	for _, p := range r.procs {
		terminate(p.cmd)
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		r.mu.Lock()
		for _, p := range r.procs {
			kill(p.cmd)
		}
		r.mu.Unlock()
	}
	r.Recover(context.Background())
}

func (r *Runner) fail(ctx context.Context, run store.Run, err error) error {
	now := time.Now().UTC()
	run.State, run.Ended, run.Error = store.RunFailed, &now, redact.String(err.Error())
	r.Store.UpdateRun(ctx, run)
	return err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " ..."
	}
	return s
}

// ReadLog returns the run log from offset, at most max bytes, and the offset to read
// from next.
func ReadLog(path string, offset int64, max int) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	buf := make([]byte, max)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, offset, err
	}
	return buf[:n], offset + int64(n), nil
}
