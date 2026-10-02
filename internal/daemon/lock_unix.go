//go:build unix

package daemon

import (
	"os"
	"syscall"
)

// acquireLock takes an exclusive, non-blocking lock on path. The OS releases it when the
// process exits, however it exits, so a crash never leaves a stale lock.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
