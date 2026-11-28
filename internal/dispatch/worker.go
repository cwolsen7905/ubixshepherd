package dispatch

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

// A Worker starts agent runs. The Runner decides everything about a run (the lane, the
// brief, the adapter's arguments, the run's token and push block) and hands the worker a
// Launch; the worker only starts the process and reports on it. A standalone daemon uses
// LocalWorker, which starts the agent on this machine. A controller hands runs to workers
// on other machines instead, over the cluster link.
type Worker interface {
	// Start starts the agent described by l. The process's output and errors are one
	// ordered stream, read from Process.Output.
	Start(ctx context.Context, l Launch) (Process, error)
}

// Launch is everything a worker needs to start one run.
type Launch struct {
	// RunID is the run being started, for the worker's own records and logs.
	RunID int64
	// Bin and Args are the agent's command line; Dir is the lane's worktree.
	Bin  string
	Args []string
	Dir  string
	// Stdin is the prompt, written whole and then closed: agents run headless, and the
	// prompt stays off the command line.
	Stdin string
	// Unset names variables of the worker's own environment the agent must not inherit,
	// such as an operator token. Env is added after, and wins over the worker's own.
	Unset []string
	Env   []string
}

// Process is a started agent.
type Process interface {
	// Pid is the agent's process id on its worker.
	Pid() int
	// Output is what the agent prints, standard output and errors together.
	Output() io.Reader
	// Wait waits for the agent to end; call it once, after Output reads to the end.
	Wait() error
	// ExitCode is the agent's exit code once Wait has returned: -1 if it was killed.
	ExitCode() int
	// Terminate asks the agent and the tools it started to end; Kill makes them.
	Terminate()
	Kill()
}

// LocalWorker starts agents on this machine, each in its own process group so stopping
// it stops the tools it started. It is the worker of a standalone daemon.
type LocalWorker struct{}

func (LocalWorker) Start(ctx context.Context, l Launch) (Process, error) {
	cmd := exec.Command(l.Bin, l.Args...)
	cmd.Dir = l.Dir
	cmd.Stdin = strings.NewReader(l.Stdin)
	cmd.Env = append(withoutEnv(os.Environ(), l.Unset...), l.Env...)
	cmd.SysProcAttr = groupAttr()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // one ordered stream
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &localProcess{cmd: cmd, out: out}, nil
}

type localProcess struct {
	cmd *exec.Cmd
	out io.Reader
}

func (p *localProcess) Pid() int          { return p.cmd.Process.Pid }
func (p *localProcess) Output() io.Reader { return p.out }
func (p *localProcess) Wait() error       { return p.cmd.Wait() }
func (p *localProcess) ExitCode() int     { return p.cmd.ProcessState.ExitCode() }
func (p *localProcess) Terminate()        { terminate(p.cmd) }
func (p *localProcess) Kill()             { kill(p.cmd) }

// worker is the Runner's worker: LocalWorker unless one is set.
func (r *Runner) worker() Worker {
	if r.Worker != nil {
		return r.Worker
	}
	return LocalWorker{}
}
