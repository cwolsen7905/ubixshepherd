//go:build unix

package daemon_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/cli"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// TestDaemonReloadCommand runs `shepherd daemon reload` against a daemon in this
// process: the SIGHUP it sends comes back here.
func TestDaemonReloadCommand(t *testing.T) {
	l := paths.Layout{Home: t.TempDir()}
	reload := func() (int, string, string) {
		var out, errb bytes.Buffer
		env := cli.Env{Stdout: &out, Stderr: &errb, Layout: l, Cwd: l.Home, Client: "cli"}
		code := cli.Run(context.Background(), env, []string{"daemon", "reload"})
		return code, out.String(), errb.String()
	}

	if code, _, stderr := reload(); code != 1 || !strings.Contains(stderr, "the daemon is not running") {
		t.Fatalf("no daemon: exit %d, %q", code, stderr)
	}

	st, err := sqlite.Open(context.Background(), l.Store())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := daemon.NewServer(st, config.Default(), l.Config(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, l.Runtime()) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(l.Runtime()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not start")
		}
	}

	write := func(yaml string) {
		if err := os.WriteFile(filepath.Join(l.Home, "config.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("daemon:\n  max_runs: 2\n")
	code, stdout, stderr := reload()
	if code != 0 || strings.TrimSpace(stdout) != "config reloaded: daemon.max_runs 4 to 2" {
		t.Fatalf("reload: exit %d, %q, %q", code, stdout, stderr)
	}
	if s.LiveConfig().Daemon.MaxRuns != 2 {
		t.Error("the daemon did not take the new config")
	}

	write("daemon:\n  max_runs: 3\n  bogus: 1\n")
	code, _, stderr = reload()
	if code != 1 || !strings.Contains(stderr, "config not reloaded") || !strings.Contains(stderr, "bogus") {
		t.Errorf("refused reload: exit %d, %q", code, stderr)
	}
	if s.LiveConfig().Daemon.MaxRuns != 2 {
		t.Error("a refused reload replaced the config")
	}
}
