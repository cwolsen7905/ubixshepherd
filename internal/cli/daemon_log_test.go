package cli

import (
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/paths"
)

// What a manager or a spawning parent captures must not be the file the daemon rotates.
func TestConsoleLogIsNotTheDaemonLog(t *testing.T) {
	l := paths.Layout{Home: filepath.Join(t.TempDir(), "home")}
	if consoleLog(l) == l.Log() {
		t.Errorf("console and log are both %s", l.Log())
	}
	if filepath.Dir(consoleLog(l)) != l.Home {
		t.Errorf("console log %s is outside the home", consoleLog(l))
	}
}
