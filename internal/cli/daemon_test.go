package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	stopped, started := 0, 0
	oldDrain, oldStop, oldStart := restartDrain, restartStop, restartStart
	t.Cleanup(func() { restartDrain, restartStop, restartStart = oldDrain, oldStop, oldStart })
	restartStop = func(context.Context, Env) error { stopped++; return nil }
	restartStart = func(context.Context, Env) error { started++; return nil }
	// --timeout 0 asks the daemon for nothing: it is stopped and started at once.
	if code := h.run("daemon", "restart", "--drain", "--timeout", "0"); code != 0 {
		t.Fatalf("restart: %d %q", code, h.err)
	}
	if stopped != 1 || started != 1 {
		t.Errorf("stopped %d, started %d", stopped, started)
	}
	if h.srv.Runner.Draining() != nil {
		t.Error("the daemon was asked to drain")
	}
	// Without --drain it is the same.
	if code := h.run("daemon", "restart"); code != 0 || h.srv.Runner.Draining() != nil {
		t.Errorf("plain restart: %d, draining %v", code, h.srv.Runner.Draining())
	}
	// With --drain the daemon is drained first.
	if code := h.run("daemon", "restart", "--drain", "--timeout", "1m"); code != 0 || h.srv.Runner.Draining() == nil {
		t.Errorf("drained restart: %d, draining %v", code, h.srv.Runner.Draining())
	}
}

// fakeDaemon answers status, and the drain with the given code and body.
func fakeDaemon(t *testing.T, drainCode int, drainBody string) Env {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.PathStatus {
			json.NewEncoder(w).Encode(api.Status{})
			return
		}
		w.WriteHeader(drainCode)
		io.WriteString(w, drainBody)
	}))
	t.Cleanup(ts.Close)
	l := paths.Layout{Home: t.TempDir()}
	rt, _ := json.Marshal(api.Runtime{Addr: strings.TrimPrefix(ts.URL, "http://"), Token: "t"})
	os.WriteFile(l.Runtime(), rt, 0o600)
	return Env{Stdout: io.Discard, Stderr: io.Discard, Layout: l, Cwd: l.Home}
}

func TestDrainFallbackKeysOnTheHTTPStatus(t *testing.T) {
	ctx := context.Background()
	// A daemon with no drain route answers 404: it is restarted as it is.
	if err := daemonDrain(ctx, fakeDaemon(t, http.StatusNotFound, "404 page not found"), time.Minute); err != nil {
		t.Errorf("old daemon: %v", err)
	}
	// Any other failure stops the restart, whatever digits its message holds.
	err := daemonDrain(ctx, fakeDaemon(t, http.StatusInternalServerError, `{"error":"disk 404 full"}`), time.Minute)
	if err == nil || !strings.Contains(err.Error(), "left as it is") {
		t.Errorf("a 500 mentioning 404: %v", err)
	}
}

func TestHeldRunIsADistinctResult(t *testing.T) {
	var out bytes.Buffer
	err := started(context.Background(), Env{Stdout: &out}, nil, api.RunView{Held: "Shepherd is restarting; the run starts after the restart"}, true)
	if !errors.Is(err, errHeld) || !strings.Contains(out.String(), "not started yet") {
		t.Errorf("held start: %v %q", err, out.String())
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
