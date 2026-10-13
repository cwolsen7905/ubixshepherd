package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/update"
)

// shepherd update status and dismiss are a client of the daemon's cached release check.
func TestUpdateCLI(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"v0.9.0-beta.1","html_url":"https://example.test/r","prerelease":true}]`))
	}))
	t.Cleanup(gh.Close)
	h := newHarness(t, "", false)
	h.srv.Updates = &update.Checker{
		URL: gh.URL, Current: "v0.8.0", StatePath: filepath.Join(t.TempDir(), "update.json"),
		Settings: func() (bool, string) { return true, "beta" },
	}

	// Before a check has succeeded there is nothing to report or dismiss.
	if code := h.run("update"); code != 0 || !strings.Contains(h.out.String(), "not found yet") {
		t.Fatalf("before: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("update", "dismiss"); code == 0 {
		t.Error("dismissed with nothing found")
	}
	if err := h.srv.Updates.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := h.run("update", "status"); code != 0 || !strings.Contains(h.out.String(), "v0.9.0-beta.1") ||
		!strings.Contains(h.out.String(), "newer release is available") {
		t.Fatalf("status: %d %s%s", code, h.out, h.err)
	}
	if code := h.run("update", "dismiss", "--json"); code != 0 {
		t.Fatalf("dismiss: %d %s", code, h.err)
	}
	var st api.UpdateStatus
	if err := json.Unmarshal(h.out.Bytes(), &st); err != nil || !st.Dismissed || st.Show || st.Latest != "v0.9.0-beta.1" {
		t.Fatalf("dismissed: %v %+v", err, st)
	}
	if code := h.run("update", "sideways"); code == 0 {
		t.Error("an unknown subcommand was accepted")
	}
}

// shepherd update upgrade asks the daemon to install; it says what to run instead when the
// install is not one the daemon replaces, and reports a failed install.
func TestUpgradeCLI(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name":"v0.9.0","html_url":"https://example.test/r"}]`))
	}))
	t.Cleanup(gh.Close)
	h := newHarness(t, "", false)
	h.srv.Updates = &update.Checker{
		URL: gh.URL, Current: "v0.8.0", StatePath: filepath.Join(t.TempDir(), "update.json"),
		Settings: func() (bool, string) { return true, "beta" },
	}
	if err := h.srv.Updates.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "shepherd")
	if err := os.WriteFile(fake, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.srv.Exe = fake
	h.srv.Upgrade.Method = func(string, bool) update.Method { return update.MethodBrew }

	if code := h.run("update", "upgrade"); code == 0 || !strings.Contains(h.err.String(), "brew upgrade") {
		t.Fatalf("brew: %d %s%s", code, h.out, h.err)
	}
	h.out.Reset()
	if code := h.run("update", "status"); code != 0 || !strings.Contains(h.out.String(), "brew upgrade") {
		t.Errorf("status lacks the hint: %s", h.out)
	}

	// An install the daemon replaces, whose release has no archive: it fails, says why and
	// leaves the binary.
	h.srv.Upgrade.Method = func(string, bool) update.Method { return update.MethodBinary }
	h.out.Reset()
	h.err.Reset()
	if code := h.run("update", "upgrade"); code == 0 || !strings.Contains(h.err.String(), "failed") {
		t.Fatalf("upgrade: %d %s%s", code, h.out, h.err)
	}
	if b, _ := os.ReadFile(fake); string(b) != "old" {
		t.Errorf("binary changed: %q", b)
	}
	if code := h.run("update", "status", "--now"); code == 0 {
		t.Error("--now accepted outside upgrade")
	}
}
