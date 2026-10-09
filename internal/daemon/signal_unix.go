//go:build unix

package daemon

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// watchReload reloads the config on each SIGHUP until ctx is done. The returned channel
// closes when it has stopped listening.
func watchReload(ctx context.Context, s *Server) <-chan struct{} {
	done := make(chan struct{})
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer close(done)
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				s.ReloadConfig(context.Background())
			}
		}
	}()
	return done
}

// leftoverAgent says whether pid is still an agent Shepherd started at about started:
// it leads its own process group, as every agent does, and has been running since then.
func leftoverAgent(pid int, started time.Time) bool {
	out, err := exec.Command("ps", "-o", "pgid=,etime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // no such process
	}
	f := strings.Fields(string(out))
	if len(f) != 2 || f[0] != strconv.Itoa(pid) {
		return false
	}
	age, ok := parseEtime(f[1])
	return ok && startedNear(age, started)
}

// parseEtime reads ps's elapsed time, [[dd-]hh:]mm:ss.
func parseEtime(s string) (time.Duration, bool) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		secs = secs*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(secs)*time.Second, true
}

// stopGroup ends an agent and the tools it started.
func stopGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
