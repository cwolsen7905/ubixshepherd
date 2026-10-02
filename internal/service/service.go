// Package service registers the daemon with the OS's per-user service manager, so it
// starts at login and restarts if it crashes: launchd on macOS, systemd --user on Linux.
//
// The daemon exits 0 when asked to stop, and both managers are told to restart it only
// on failure, so a deliberate stop stays stopped until the next login or start.
package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrUnsupported means this OS has no service manager support yet.
var ErrUnsupported = errors.New("starting at login is not supported on this OS yet; use: shepherd daemon start")

// Spec is what the service runs.
type Spec struct {
	// Exe is the shepherd binary, absolute.
	Exe string
	// Home is SHEPHERD_HOME when it was set, so the service uses the same home.
	Home string
	// Log receives the daemon's output.
	Log string
	// Path is the PATH the daemon gets. Service managers start programs with a bare
	// PATH, which would hide git and the agent CLIs.
	Path string
}

// Manager is one OS's service manager.
type Manager interface {
	// Name is "launchd" or "systemd".
	Name() string
	// File is the unit or plist path.
	File() string
	// Installed reports whether the file exists.
	Installed() bool
	// Install writes the file and loads it, which starts the daemon.
	Install(Spec) error
	// Uninstall stops the service and removes the file.
	Uninstall() error
	// Start starts an installed service that is not running.
	Start() error
}

// runCmd runs a command and returns its combined output; tests replace it.
var runCmd = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

var defaultRun = runCmd

func run(name string, args ...string) error {
	out, err := runCmd(name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

// For returns the manager for this OS.
func For() (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	switch runtime.GOOS {
	case "darwin":
		return Launchd{Dir: filepath.Join(home, "Library", "LaunchAgents"), UID: os.Getuid()}, nil
	case "linux":
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return Systemd{Dir: filepath.Join(cfg, "systemd", "user")}, nil
	}
	return nil, ErrUnsupported
}

// Label names the service in both managers.
const Label = "com.ubixsys.shepherd"

// Launchd is a macOS LaunchAgent.
type Launchd struct {
	Dir string
	UID int
}

func (l Launchd) Name() string    { return "launchd" }
func (l Launchd) File() string    { return filepath.Join(l.Dir, Label+".plist") }
func (l Launchd) Installed() bool { return exists(l.File()) }
func (l Launchd) domain() string  { return "gui/" + strconv.Itoa(l.UID) }
func (l Launchd) target() string  { return l.domain() + "/" + Label }
func (l Launchd) Start() error    { return run("launchctl", "kickstart", l.target()) }
func (l Launchd) Install(s Spec) error {
	if err := writeFile(l.File(), Plist(s)); err != nil {
		return err
	}
	runCmd("launchctl", "bootout", l.target()) // not loaded yet is fine
	return run("launchctl", "bootstrap", l.domain(), l.File())
}

func (l Launchd) Uninstall() error {
	runCmd("launchctl", "bootout", l.target())
	return removeFile(l.File())
}

// Plist renders the LaunchAgent.
func Plist(s Spec) []byte {
	var b bytes.Buffer
	esc := func(v string) string {
		var e bytes.Buffer
		xml.EscapeText(&e, []byte(v))
		return e.String()
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by shepherd daemon install; remove with shepherd daemon uninstall. -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(s.Exe) + `</string>
		<string>daemon</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + esc(s.Path) + `</string>
`)
	if s.Home != "" {
		b.WriteString("\t\t<key>SHEPHERD_HOME</key>\n\t\t<string>" + esc(s.Home) + "</string>\n")
	}
	b.WriteString(`	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>` + esc(s.Log) + `</string>
	<key>StandardErrorPath</key>
	<string>` + esc(s.Log) + `</string>
</dict>
</plist>
`)
	return b.Bytes()
}

// Systemd is a systemd user unit.
type Systemd struct {
	Dir string
}

const unitName = "shepherd.service"

func (u Systemd) Name() string    { return "systemd" }
func (u Systemd) File() string    { return filepath.Join(u.Dir, unitName) }
func (u Systemd) Installed() bool { return exists(u.File()) }
func (u Systemd) Start() error    { return run("systemctl", "--user", "start", unitName) }
func (u Systemd) Install(s Spec) error {
	if err := writeFile(u.File(), Unit(s)); err != nil {
		return err
	}
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "--user", "enable", "--now", unitName)
}

func (u Systemd) Uninstall() error {
	runCmd("systemctl", "--user", "disable", "--now", unitName)
	if err := removeFile(u.File()); err != nil {
		return err
	}
	return run("systemctl", "--user", "daemon-reload")
}

// Unit renders the systemd user unit.
func Unit(s Spec) []byte {
	q := func(v string) string { return strconv.Quote(v) }
	var b strings.Builder
	b.WriteString("# Written by shepherd daemon install; remove with shepherd daemon uninstall.\n")
	b.WriteString("[Unit]\nDescription=uBixShepherd daemon\n\n[Service]\n")
	b.WriteString("ExecStart=" + q(s.Exe) + " daemon\n")
	b.WriteString("Environment=" + q("PATH="+s.Path) + "\n")
	if s.Home != "" {
		b.WriteString("Environment=" + q("SHEPHERD_HOME="+s.Home) + "\n")
	}
	b.WriteString("Restart=on-failure\nRestartSec=10\n")
	b.WriteString("StandardOutput=append:" + s.Log + "\nStandardError=append:" + s.Log + "\n")
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return []byte(b.String())
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func removeFile(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Warning returns a reason the binary at exe is a poor thing to register, or "". The
// service keeps the path, so a path that an upgrade or a clean removes breaks it.
func Warning(exe string) string {
	switch {
	case strings.Contains(exe, "/Cellar/"):
		return "this is a versioned Homebrew path that the next upgrade removes; use `brew services` or the path in Homebrew's bin"
	case strings.HasPrefix(exe, os.TempDir()) || strings.Contains(exe, "/go-build"):
		return "this binary is in a temporary directory"
	case exists(filepath.Join(filepath.Dir(filepath.Dir(exe)), "go.mod")):
		return "this looks like a development build; `make clean` would remove it"
	}
	return ""
}
