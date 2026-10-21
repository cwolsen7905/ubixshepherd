//go:build !windows

package muster

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid is a running process. A process that exists but is
// not ours (EPERM) counts as alive.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
