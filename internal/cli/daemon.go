package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/muster"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/service"
	"github.com/ubixsys/ubixshepherd/internal/standards"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
	"github.com/ubixsys/ubixshepherd/internal/update"
	"github.com/ubixsys/ubixshepherd/internal/version"
	"github.com/ubixsys/ubixshepherd/internal/watch"
)

// serviceFor finds the OS service manager; tests replace it.
var serviceFor = service.For

// isDaemonRun says whether args, a whole command line, run the daemon in the foreground:
// `shepherd daemon` or `shepherd daemon run`, as launchd, systemd or a person starts it.
func isDaemonRun(args []string) bool {
	_, rest, err := splitGlobals(args)
	if err != nil || len(rest) == 0 || rest[0] != "daemon" {
		return false
	}
	var pos []string
	for _, a := range rest[1:] {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	return len(pos) == 0 || (len(pos) == 1 && pos[0] == "run")
}

func runDaemon(ctx context.Context, env Env, args []string) error {
	fs := flags("daemon", env)
	force := fs.Bool("force-from-run", false, "allow start, restart or install from inside an agent run")
	drain := fs.Bool("drain", false, "restart: stop starting runs, let the running ones finish, then restart (they are continued after)")
	timeout := fs.Duration("timeout", 10*time.Minute, "restart --drain: how long to wait for runs before stopping them (0: do not wait)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	sub := "run"
	switch len(pos) {
	case 0:
	case 1:
		sub = pos[0]
	default:
		return errUsage
	}
	if sub == "start" || sub == "restart" || sub == "install" {
		if err := refuseFromRun(env, sub, *force); err != nil {
			return err
		}
	}
	switch sub {
	case "run":
		return daemonRun(ctx, env)
	case "start":
		if c, err := connect(ctx, env); err == nil {
			st, _ := c.Status(ctx)
			fmt.Fprintf(env.Stdout, "already running (pid %d)\n", st.PID)
			return nil
		}
		_, err := startDaemon(ctx, env, false)
		return err
	case "stop":
		return daemonStop(ctx, env)
	case "restart":
		// Refuse before stopping, so a bad config.yaml leaves the running daemon alone.
		warns, err := checkConfig(env.Layout)
		if err != nil {
			return fmt.Errorf("%w\n  a running daemon was left as it is; fix %s, then restart", err, env.Layout.Config())
		}
		printConfigWarnings(env, warns)
		if *timeout < 0 {
			return errors.New("--timeout must not be negative")
		}
		return daemonRestart(ctx, env, *drain && *timeout > 0, *timeout)
	case "reload":
		return daemonReload(ctx, env)
	case "check":
		return daemonCheck(env)
	case "status":
		return daemonStatus(ctx, env)
	case "install":
		return daemonInstall(ctx, env)
	case "uninstall":
		return daemonUninstall(ctx, env)
	}
	return errUsage
}

// daemonRun runs the daemon in the foreground until interrupted or asked to stop. When
// an upgrade asked for the stop, it then starts the binary now in place (reexec): under a
// service manager the process keeps its place, so nothing sees a restart.
func daemonRun(ctx context.Context, env Env) error {
	var restart bool
	if err := daemonServe(ctx, env, &restart); err != nil {
		return err
	}
	if !restart {
		return nil
	}
	if err := reexec(env); err != nil {
		// The new binary would not start: put the old one back and start that.
		if rerr := rollbackUpgrade(env, "the new binary could not be started: "+err.Error()); rerr != nil {
			return fmt.Errorf("upgrade restart failed (%v) and rollback failed: %w", err, rerr)
		}
		return reexec(env)
	}
	return nil
}

// rollbackUpgrade restores the binary an upgrade replaced and records that the release
// it tried did not take, so update.auto does not try it again.
func rollbackUpgrade(env Env, why string) error {
	marker := daemon.UpgradeMarkerPath(env.Layout.Config())
	m, _ := update.ReadMarker(marker)
	if err := update.Restore(env.Exe); err != nil {
		return err
	}
	update.ClearMarker(marker)
	if m.To != "" {
		c := &update.Checker{StatePath: filepath.Join(env.Layout.Home, "update.json")}
		c.MarkFailed(m.To, "rolled back to "+m.From+": "+why, true)
	}
	return nil
}

func daemonServe(ctx context.Context, env Env, restart *bool) error {
	l := env.Layout
	// Whatever started this daemon, it must not carry a run's push block or identity.
	unsetRunEnv()
	// Fail loudly: on a git older than the minimum the push block silently does not apply.
	if err := git.CheckVersion(ctx, git.MinVersion); err != nil {
		return err
	}
	if c, err := connect(ctx, env); err == nil {
		st, _ := c.Status(ctx)
		return fmt.Errorf("a daemon is already running (pid %d, version %s); a command or make install started it in the background.\n"+
			"  watch it:        tail -f %s\n"+
			"  or run it here:  shepherd daemon stop && shepherd daemon", st.PID, st.Version, l.Log())
	}
	// A freshly upgraded binary that never stayed up is put back before it runs again.
	if bad, m := update.Startup(daemon.UpgradeMarkerPath(l.Config()), version.Version); bad {
		fmt.Fprintf(env.Stderr, "upgrade to %s did not start cleanly: restoring %s\n", m.To, m.From)
		if err := rollbackUpgrade(env, "it did not stay up after starting"); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		*restart = true
		return nil
	}
	if err := os.MkdirAll(l.Home, 0o700); err != nil {
		return err
	}
	if wrote, err := config.WriteTemplate(l.Config()); err != nil {
		return err
	} else if wrote {
		fmt.Fprintf(env.Stderr, "Wrote %s: every setting commented out, defaults apply.\n", l.Config())
		// With the config: the example pack, which no repo uses until its profile lists it.
		if seeded, err := standards.SeedHouse(filepath.Join(l.Home, "standards")); err != nil {
			fmt.Fprintf(env.Stderr, "Could not write the example standards pack: %v\n", err)
		} else if seeded {
			fmt.Fprintf(env.Stderr, "Wrote an example standards pack to %s.\n", filepath.Join(l.Home, "standards", "house"))
		}
	}
	cfg, warns, err := config.LoadWarn(l.Config())
	if err != nil {
		return err
	}
	st, err := sqlite.Open(ctx, l.Store())
	if err != nil {
		return err
	}
	defer st.Close()
	// The daemon writes and rotates its own log; whatever started it captures only what
	// the log cannot (see Layout.Console). Run by hand in a terminal, it logs there too.
	var also io.Writer
	if term.IsTerminal(int(os.Stderr.Fd())) {
		also = os.Stderr
	}
	level := new(slog.LevelVar)
	level.Set(daemon.LogLevel(cfg.Daemon.LogLevel))
	logger, logFile, err := daemon.OpenLogger(l.Log(), also, level)
	if err != nil {
		return err
	}
	defer logFile.Close()
	srv, err := daemon.NewServer(st, cfg, l.Config(), logger)
	if err != nil {
		return err
	}
	srv.Level = level
	for _, w := range warns {
		srv.Log.Warn("config", "warning", w)
	}
	srv.Fold.Exe = env.Exe
	srv.Exe = env.Exe
	srv.Runner = &dispatch.Runner{Store: st, Config: cfg, Dir: filepath.Join(l.Home, "runs"), Log: srv.Log, Exe: env.Exe}
	srv.Fold.ArtifactDir = filepath.Join(l.Home, "artifacts")
	srv.Runner.ArtifactScan = srv.Fold.Artifacts
	go srv.ArtifactRetention(ctx)
	srv.QuotaHolds = func() []api.QuotaHold { return quotaHolds(srv.Runner) }
	if cfg.Daemon.Poll != "off" {
		every, _ := time.ParseDuration(cfg.Daemon.Poll)
		w := &watch.Watcher{Store: st, Fold: srv.Fold, Runner: srv.Runner, Log: srv.Log, Interval: every}
		w.Muster = &muster.Pass{Store: st, Conf: srv.Runner.Conf, Log: srv.Log, Probe: muster.DefaultProbe(srv.Fold.ArtifactDir)}
		srv.ForgeWatch = func() api.ForgeWatch { return forgeWatch(w) }
		go w.Run(ctx)
	}
	err = srv.Run(ctx, l.Runtime())
	*restart = srv.RestartRequested()
	return err
}

// quotaHolds maps the runner's quota holds to the status detail's.
func quotaHolds(r *dispatch.Runner) []api.QuotaHold {
	holds := []api.QuotaHold{}
	for _, h := range r.QuotaHolds() {
		holds = append(holds, api.QuotaHold{Agent: h.Agent, Until: h.Until})
	}
	return holds
}

// forgeWatch maps the watcher's last poll to the status detail's.
func forgeWatch(w *watch.Watcher) api.ForgeWatch {
	s := w.Status()
	return api.ForgeWatch{LastPoll: s.LastPoll, Error: s.Error}
}

// startDaemon starts the daemon, through the service manager when it is installed and
// otherwise as a detached child, and waits for it to answer. auto marks a start that a
// command made on its own, which says so and suggests installing.
func startDaemon(ctx context.Context, env Env, auto bool) (*client.Client, error) {
	l := env.Layout
	if _, err := checkConfig(l); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(l.Home, 0o700); err != nil {
		return nil, err
	}
	// What the daemon prints before it dies goes to the console file; note where this
	// start's output begins, so a failure can quote it.
	var from int64
	if fi, err := os.Stat(l.Console()); err == nil {
		from = fi.Size()
	}
	mgr, _ := serviceFor()
	how := ""
	if mgr != nil && mgr.Installed() {
		if err := mgr.Start(); err != nil {
			return nil, err
		}
		how = "through " + mgr.Name()
	} else {
		pid, err := spawn(env)
		if err != nil {
			return nil, err
		}
		how = fmt.Sprintf("pid %d", pid)
	}
	c, err := waitForDaemon(ctx, env, 10*time.Second)
	if err != nil {
		if out := consoleSince(l, from); out != "" {
			return nil, fmt.Errorf("started the daemon (%s) but it did not answer; it said:\n%s\nsee %s", how, out, l.Console())
		}
		return nil, fmt.Errorf("started the daemon (%s) but it did not answer: %w; see %s and %s", how, err, l.Console(), l.Log())
	}
	st, _ := c.Status(ctx)
	fmt.Fprintf(env.Stderr, "started shepherd daemon (pid %d, log %s)\n", st.PID, l.Log())
	if auto && (mgr == nil || !mgr.Installed()) {
		fmt.Fprintln(env.Stderr, "run `shepherd daemon install` to start it at login")
	}
	return c, nil
}

// checkConfig reads config.yaml as the daemon will, so a start that would fail on it
// says why at once instead of waiting for a daemon that has already exited. It returns
// the warnings for retired keys the file still has, which do not stop a start.
func checkConfig(l paths.Layout) ([]string, error) {
	_, warns, err := config.LoadWarn(l.Config())
	if err != nil {
		return nil, fmt.Errorf("the daemon will not start with this config: %w", err)
	}
	return warns, nil
}

func printConfigWarnings(env Env, warns []string) {
	for _, w := range warns {
		fmt.Fprintf(env.Stderr, "warning: %s: %s\n", env.Layout.Config(), w)
	}
}

// daemonCheck reads config.yaml as the daemon will and says whether it would start. It
// touches no daemon; `make install` runs it with the new binary before replacing the
// installed one.
func daemonCheck(env Env) error {
	warns, err := checkConfig(env.Layout)
	if err != nil {
		return err
	}
	printConfigWarnings(env, warns)
	fmt.Fprintf(env.Stdout, "%s: ok\n", env.Layout.Config())
	return nil
}

// consoleSince returns the last lines written to the console file after offset from,
// trimmed: what a daemon that failed to start printed.
func consoleSince(l paths.Layout, from int64) string {
	b, err := os.ReadFile(l.Console())
	if err != nil || int64(len(b)) <= from {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b[from:])), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return "  " + strings.Join(lines, "\n  ")
}

// spawn starts `shepherd daemon` detached from this terminal, its output going to the
// console file.
func spawn(env Env) (int, error) {
	logf, err := os.OpenFile(env.Layout.Console(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	cmd := exec.Command(env.Exe, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = append(scrubRunEnv(os.Environ()), paths.HomeEnv+"="+env.Layout.Home)
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}

func waitForDaemon(ctx context.Context, env Env, limit time.Duration) (*client.Client, error) {
	deadline := time.Now().Add(limit)
	for {
		c, err := connect(ctx, env)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// The steps of a restart; tests replace them.
var (
	restartDrain = daemonDrain
	restartStop  = daemonStop
	restartGone  = daemonGone
	restartStart = func(ctx context.Context, env Env) error { _, err := startDaemon(ctx, env, false); return err }
)

// daemonRestart stops the daemon and starts it again, draining it first when asked: no
// drain request is made otherwise, and the daemon is stopped at once.
func daemonRestart(ctx context.Context, env Env, drain bool, timeout time.Duration) error {
	if drain {
		if err := restartDrain(ctx, env, timeout); err != nil {
			// A daemon stopped by someone else while this restart waited (a shutdown that
			// came before "drained") is the restart's business too: no daemon is left
			// running, so go on and start one. Any other error leaves the daemon as it is.
			if !restartGone(ctx, env) {
				return err
			}
			fmt.Fprintf(env.Stdout, "the daemon stopped while the restart waited for it (%v); starting a new one\n", err)
		}
	}
	if err := restartStop(ctx, env); err != nil {
		return err
	}
	if err := restartStart(ctx, env); err != nil {
		return fmt.Errorf("the old daemon stopped but a new one did not start: %w; run `shepherd daemon start`", err)
	}
	if drain {
		fmt.Fprintln(env.Stdout, "restarted: the new daemon is running; runs the restart cut off are continued")
	} else {
		fmt.Fprintln(env.Stdout, "restarted: the new daemon is running")
	}
	return nil
}

// daemonGone waits up to stopWait for a daemon that is stopping to be gone: its runtime
// file removed or its process ended. A daemon that answers is not gone. A stopping daemon
// closes its HTTP server first and goes on ending its agents for a while, so not answering
// is not the same as gone.
func daemonGone(ctx context.Context, env Env) bool {
	deadline := time.Now().Add(stopWait)
	for {
		rt, err := daemon.ReadRuntime(env.Layout.Runtime())
		switch {
		case errors.Is(err, os.ErrNotExist):
			return true
		case err == nil && !processAlive(rt.PID):
			return true
		}
		// A file that cannot be read (half written, say) says nothing: look again.
		if _, err := connect(ctx, env); err == nil {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// drainPoll is how often a restart looks at the drain.
const drainPoll = 500 * time.Millisecond

// connectRunning is a client for the running daemon, nil when none is running. A
// daemon that is there but does not answer (after a few tries) is an error, so a restart
// does not go on without draining it.
func connectRunning(ctx context.Context, env Env) (*client.Client, error) {
	var err error
	for try := 0; try < 3; try++ {
		var c *client.Client
		if c, err = connect(ctx, env); err == nil {
			return c, nil
		}
		if _, serr := os.Stat(env.Layout.Runtime()); serr != nil {
			return nil, nil // no runtime file: no daemon
		}
		if rt, rerr := daemon.ReadRuntime(env.Layout.Runtime()); rerr != nil || !processAlive(rt.PID) {
			return nil, nil // a file left by a daemon that is gone
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("the daemon is running but did not answer (%v); it was left as it is. Try again, or restart without --drain", err)
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

// waitDrained polls the daemon until it reports nothing running, d being its answer to
// the drain. A daemon that gave the drain up is an error.
func waitDrained(ctx context.Context, env Env, c interface {
	Status(context.Context) (api.Status, error)
}, d api.Draining, timeout time.Duration) error {
	last := d.Runs
	// The daemon stops what is left at the timeout and then needs a moment to record it.
	limit := time.Now().Add(timeout + time.Minute)
	for !d.Drained {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(drainPoll):
		}
		st, err := c.Status(ctx)
		if err != nil {
			return fmt.Errorf("drain the daemon: %w", err)
		}
		if st.Draining == nil {
			// The daemon gave the drain up (its grace ran out): runs may be going again.
			return errors.New("the daemon gave up the drain; it was left running. Run the restart again")
		}
		d = *st.Draining
		if d.Runs != last && !d.Drained {
			last = d.Runs
			fmt.Fprintln(env.Stdout, d.Text(time.Now()))
		}
		if time.Now().After(limit) {
			return errors.New("the daemon did not finish draining; it was left running")
		}
	}
	return nil
}

// daemonDrain has a running daemon stop starting runs and wait, up to timeout, for the
// agents going to finish; those still going then are stopped, their sessions kept, and
// continued after the restart. It returns when the daemon has nothing running. A daemon
// that is not running has nothing to drain.
func daemonDrain(ctx context.Context, env Env, timeout time.Duration) error {
	c, err := connectRunning(ctx, env)
	if err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	d, err := c.Drain(ctx, timeout)
	if err != nil {
		var se *client.StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			// A daemon from before the drain: nothing to wait for, but it can restart.
			fmt.Fprintln(env.Stderr, "this daemon is too old to drain; restarting it as it is")
			return nil
		}
		return fmt.Errorf("drain the daemon: %w (a running daemon was left as it is)", err)
	}
	if d.Runs > 0 {
		fmt.Fprintf(env.Stdout, "%s; new starts are held until after the restart\n", d.Text(time.Now()))
	}
	if err := waitDrained(ctx, env, c, d, timeout); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "drained: restarting")
	return nil
}

// stopWait is how long stop waits for the daemon to exit.
const stopWait = 30 * time.Second

func daemonStop(ctx context.Context, env Env) error {
	c, err := connect(ctx, env)
	if err != nil {
		fmt.Fprintln(env.Stdout, "not running")
		return nil
	}
	st, _ := c.Status(ctx)
	if err := c.Shutdown(ctx); err != nil {
		return err
	}
	// A stopping daemon gives its agents up to 15 seconds to end (after 5 for requests in
	// flight) before it lets go of the runtime file, so restart must wait that long or
	// it starts a second daemon into the first one's lock.
	deadline := time.Now().Add(stopWait)
	for {
		if _, err := os.Stat(env.Layout.Runtime()); errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(env.Stdout, "stopped (pid %d)\n", st.PID)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("asked pid %d to stop, but it is still running", st.PID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// daemonReload has the running daemon read config.yaml again (SIGHUP), then says what
// the daemon made of it: what changed, or why it kept the config it had.
func daemonReload(ctx context.Context, env Env) error {
	if runtime.GOOS == "windows" {
		return errors.New("daemon reload is not supported on Windows yet; use shepherd daemon restart")
	}
	c, err := connect(ctx, env)
	if err != nil {
		return fmt.Errorf("the daemon is not running; it reads %s when it starts", env.Layout.Config())
	}
	// The daemon's reload says only that it took; the warnings for retired keys are
	// read here, the same way.
	if _, warns, err := config.LoadWarn(env.Layout.Config()); err == nil {
		printConfigWarnings(env, warns)
	}
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	feed, err := c.Feed(ctx, -1)
	if err != nil {
		return err
	}
	p, err := os.FindProcess(st.PID)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("signal the daemon (pid %d): %w", st.PID, err)
	}
	after := feed.Last
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		feed, err := c.Feed(ctx, after)
		if err != nil {
			return err
		}
		for _, it := range feed.Items {
			if it.Kind != daemon.FeedConfig {
				continue
			}
			if !strings.HasPrefix(it.Text, daemon.ReloadedPrefix) {
				return errors.New(it.Text)
			}
			fmt.Fprintln(env.Stdout, it.Text)
			return nil
		}
		after = feed.Last
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("asked pid %d to reload, but it has not said whether it did; see %s", st.PID, env.Layout.Log())
}

func daemonStatus(ctx context.Context, env Env) error {
	w := env.Stdout
	if c, err := connect(ctx, env); err == nil {
		st, _ := c.Status(ctx)
		fmt.Fprintf(w, "daemon   running, pid %d, version %s, since %s\n", st.PID, st.Version, st.Started.Local().Format("2006-01-02 15:04"))
		if st.Draining != nil {
			fmt.Fprintf(w, "         %s\n", st.Draining.Text(time.Now()))
		}
	} else {
		fmt.Fprintln(w, "daemon   not running")
	}
	mgr, err := serviceFor()
	switch {
	case err != nil:
		fmt.Fprintf(w, "login    %v\n", err)
	case mgr.Installed():
		fmt.Fprintf(w, "login    starts at login through %s (%s)\n", mgr.Name(), mgr.File())
	default:
		fmt.Fprintln(w, "login    not installed: `shepherd daemon install` starts it at login")
	}
	fmt.Fprintf(w, "home     %s\n", env.Layout.Home)
	fmt.Fprintf(w, "log      %s\n", env.Layout.Log())
	return nil
}

func daemonInstall(ctx context.Context, env Env) error {
	mgr, err := serviceFor()
	if err != nil {
		return err
	}
	if env.Exe == "" {
		return errors.New("cannot find this binary's path")
	}
	if warn := service.Warning(env.Exe); warn != "" {
		fmt.Fprintf(env.Stderr, "warning: registering %s: %s\n", env.Exe, warn)
	}
	if _, err := checkConfig(env.Layout); err != nil {
		return err
	}
	// A daemon started by hand would hold the lock and make the managed one fail.
	if _, err := connect(ctx, env); err == nil {
		if err := daemonStop(ctx, env); err != nil {
			return err
		}
	}
	spec := service.Spec{Exe: env.Exe, Log: env.Layout.Log(), Console: env.Layout.Console(), Path: os.Getenv("PATH")}
	if os.Getenv(paths.HomeEnv) != "" {
		spec.Home = env.Layout.Home
	}
	if err := os.MkdirAll(env.Layout.Home, 0o700); err != nil {
		return err
	}
	if err := mgr.Install(spec); err != nil {
		return err
	}
	w := env.Stdout
	fmt.Fprintf(w, "installed %s\n", mgr.File())
	fmt.Fprintf(w, "  runs   %s daemon\n", spec.Exe)
	fmt.Fprintf(w, "  PATH   %s\n", spec.Path)
	fmt.Fprintf(w, "  log    %s (rotated at %d MB, %d old files kept)\n", spec.Log, daemon.LogMaxBytes>>20, daemon.LogKeep)
	fmt.Fprintf(w, "  other  %s (panics and startup errors)\n", spec.Console)
	fmt.Fprintln(w, "It starts at login and restarts if it crashes; `shepherd daemon stop` stops it until the next login.")
	if mgr.Name() == "systemd" {
		fmt.Fprintln(w, "To keep it running while you are logged out: loginctl enable-linger $USER")
	}
	if _, err := waitForDaemon(ctx, env, 10*time.Second); err != nil {
		return fmt.Errorf("installed, but the daemon did not answer: %w; see %s and %s", err, spec.Console, spec.Log)
	}
	fmt.Fprintln(w, "daemon is running")
	return nil
}

func daemonUninstall(ctx context.Context, env Env) error {
	mgr, err := serviceFor()
	if err != nil {
		return err
	}
	if !mgr.Installed() {
		fmt.Fprintln(env.Stdout, "not installed")
		return nil
	}
	if err := mgr.Uninstall(); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "removed %s; the daemon is stopped and will not start at login\n", mgr.File())
	return nil
}
