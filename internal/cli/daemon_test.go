package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/watch"
)

const badConfig = "repos:\n  x:\n    autonomy:\n      merge: sometimes\n"

func TestStartRefusesBadConfig(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	if err := os.WriteFile(l.Config(), []byte(badConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	errOut := &bytes.Buffer{}
	env := Env{Stdout: io.Discard, Stderr: errOut, Layout: l, Cwd: l.Home}
	if code := Run(context.Background(), env, []string{"daemon", "start"}); code != 1 {
		t.Fatalf("daemon start: %d", code)
	}
	if !strings.Contains(errOut.String(), `"sometimes" is not human or agent`) {
		t.Errorf("the config error is not shown: %q", errOut)
	}
	if _, err := os.Stat(l.Console()); err == nil {
		t.Error("a daemon was started anyway")
	}
}

func TestRestartKeepsDaemonOnBadConfig(t *testing.T) {
	h := newHarness(t, "", false)
	if err := os.WriteFile(h.env.Layout.Config(), []byte(badConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("daemon", "restart"); code != 1 || !strings.Contains(h.err.String(), "is not human or agent") {
		t.Fatalf("daemon restart: %d %q", code, h.err)
	}
	if h.run("daemon", "status"); !strings.Contains(h.out.String(), "running, pid") {
		t.Errorf("restart stopped the daemon: %q", h.out)
	}
}

func TestConsoleSince(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	if got := consoleSince(l, 0); got != "" {
		t.Errorf("no console file: %q", got)
	}
	old := "an earlier start\n"
	var b strings.Builder
	b.WriteString(old)
	for i := 0; i < 12; i++ {
		b.WriteString("line " + string(rune('a'+i)) + "\n")
	}
	if err := os.WriteFile(l.Console(), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := consoleSince(l, int64(len(old)))
	if strings.Contains(got, "earlier") || strings.Contains(got, "line b") || !strings.HasPrefix(got, "  line c\n") || !strings.HasSuffix(got, "  line l") {
		t.Errorf("consoleSince = %q", got)
	}
	if got := consoleSince(l, int64(b.Len())); got != "" {
		t.Errorf("nothing new: %q", got)
	}
}

func TestStatusHooksMapWhatTheRunnerAndWatcherKnow(t *testing.T) {
	// Nothing held and nothing polled is an empty list and a zero poll, not "unreported".
	if h := quotaHolds(&dispatch.Runner{}); h == nil || len(h) != 0 {
		t.Errorf("quota holds of an idle runner = %#v", h)
	}
	if fw := forgeWatch(&watch.Watcher{}); !fw.LastPoll.IsZero() || fw.Error != "" {
		t.Errorf("forge watch before a poll = %+v", fw)
	}
}

func TestDaemonCheck(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Layout: l, Cwd: l.Home}
	run := func(cfg string) int {
		out.Reset()
		errOut.Reset()
		if err := os.WriteFile(l.Config(), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		return Run(context.Background(), env, []string{"daemon", "check"})
	}
	if code := run("daemon:\n  max_runs: 2\n"); code != 0 || !strings.Contains(out.String(), "ok") || errOut.Len() != 0 {
		t.Errorf("valid config: %d out=%q err=%q", code, out.String(), errOut.String())
	}
	if code := run(badConfig); code != 1 || !strings.Contains(errOut.String(), "is not human or agent") {
		t.Errorf("bad config: %d err=%q", code, errOut.String())
	}
	// A key that is only a typo still fails; a retired one warns and passes.
	old := config.RetiredKeys
	config.RetiredKeys = map[string]string{"daemon.old_poll": "use daemon.poll"}
	t.Cleanup(func() { config.RetiredKeys = old })
	if code := run("daemon:\n  old_poll: 30s\n"); code != 0 ||
		!strings.Contains(errOut.String(), "daemon.old_poll: retired key, ignored: use daemon.poll") || !strings.Contains(out.String(), "ok") {
		t.Errorf("retired key: %d out=%q err=%q", code, out.String(), errOut.String())
	}
	if code := run("daemon:\n  old_pol: 30s\n"); code != 1 {
		t.Errorf("typo: %d", code)
	}
}

func TestRestartDrainWaitsForTheDaemonToBeDrained(t *testing.T) {
	h := newHarness(t, "", false)
	if err := daemonDrain(context.Background(), h.env, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "drained: restarting") {
		t.Errorf("output = %q", h.out)
	}
	// The daemon reports it, in the status words, until it is stopped.
	if h.run("daemon", "status"); !strings.Contains(h.out.String(), "drained (restarting)") {
		t.Errorf("status = %q", h.out)
	}
}

func TestDrainWithAZeroTimeoutIsToday(t *testing.T) {
	h := newHarness(t, "", false)
	if err := os.WriteFile(h.env.Layout.Config(), []byte(badConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// The bad config stops the restart before anything is drained or stopped.
	if code := h.run("daemon", "restart", "--drain", "--timeout", "0"); code != 1 {
		t.Fatalf("restart: %d %q", code, h.err)
	}
	if h.run("daemon", "status"); strings.Contains(h.out.String(), "drain") {
		t.Errorf("the daemon was drained: %q", h.out)
	}
}

func TestConnectRunningTellsNoDaemonFromAnUnansweringOne(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	env := Env{Stdout: io.Discard, Stderr: io.Discard, Layout: l, Cwd: l.Home}
	if c, err := connectRunning(context.Background(), env); c != nil || err != nil {
		t.Errorf("no runtime file: %v %v", c, err)
	}
	// A file left by a daemon that is gone is no daemon.
	os.WriteFile(l.Runtime(), []byte(`{"addr":"127.0.0.1:1","pid":2147483646,"token":"x"}`), 0o600)
	if c, err := connectRunning(context.Background(), env); c != nil || err != nil {
		t.Errorf("stale runtime file: %v %v", c, err)
	}
	// A live process that does not answer is an error: the restart must not go on.
	os.WriteFile(l.Runtime(), []byte(fmt.Sprintf(`{"addr":"127.0.0.1:1","pid":%d,"token":"x"}`, os.Getpid())), 0o600)
	if c, err := connectRunning(context.Background(), env); c != nil || err == nil {
		t.Errorf("unanswering daemon: %v %v", c, err)
	}
}

type fakeStatus struct{ st api.Status }

func (f fakeStatus) Status(context.Context) (api.Status, error) { return f.st, nil }

func TestRestartDrainRefusesWhenTheDaemonGaveTheDrainUp(t *testing.T) {
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: io.Discard}
	// The daemon no longer reports a drain: its grace ran out and it took starts again.
	err := waitDrained(context.Background(), env, fakeStatus{}, api.Draining{Runs: 1, Until: time.Now().Add(time.Minute)}, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "gave up the drain") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out.String(), "drained") {
		t.Errorf("said drained: %q", out.String())
	}
	// One that reports Drained lets the restart go on.
	done := fakeStatus{api.Status{Draining: &api.Draining{Drained: true}}}
	if err := waitDrained(context.Background(), env, done, api.Draining{Runs: 1}, time.Minute); err != nil {
		t.Errorf("drained daemon: %v", err)
	}
}
