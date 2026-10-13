package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/update"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// upgradeFixture is a server whose checker offers v0.5.0 over a current v0.4.0, with a
// fake binary on disk and a fake release server holding the archive.
type upgradeFixture struct {
	s      *Server
	ts     *httptest.Server
	exe    string
	cfgDir string
	active atomic.Int32
}

func newUpgradeFixture(t *testing.T, corrupt bool) *upgradeFixture {
	t.Helper()
	f := &upgradeFixture{}
	f.s, f.ts = newServer(t)
	f.cfgDir = t.TempDir()
	f.s.ConfigPath = filepath.Join(f.cfgDir, "config.yaml")
	f.exe = filepath.Join(t.TempDir(), "shepherd")
	os.WriteFile(f.exe, []byte("old"), 0o755)
	f.s.Exe = f.exe

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	name := update.ArchiveName("v0.5.0", "linux", "amd64")
	tw.WriteHeader(&tar.Header{Name: "pkg/shepherd", Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
	tw.Write([]byte("new"))
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	hexsum := hex.EncodeToString(sum[:])
	if corrupt {
		hexsum = strings.Repeat("0", 64)
	}
	rel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS"):
			fmt.Fprintf(w, "%s  %s\n", hexsum, name)
		default:
			w.Write(buf.Bytes())
		}
	}))
	t.Cleanup(rel.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"tag_name":"v0.5.0","html_url":"https://example.test/r","assets":[
			{"name":%q,"browser_download_url":%q},{"name":"SHA256SUMS","browser_download_url":%q}]}]`,
			name, rel.URL+"/"+name, rel.URL+"/SHA256SUMS")
	}))
	t.Cleanup(api.Close)
	f.s.Updates = updateChecker(t, api, "v0.4.0")
	f.s.Upgrade = UpgradeHooks{
		Method: func(string, bool) update.Method { return update.MethodBinary },
		Installer: func(exe string) *update.Installer {
			return &update.Installer{Exe: exe, Trusted: rel.URL, GOOS: "linux", GOARCH: "amd64"}
		},
		Active:   func() int { return int(f.active.Load()) },
		IdlePoll: 10 * time.Millisecond,
	}
	return f
}

func (f *upgradeFixture) status(t *testing.T) api.UpdateStatus {
	t.Helper()
	var st api.UpdateStatus
	if code := call(t, f.ts, f.s.Token, "GET", api.PathUpdate, nil, &st); code != 200 {
		t.Fatalf("GET: %d", code)
	}
	return st
}

func waitState(t *testing.T, f *upgradeFixture, want string) api.UpdateStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := f.status(t)
		if st.Upgrade.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %q, want %q: %+v", st.Upgrade.State, want, st.Upgrade)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpgradeInstallsAndRestartsWhenIdle(t *testing.T) {
	f := newUpgradeFixture(t, false)
	if st := f.status(t); !st.Upgrade.CanUpgrade || st.Upgrade.State != api.UpgradeIdle || st.Upgrade.Method != "binary" {
		t.Fatalf("before: %+v", st.Upgrade)
	}
	var st api.UpdateStatus
	if code := call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, &st); code != http.StatusAccepted {
		t.Fatalf("upgrade: %d", code)
	}
	waitState(t, f, api.UpgradeRestarting)
	if !f.s.RestartRequested() {
		t.Error("no restart requested")
	}
	select {
	case <-f.s.stop:
	default:
		t.Error("Run was not told to stop")
	}
	if b, _ := os.ReadFile(f.exe); string(b) != "new" {
		t.Errorf("binary %q", b)
	}
	if b, _ := os.ReadFile(update.PreviousPath(f.exe)); string(b) != "old" {
		t.Errorf("previous %q", b)
	}
	m, ok := update.ReadMarker(UpgradeMarkerPath(f.s.ConfigPath))
	if !ok || m.To != "v0.5.0" || m.Launches != 0 {
		t.Errorf("marker %+v %v", m, ok)
	}
}

func TestUpgradeWaitsForAgentsUnlessNow(t *testing.T) {
	f := newUpgradeFixture(t, false)
	f.active.Store(2)
	call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, nil)
	st := waitState(t, f, api.UpgradeInstalled)
	if st.Upgrade.ActiveRuns != 2 || st.Upgrade.Target != "v0.5.0" {
		t.Errorf("%+v", st.Upgrade)
	}
	time.Sleep(60 * time.Millisecond)
	if f.s.RestartRequested() {
		t.Fatal("restarted with agents running")
	}
	// A second plain request changes nothing; now restarts at once.
	call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, nil)
	if f.s.RestartRequested() {
		t.Fatal("plain request restarted")
	}
	if code := call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0", Now: true}, nil); code != http.StatusAccepted {
		t.Fatalf("now: %d", code)
	}
	if !f.s.RestartRequested() {
		t.Error("now did not restart")
	}
}

func TestUpgradeRestartsOnceAgentsFinish(t *testing.T) {
	f := newUpgradeFixture(t, false)
	f.active.Store(1)
	call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, nil)
	waitState(t, f, api.UpgradeInstalled)
	f.active.Store(0)
	waitState(t, f, api.UpgradeRestarting)
}

func TestUpgradeFailureKeepsTheBinaryAndIsNotRetried(t *testing.T) {
	f := newUpgradeFixture(t, true)
	call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, nil)
	st := waitState(t, f, api.UpgradeFailed)
	if !strings.Contains(st.Upgrade.Error, "SHA256SUMS") || st.Upgrade.FailedVersion != "v0.5.0" {
		t.Errorf("%+v", st.Upgrade)
	}
	if b, _ := os.ReadFile(f.exe); string(b) != "old" {
		t.Errorf("binary %q", b)
	}
	if f.s.RestartRequested() {
		t.Error("restarted after a failure")
	}
	if _, ok := update.ReadMarker(UpgradeMarkerPath(f.s.ConfigPath)); ok {
		t.Error("marker left after a failure")
	}
	// Automatic upgrade skips a release that failed.
	on := true
	cfg := f.s.LiveConfig()
	cfg.Update.Auto = &on
	f.s.live.Store(&cfg)
	f.s.upg.mu.Lock()
	f.s.upg.state = ""
	f.s.upg.mu.Unlock()
	f.s.autoUpgradeOnce()
	time.Sleep(50 * time.Millisecond)
	if got := f.status(t).Upgrade.State; got != api.UpgradeFailed {
		t.Errorf("auto retried: %s", got)
	}
}

func TestUpgradeRefusals(t *testing.T) {
	f := newUpgradeFixture(t, false)
	if code := call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.4.9"}, nil); code != http.StatusConflict {
		t.Errorf("stale version: %d", code)
	}
	f.s.Upgrade.Method = func(string, bool) update.Method { return update.MethodBrew }
	st := f.status(t)
	if st.Upgrade.CanUpgrade || !strings.Contains(st.Upgrade.Hint, "brew upgrade") {
		t.Errorf("%+v", st.Upgrade)
	}
	var e api.Error
	if code := call(t, f.ts, f.s.Token, "POST", api.PathUpdateUpgrade, api.UpdateUpgrade{Version: "v0.5.0"}, &e); code != http.StatusConflict || !strings.Contains(e.Error, "brew upgrade") {
		t.Errorf("brew: %d %q", code, e.Error)
	}
	if b, _ := os.ReadFile(f.exe); string(b) != "old" {
		t.Error("replaced a brew install")
	}
}

func TestAutoUpgradeIsOptIn(t *testing.T) {
	f := newUpgradeFixture(t, false)
	f.s.autoUpgradeOnce()
	time.Sleep(50 * time.Millisecond)
	if st := f.status(t); st.Upgrade.State != api.UpgradeIdle || st.Upgrade.Auto {
		t.Fatalf("auto off by default: %+v", st.Upgrade)
	}
	on := true
	cfg := f.s.LiveConfig()
	cfg.Update.Auto = &on
	f.s.live.Store(&cfg)
	if err := f.s.updates().Dismiss("v0.5.0"); err != nil {
		t.Fatal(err)
	}
	f.s.autoUpgradeOnce()
	time.Sleep(50 * time.Millisecond)
	if f.s.RestartRequested() {
		t.Fatal("auto upgraded a dismissed release")
	}
	// Not dismissed: a fresh checker state.
	f.s.Updates.StatePath = filepath.Join(t.TempDir(), "other.json")
	f.s.Updates = updateCheckerFrom(t, f)
	f.s.autoUpgradeOnce()
	waitState(t, f, api.UpgradeRestarting)
}

func TestUpgradeVerifyClearsTheMarker(t *testing.T) {
	f := newUpgradeFixture(t, false)
	p := UpgradeMarkerPath(f.s.ConfigPath)
	// Only a marker for the version running counts; the test binary reports "dev".
	update.WriteMarker(p, update.Marker{From: "v0.4.0", To: versionNow()})
	f.s.Upgrade.VerifyAfter = 20 * time.Millisecond
	ctx, cancel := contextForTest(t)
	defer cancel()
	f.s.startUpgradeVerify(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := update.ReadMarker(p); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("marker never cleared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func versionNow() string { return version.Version }

func contextForTest(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// updateCheckerFrom is a fresh checker against the same fake releases, with nothing
// dismissed.
func updateCheckerFrom(t *testing.T, f *upgradeFixture) *update.Checker {
	t.Helper()
	c := &update.Checker{
		URL: f.s.Updates.URL, Current: "v0.4.0", StatePath: filepath.Join(t.TempDir(), "update.json"),
		Settings: func() (bool, string) { return true, "beta" },
	}
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}
