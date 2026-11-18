package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// runRig is a daemon started through Run, as the CLI starts it.
type runRig struct {
	t      *testing.T
	s      *Server
	home   string
	cancel context.CancelFunc
	done   chan error
	logs   *syncBuf
}

func startRun(t *testing.T, home string, cfg config.Config, cfgPath string) *runRig {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs := &syncBuf{}
	s, err := NewServer(st, cfg, cfgPath, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.DeskAgent = nopDesk{}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runRig{t: t, s: s, home: home, cancel: cancel, done: make(chan error, 1), logs: logs}
	go func() { r.done <- s.Run(ctx, filepath.Join(home, "daemon.json")) }()
	t.Cleanup(func() { r.stop() })
	return r
}

func (r *runRig) stop() error {
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	err := <-r.done
	r.cancel = nil
	return err
}

func (r *runRig) up() {
	r.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := ReadRuntime(filepath.Join(r.home, "daemon.json")); err == nil {
			return
		}
	}
	r.t.Fatal("daemon did not start")
}

func (r *runRig) waitPublic(want bool) {
	r.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if (r.s.PublicAddr() != "") == want {
			return
		}
	}
	r.t.Fatalf("public listener open = %v, want %v", r.s.PublicAddr() != "", want)
}

func writeKey(t *testing.T, home string) string {
	t.Helper()
	p := filepath.Join(home, "vault-totp.key")
	if err := os.WriteFile(p, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func tlsPublic(t *testing.T, home string) (*config.Public, func(addr string) bool) {
	certFile, keyFile, pool := selfSigned(t, home)
	ln := freePort(t)
	pub := &config.Public{Listen: ln, URL: "https://shepherd.example.com:" + portOf(ln), TLS: config.PublicTLS{Cert: certFile, Key: keyFile}, TOTPKeyFile: writeKey(t, home)}
	// answers401 says an unauthenticated request over TLS gets 401.
	return pub, func(addr string) bool {
		cl := tlsClient(pool, "shepherd.example.com", addr)
		resp, err := cl.Get(pub.URL + "/v1/status")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 401
	}
}

func TestRunOpensNoPublicListenerWithoutConfig(t *testing.T) {
	r := startRun(t, t.TempDir(), config.Default(), "/cfg")
	r.up()
	time.Sleep(150 * time.Millisecond)
	if r.s.PublicAddr() != "" || r.s.hostedNow() != nil {
		t.Fatal("hosted mode on without daemon.public")
	}
}

func TestRunOpensPublicListenerOnlyWithAnAccountAndStopsItOnStop(t *testing.T) {
	home := t.TempDir()
	pub, answers401 := tlsPublic(t, home)
	cfg := config.Default()
	cfg.Daemon.Public = pub
	r := startRun(t, home, cfg, "/cfg")
	r.up()
	time.Sleep(100 * time.Millisecond)
	if r.s.PublicAddr() != "" {
		t.Fatal("public listener open before an account exists")
	}
	if _, err := net.DialTimeout("tcp", pub.Listen, 200*time.Millisecond); err == nil {
		t.Fatal("something listens on the public port with no account")
	}
	// Loopback works as ever, with the operator token.
	rt, _ := ReadRuntime(filepath.Join(home, "daemon.json"))
	if code := callAddr(t, rt.Addr, rt.Token, "GET", api.PathStatus); code != 200 {
		t.Fatalf("loopback status: %d", code)
	}
	if code := callAddr(t, rt.Addr, rt.Token, "POST", api.PathAccount); code == 404 {
		t.Fatal("account route missing")
	}
	r.setAccountOnLoopback(rt)
	r.waitPublic(true)
	if !answers401(r.s.PublicAddr()) {
		t.Fatal("an unauthenticated request over TLS was not refused with 401")
	}
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.DialTimeout("tcp", pub.Listen, 200*time.Millisecond); err == nil {
		t.Fatal("the public listener survived the daemon's stop")
	}
}

func TestRunRefusesToStartOnAnUnsafePublicConfig(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default()
	cfg.Daemon.Public = &config.Public{Listen: freePort(t), URL: "https://shepherd.example.com"} // no TLS, no proxy
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, _ := NewServer(st, cfg, "/cfg", slog.New(slog.NewTextHandler(&syncBuf{}, nil)))
	s.DeskAgent = nopDesk{}
	err = s.Run(context.Background(), filepath.Join(home, "daemon.json"))
	if err == nil || !strings.Contains(err.Error(), "daemon.public") {
		t.Fatalf("Run: %v", err)
	}
	if _, rerr := ReadRuntime(filepath.Join(home, "daemon.json")); rerr == nil {
		t.Error("a daemon that refused to start left a runtime file")
	}
}

func TestReloadRestartsThePublicListenerOnlyWhenItChanged(t *testing.T) {
	home := t.TempDir()
	pub, answers401 := tlsPublic(t, home)
	cfgPath := filepath.Join(home, "config.yaml")
	cfg := config.Default()
	cfg.Daemon.Public = pub
	writeConfig(t, cfgPath, cfg)
	r := startRun(t, home, cfg, cfgPath)
	r.up()
	rt, _ := ReadRuntime(filepath.Join(home, "daemon.json"))
	r.setAccountOnLoopback(rt)
	r.waitPublic(true)
	first := r.s.PublicAddr()

	// A reload that leaves daemon.public alone leaves the listener alone.
	if err := r.s.ReloadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * publicPollEvery)
	if r.s.PublicAddr() != first || !answers401(first) {
		t.Fatal("an unrelated reload disturbed the public listener")
	}

	// Moving it to another port closes the old one and opens the new.
	moved := *pub
	moved.Listen = freePort(t)
	moved.URL = "https://shepherd.example.com:" + portOf(moved.Listen)
	cfg.Daemon.Public = &moved
	writeConfig(t, cfgPath, cfg)
	if err := r.s.ReloadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.s.PublicAddr() == first && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.s.PublicAddr() == first || r.s.PublicAddr() == "" {
		t.Fatalf("listener did not move: %q", r.s.PublicAddr())
	}
	if _, err := net.DialTimeout("tcp", first, 200*time.Millisecond); err == nil {
		t.Error("the old public port still listens")
	}

	// Removing daemon.public closes it for good.
	cfg.Daemon.Public = nil
	writeConfig(t, cfgPath, cfg)
	if err := r.s.ReloadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); r.s.PublicAddr() != "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if _, err := net.DialTimeout("tcp", moved.Listen, 200*time.Millisecond); err == nil {
		t.Error("the public listener survived the removal of daemon.public")
	}
}

