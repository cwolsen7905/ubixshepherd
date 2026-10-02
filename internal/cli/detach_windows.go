//go:build windows

package cli

import "syscall"

// detached starts the child without a console and outside the caller's process group, so
// closing the terminal does not stop it.
func detached() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
