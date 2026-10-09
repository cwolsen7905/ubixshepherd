//go:build windows

package daemon

import (
	"context"
	"errors"
	"time"
)

// watchReload does nothing on Windows, which has no SIGHUP; reloading there needs a
// restart until the API offers a reload.
func watchReload(context.Context, *Server) <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// leftoverAgent cannot yet tell an agent from a process that reused its pid on Windows,
// so a leftover agent is never stopped there; its run is still marked interrupted.
func leftoverAgent(int, time.Time) bool { return false }

func stopGroup(int) error { return errors.New("not supported on Windows") }
