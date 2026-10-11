//go:build unix

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/paths"
)

func TestIsDaemonRun(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"daemon"}, true},
		{[]string{"daemon", "run"}, true},
		{[]string{"--local", "daemon", "run"}, true},
		{[]string{"daemon", "--force-from-run"}, true},
		{[]string{"daemon", "reload"}, false},
		{[]string{"daemon", "stop"}, false},
		{[]string{"daemon", "status"}, false},
		{[]string{"chat"}, false},
		{[]string{"lane", "run", "daemon"}, false},
		{nil, false},
	} {
		if got := isDaemonRun(tc.args); got != tc.want {
			t.Errorf("isDaemonRun(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestDaemonSurvivesSIGHUP starts the daemon with the context Main gives `shepherd
// daemon`, then runs `shepherd daemon reload`, whose SIGHUP comes back to this process:
// the daemon must reload and keep serving, not shut down.
func TestDaemonSurvivesSIGHUP(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	env := Env{Stdout: io.Discard, Stderr: io.Discard, Layout: l, Cwd: l.Home, Client: "cli"}

	ctx, stop := notifyContext(context.Background(), []string{"daemon"})
	defer stop()
	done := make(chan error, 1)
	go func() { done <- daemonRun(ctx, env) }()
	defer func() {
		stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the daemon did not stop")
		}
	}()
	waitServing := func() {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			if _, err := connect(context.Background(), env); err == nil {
				return
			}
			select {
			case err := <-done:
				t.Fatalf("the daemon exited: %v", err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("the daemon is not serving")
			}
		}
	}
	waitServing()

	if err := os.WriteFile(filepath.Join(l.Home, "config.yaml"), []byte("daemon:\n  max_runs: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	renv := env
	renv.Stdout, renv.Stderr = &out, &errb
	if code := Run(context.Background(), renv, []string{"daemon", "reload"}); code != 0 {
		t.Fatalf("reload: exit %d, %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "config reloaded") {
		t.Errorf("reload said %q", out.String())
	}

	// A SIGHUP delivered a moment later would show a late shutdown.
	time.Sleep(300 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("SIGHUP cancelled the daemon's context")
	}
	select {
	case err := <-done:
		t.Fatalf("the daemon stopped after SIGHUP: %v", err)
	default:
	}
	waitServing()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("a bare SIGHUP cancelled the daemon's context")
	}
	waitServing()
}
