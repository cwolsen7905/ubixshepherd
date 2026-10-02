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
//
// By default a lane keeps its conversation: a run continues the lane's last session
// with the same agent. NewSession starts a fresh one; Continue names the run whose
// session to continue (and so the lane and agent).
type StartRequest struct {
	LaneID     int64  `json:"lane_id,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Model      string `json:"model,omitempty"`
	Prompt     string `json:"prompt"`
	NewSession bool   `json:"new_session,omitempty"`
	Continue   int64  `json:"continue,omitempty"`
}

// Runner starts agents and watches them until they exit.
type Runner struct {
	Store  store.Store
	Config config.Config
	// Dir holds the run logs.
	Dir string
	// Exe is the shepherd binary that serves agents their worker tools; "" gives
	// agents none.
	Exe string
	Log *slog.Logger
	// lookPath finds an agent's executable; tests replace it.
	lookPath func(string) (string, error)

	mu      sync.Mutex
	procs   map[int64]*proc
	wg      sync.WaitGroup
	routeMu sync.Mutex
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
	var parent store.Run
	if req.Continue != 0 {
		p, err := r.Store.Run(ctx, req.Continue)
		if err != nil {
			return store.Run{}, err
		}
		switch {
		case p.State == store.RunRunning:
			return store.Run{}, refuse("run %d is still going; wait for it, or stop it first", p.ID)
		case p.Session == "":
			return store.Run{}, refuse("run %d has no session to continue (it started before Shepherd kept sessions, or its agent never reported one)", p.ID)
		case req.Agent != "" && req.Agent != p.Agent:
			return store.Run{}, refuse("run %d was %s; a session continues with the same agent", p.ID, p.Agent)
		}
		parent, req.LaneID, req.Agent = p, p.LaneID, p.Agent
		if req.Model == "" {
			req.Model = p.Model
		}
	}
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

	// The lane's conversation: continue its last session with this agent unless asked
	// for a new one.
	if parent.ID == 0 && !req.NewSession {
		prev, err := r.Store.Runs(ctx, lane.ID, "", 50)
		if err != nil {
			return store.Run{}, err
		}
		for _, p := range prev {
			if p.Agent == ad.Name && p.Session != "" {
				parent = p
				break
			}
		}
	}
	session, resume := parent.Session, parent.ID != 0
	if !resume {
		if session, err = ad.NewSession(ctx, bin, lane.Worktree); err != nil {
			return store.Run{}, err
		}
	}

	startSHA, err := git.Run(ctx, lane.Worktree, "rev-parse", "HEAD")
	if err != nil {
		return store.Run{}, err
	}
	run, err := r.Store.CreateRun(ctx, store.Run{
		LaneID: lane.ID, Agent: ad.Name, Model: req.Model, Prompt: req.Prompt,
		Session: session, Parent: parent.ID,
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
	// A new session gets the full brief; a continuing one already has it.
	prompt := req.Prompt
	worker := ""
	if r.Exe != "" && ad.WorkerReady() {
		worker = r.Exe
	}
	if !resume {
		prompt = Brief(req.Prompt, lane.Name, repo.Name, lane.Branch, lane.Base, lane.Worktree, lane.Scope, gate, worker != "", ad.Note)
	}
	cmd := exec.Command(bin, ad.Args(Opts{Prompt: prompt, Model: req.Model, Gate: gate, Worktree: lane.Worktree,
		Session: session, Resume: resume, Worker: worker})...)
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

	how := "new session"
	if resume {
		how = fmt.Sprintf("continues run %d", parent.ID)
	}
	fmt.Fprintf(logf, "# shepherd run %d: %s in lane %s (%s), %s, started %s\n# task: %s\n\n",
		run.ID, ad.Name, lane.Name, repo.Name, how, time.Now().Format(time.RFC3339), redact.String(firstLine(req.Prompt)))
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
	go r.watch(run, ad, lane, p, out, logf)
	r.Log.Info("run started", "run", run.ID, "agent", ad.Name, "lane", lane.Name, "pid", run.PID)
	verb := "started"
	if resume {
		verb = "continues"
	}
	r.feed(ctx, store.FeedRunStarted, run.ID, "run %d: %s %s in lane %s: %s", run.ID, ad.Name, verb, lane.Name, clip(req.Prompt, 120))
	return run, nil
}

// watch copies the agent's output to the log line by line, redacted, then records the
// outcome when the agent exits.
func (r *Runner) watch(run store.Run, ad Adapter, lane store.Lane, p *proc, out io.Reader, logf *os.File) {
	defer r.wg.Done()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		// The latest id an agent reports wins, in case resuming ever moves a session.
		if id := ad.SessionIn(line); id != "" {
			run.Session = id
		}
		fmt.Fprintln(logf, redact.String(line))
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
	ended := fmt.Sprintf("run %d: %s in lane %s %s, %d commit(s)", run.ID, run.Agent, lane.Name, run.State, run.Commits)
	if len(run.Outside) > 0 {
		ended += ", outside its scope: " + strings.Join(run.Outside, ", ")
	}
	r.feed(ctx, store.FeedRunEnded, run.ID, "%s", ended)
	r.deliverAnswers(ctx, run.ID)
	r.Route(ctx)
}

// Answer records a person's answer to a decision and carries it back into the asking
// agent's session: at once if the agent has ended its turn, or when its run ends.
func (r *Runner) Answer(ctx context.Context, id int64, answer string) (store.Decision, error) {
	if strings.TrimSpace(answer) == "" {
		return store.Decision{}, refuse("an answer cannot be empty")
	}
	d, err := r.Store.AnswerDecision(ctx, id, answer)
	if errors.Is(err, store.ErrConflict) {
		return d, refuse("%v", err)
	}
	if err != nil {
		return d, err
	}
	run, err := r.Store.Run(ctx, d.RunID)
	if err != nil {
		return d, err
	}
	if run.State == store.RunRunning {
		r.Log.Info("answer held until the run ends", "decision", d.ID, "run", run.ID)
		return d, nil
	}
	return r.deliver(ctx, d)
}

func (r *Runner) deliverAnswers(ctx context.Context, runID int64) {
	answered, err := r.Store.Decisions(ctx, store.DecisionAnswered)
	if err != nil {
		r.Log.Error("load answered decisions", "err", err)
		return
	}
	for _, d := range answered {
		if d.RunID == runID && d.AnswerRun == 0 {
			if _, err := r.deliver(ctx, d); err != nil {
				r.Log.Error("deliver answer", "decision", d.ID, "err", err)
			}
		}
	}
}

// deliver continues the asking run's session with the answer.
func (r *Runner) deliver(ctx context.Context, d store.Decision) (store.Decision, error) {
	prompt := fmt.Sprintf("The person answered your question (decision %d): %q\nAnswer: %s\nContinue the work with that.", d.ID, d.Question, d.Answer)
	next, err := r.Start(ctx, StartRequest{Continue: d.RunID, Prompt: prompt})
	if err != nil {
		return d, fmt.Errorf("answer recorded, but continuing run %d failed: %w", d.RunID, err)
	}
	if err := r.Store.SetDecisionRun(ctx, d.ID, next.ID); err != nil {
		return d, err
	}
	d.AnswerRun = next.ID
	r.Log.Info("answer delivered", "decision", d.ID, "run", next.ID)
	r.feed(ctx, store.FeedDecisionAnswer, d.ID, "decision %d answered; the agent carries on as run %d", d.ID, next.ID)
	return d, nil
}

// Ask records a question an agent holds for a person.
func (r *Runner) Ask(ctx context.Context, d store.Decision) (store.Decision, error) {
	if strings.TrimSpace(d.Question) == "" {
		return d, refuse("a question cannot be empty")
	}
	if _, err := r.Store.Run(ctx, d.RunID); err != nil {
		return d, err
	}
	d, err := r.Store.CreateDecision(ctx, d)
	if err == nil {
		r.Log.Info("decision held for the person", "decision", d.ID, "run", d.RunID)
		text := fmt.Sprintf("decision %d from %s: %s", d.ID, r.who(ctx, d.RunID), d.Question)
		for i, o := range d.Options {
			text += fmt.Sprintf("  [%d] %s", i+1, o)
		}
		if d.Recommendation != "" {
			text += "  (recommends: " + clip(d.Recommendation, 120) + ")"
		}
		r.feed(ctx, store.FeedDecision, d.ID, "%s", text)
	}
	return d, err
}

// EventReport is an agent's report; its status is progress, done or blocked.
const EventReport = "report"

var reportStatus = map[string]bool{"progress": true, "done": true, "blocked": true}

// Record stores something an agent told Shepherd.
func (r *Runner) Record(ctx context.Context, e store.Event) (store.Event, error) {
	switch {
	case e.Kind == EventReport && !reportStatus[e.Status]:
		return e, refuse("report status %q: want progress, done or blocked", e.Status)
	case e.Kind != EventReport:
		return e, refuse("event kind %q", e.Kind)
	case strings.TrimSpace(e.Text) == "":
		return e, refuse("say something")
	}
	if _, err := r.Store.Run(ctx, e.RunID); err != nil {
		return e, err
	}
	e.Text = redact.String(e.Text)
	e, err := r.Store.AddEvent(ctx, e)
	if err == nil {
		r.Log.Info("agent "+e.Kind, "run", e.RunID, "status", e.Status)
		r.feed(ctx, store.FeedReport, e.RunID, "%s reports %s: %s", r.who(ctx, e.RunID), e.Status, clip(e.Text, 200))
	}
	return e, err
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

// feed adds a line to the person's thread. A feed that cannot be written is logged, never
// a reason to fail the work it describes.
func (r *Runner) feed(ctx context.Context, kind string, ref int64, format string, a ...any) {
	if err := r.Store.AddFeed(ctx, kind, redact.String(fmt.Sprintf(format, a...)), ref); err != nil {
		r.Log.Error("write feed", "kind", kind, "err", err)
	}
}

// who names a run's agent and lane: "claude in lane feat/login".
func (r *Runner) who(ctx context.Context, runID int64) string {
	run, err := r.Store.Run(ctx, runID)
	if err != nil {
		return fmt.Sprintf("run %d", runID)
	}
	lane, err := r.Store.Lane(ctx, run.LaneID)
	if err != nil {
		return run.Agent
	}
	return run.Agent + " in lane " + lane.Name
}

// clip shortens text to one line of at most n bytes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
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
