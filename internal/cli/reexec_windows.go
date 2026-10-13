//go:build windows

package cli

// reexec starts the daemon from the binary now at env.Exe as a detached child; this
// process then exits. Windows has no exec, and no service manager to keep in step.
func reexec(env Env) error {
	_, err := spawn(env)
	return err
}
