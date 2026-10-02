package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var spec = Spec{
	Exe:  "/Users/a b/.local/bin/shepherd",
	Home: "/Users/a b/.shepherd-test",
	Log:  "/Users/a b/.shepherd/daemon.log",
	Path: "/opt/homebrew/bin:/usr/bin:/bin&more",
}

func TestPlistIsWellFormed(t *testing.T) {
	b := Plist(spec)
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = true
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, b)
		}
	}
	s := string(b)
	for _, want := range []string{
		"<string>/Users/a b/.local/bin/shepherd</string>",
		"/bin&amp;more",
		"<key>SHEPHERD_HOME</key>",
		"<key>SuccessfulExit</key>\n\t\t<false/>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if strings.Contains(string(Plist(Spec{Exe: "x", Path: "p", Log: "l"})), "SHEPHERD_HOME") {
		t.Error("SHEPHERD_HOME written when unset")
	}
}

func TestUnit(t *testing.T) {
	s := string(Unit(spec))
	for _, want := range []string{
		`ExecStart="/Users/a b/.local/bin/shepherd" daemon`,
		`Environment="PATH=/opt/homebrew/bin:/usr/bin:/bin&more"`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("unit lacks %q:\n%s", want, s)
		}
	}
}

func TestLaunchdInstallAndUninstall(t *testing.T) {
	var calls []string
	runCmd = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() { runCmd = defaultRun })

	l := Launchd{Dir: t.TempDir(), UID: 501}
	if l.Installed() {
		t.Fatal("installed before install")
	}
	if err := l.Install(spec); err != nil {
		t.Fatal(err)
	}
	if !l.Installed() {
		t.Error("not installed after install")
	}
	if got := calls[len(calls)-1]; got != "launchctl bootstrap gui/501 "+l.File() {
		t.Errorf("last call = %q", got)
	}
	if err := l.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.File()); !os.IsNotExist(err) {
		t.Error("plist left behind")
	}
	if err := l.Uninstall(); err != nil {
		t.Errorf("second uninstall: %v", err)
	}
}

func TestSystemdInstall(t *testing.T) {
	var calls []string
	runCmd = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() { runCmd = defaultRun })

	u := Systemd{Dir: filepath.Join(t.TempDir(), "systemd", "user")}
	if err := u.Install(spec); err != nil {
		t.Fatal(err)
	}
	if !u.Installed() || calls[len(calls)-1] != "systemctl --user enable --now shepherd.service" {
		t.Errorf("calls = %v", calls)
	}
}

func TestWarning(t *testing.T) {
	if Warning("/opt/homebrew/Cellar/shepherd/0.1.0/bin/shepherd") == "" {
		t.Error("Cellar path not flagged")
	}
	if Warning(filepath.Join(os.TempDir(), "x", "shepherd")) == "" {
		t.Error("temp path not flagged")
	}
	if w := Warning("/nonexistent/.local/bin/shepherd"); w != "" {
		t.Errorf("stable path flagged: %s", w)
	}
}
