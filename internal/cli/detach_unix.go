//go:build unix

package cli

import "syscall"

// detached starts the child in its own session, so closing the terminal does not stop it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
