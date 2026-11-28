package dispatch

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// recordingWorker starts runs on this machine, as LocalWorker does, and keeps each
// Launch and Process so a test can see what the runner asked for.
type recordingWorker struct {
	mu        sync.Mutex
	launches  []Launch
	processes []*countingProcess
}

func (w *recordingWorker) Start(ctx context.Context, l Launch) (Process, error) {
	p, err := LocalWorker{}.Start(ctx, l)
	if err != nil {
		return nil, err
	}
	cp := &countingProcess{Process: p}
	w.mu.Lock()
	w.launches = append(w.launches, l)
	w.processes = append(w.processes, cp)
	w.mu.Unlock()
	return cp, nil
}

// countingProcess counts the stops the runner sends.
type countingProcess struct {
	Process
	mu               sync.Mutex
	terminates, kill int
}

func (p *countingProcess) Terminate() {
	p.mu.Lock()
	p.terminates++
	p.mu.Unlock()
	p.Process.Terminate()
}

func (p *countingProcess) Kill() {
	p.mu.Lock()
	p.kill++
	p.mu.Unlock()
	p.Process.Kill()
}

func envHas(env []string, prefix string) bool {
	return slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, prefix) })
}

// TestRunsGoThroughTheWorker: a run is started by Runner.Worker, with the lane's
// worktree, the prompt on stdin, the run's own environment and the operator's variables
// dropped, and its outcome is recorded as with the local worker.
func TestRunsGoThroughTheWorker(t *testing.T) {
	f := newFixture(t, "ok")
	w := &recordingWorker{}
	f.runner.Worker = w

	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "do the work"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if done.State != store.RunSucceeded || done.Commits != 1 {
		t.Errorf("run = %+v", done)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.launches) != 1 {
		t.Fatalf("worker started %d runs, want 1", len(w.launches))
	}
	l := w.launches[0]
	if l.RunID != run.ID || l.Dir != f.lane.Worktree {
		t.Errorf("launch run %d in %q, want run %d in %q", l.RunID, l.Dir, run.ID, f.lane.Worktree)
	}
	if !strings.Contains(l.Stdin, "do the work") {
		t.Errorf("prompt not on stdin: %q", l.Stdin)
	}
	for _, want := range []string{"SHEPHERD_LANE=work", "GIT_TERMINAL_PROMPT=0"} {
		if !envHas(l.Env, want) {
			t.Errorf("launch env lacks %s", want)
		}
	}
	for _, name := range []string{EnvToken, EnvURL, EnvRun, EnvClient} {
		if !slices.Contains(l.Unset, name) {
			t.Errorf("launch does not drop the worker's own %s", name)
		}
	}
	if done.PID == 0 {
		t.Error("run has no pid from the worker's process")
	}
}

// TestStopGoesThroughTheWorker: stopping a run asks the worker's process to end.
func TestStopGoesThroughTheWorker(t *testing.T) {
	f := newFixture(t, "sleep")
	w := &recordingWorker{}
	f.runner.Worker = w

	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runner.Stop(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if done := f.wait(t, run.ID); done.State == store.RunRunning {
		t.Fatalf("run still running: %+v", done)
	}
	w.mu.Lock()
	p := w.processes[0]
	w.mu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.terminates == 0 {
		t.Error("stop did not reach the worker's process")
	}
}
