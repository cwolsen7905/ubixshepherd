//go:build unix

package cli

import (
	"os"
	"syscall"

	"github.com/ubixsys/ubixshepherd/internal/paths"
)

// reexec replaces this process with the daemon from the binary now at env.Exe. The pid
// stays, so a service manager sees one daemon that was never stopped.
func reexec(env Env) error {
	environ := append(scrubRunEnv(os.Environ()), paths.HomeEnv+"="+env.Layout.Home)
	return syscall.Exec(env.Exe, []string{env.Exe, "daemon"}, environ)
}