func callAddr(t *testing.T, addr, token, method, path string) int {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+addr+path, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (r *runRig) setAccountOnLoopback(rt api.Runtime) {
	r.t.Helper()
	b, _ := json.Marshal(api.AccountSet{Password: ownPW})
	req, _ := http.NewRequest("POST", "http://"+rt.Addr+api.PathAccount, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+rt.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		r.t.Fatalf("account set: %d", resp.StatusCode)
	}
}

// writeConfig writes cfg's daemon.public (the only part these tests change) as config.yaml.
func writeConfig(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	y := "daemon:\n  listen: 127.0.0.1:0\n"
	if p := cfg.Daemon.Public; p != nil {
		y += fmt.Sprintf("  public:\n    listen: %s\n    url: %s\n    tls: {cert: %s, key: %s}\n    totp_key_file: %s\n", p.Listen, p.URL, p.TLS.Cert, p.TLS.Key, p.TOTPKeyFile)
	}
	if err := os.WriteFile(path, []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReloadToAnUnpreparableConfigLeavesNoListenerAndNoAddress(t *testing.T) {
	home := t.TempDir()
	pub, _ := tlsPublic(t, home)
	cfgPath := filepath.Join(home, "config.yaml")
	cfg := config.Default()
	cfg.Daemon.Public = pub
	writeConfig(t, cfgPath, cfg)
	r := startRun(t, home, cfg, cfgPath)
	r.up()
	rt, _ := ReadRuntime(filepath.Join(home, "daemon.json"))
	r.setAccountOnLoopback(rt)
	r.waitPublic(true)
	// Valid settings, but the key file is gone: Validate passes, PreparePublic cannot.
	bad := *pub
	bad.TOTPKeyFile = filepath.Join(home, "missing.key")
	cfg.Daemon.Public = &bad
	writeConfig(t, cfgPath, cfg)
	if err := r.s.ReloadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.waitPublic(false)
	if _, err := net.DialTimeout("tcp", pub.Listen, 200*time.Millisecond); err == nil {
		t.Error("the public port still listens")
	}
}
