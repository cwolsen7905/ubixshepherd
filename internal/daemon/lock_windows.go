//go:build windows

package daemon

import (
	"os"

	"golang.org/x/sys/windows"
)

// acquireLock takes an exclusive, non-blocking lock on path. The OS releases it when the
// process exits, however it exits, so a crash never leaves a stale lock.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
