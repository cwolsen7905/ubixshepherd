//go:build unix

package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:05":       5 * time.Second,
		"12:34":       12*time.Minute + 34*time.Second,
		"01:00:00":    time.Hour,
		"2-03:04:05":  2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second,
		"10-00:00:00": 10 * 24 * time.Hour,
	} {
		if got, ok := parseEtime(in); !ok || got != want {
			t.Errorf("parseEtime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "5", "a:b", "x-01:00"} {
		if _, ok := parseEtime(in); ok {
			t.Errorf("parseEtime(%q) took it", in)
		}
	}
}

// reapStore lists the given runs as running and keeps the feed.
type reapStore struct {
	store.Store
	runs []store.Run
	feed []string
}

func (r *reapStore) Runs(context.Context, int64, string, int) ([]store.Run, error) {
	return r.runs, nil
}

func (r *reapStore) AddFeed(_ context.Context, _, text string, _ int64) error {
	r.feed = append(r.feed, text)
	return nil
}

// sleeper starts a process that waits a minute; its channel closes when it exits.
func sleeper(t *testing.T, group bool) (int, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if group {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-done })
	return cmd.Process.Pid, done
}

func exitsWithin(done <-chan struct{}, d time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestReapOrphans(t *testing.T) {
	agent, agentDone := sleeper(t, true)  // an agent the last daemon left running
	other, otherDone := sleeper(t, false) // a reused pid: not a process group leader
	young, youngDone := sleeper(t, true)  // a reused pid: started long after its run
	now := time.Now().UTC()
	st := &reapStore{runs: []store.Run{
		{ID: 1, Agent: "claude", PID: agent, Started: now},
		{ID: 2, Agent: "claude", PID: other, Started: now},
		{ID: 3, Agent: "claude", PID: young, Started: now.Add(-time.Hour)},
		{ID: 4, Agent: "cursor"}, // no pid recorded
	}}
	s, _ := newServer(t)
	s.Store = st
	s.ReapOrphans(context.Background())

	if !exitsWithin(agentDone, 3*time.Second) {
		t.Error("the leftover agent is still running")
	}
	if exitsWithin(otherDone, 300*time.Millisecond) || exitsWithin(youngDone, 300*time.Millisecond) {
		t.Error("a pid that is not the run's agent was signalled")
	}
	if len(st.feed) != 4 || !strings.Contains(st.feed[0], "so Shepherd stopped it") ||
		strings.Contains(st.feed[1]+st.feed[2]+st.feed[3], "stopped it") {
		t.Errorf("feed = %q", st.feed)
	}
}

func TestSIGHUPReloads(t *testing.T) {
	s, path := reloadServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := watchReload(ctx, s)
	defer func() { cancel(); <-done }()

	if err := os.WriteFile(path, []byte("daemon:\n  max_runs: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	for deadline := time.Now().Add(5 * time.Second); s.LiveConfig().Daemon.MaxRuns != 7; {
		if time.Now().After(deadline) {
			t.Fatal("SIGHUP did not reload the config")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
