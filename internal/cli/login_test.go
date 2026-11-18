package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// hosted is a real daemon (Run) serving TLS on the public listener from a self-signed
// certificate, and a client home that trusts it and holds no login yet.
type hosted struct {
	t       *testing.T
	srv     *daemon.Server
	st      *sqlite.DB
	home    string // the daemon's
	url     string
	cancel  context.CancelFunc
	done    chan error
	setup   string // output of `account set`
	secret  string
	recover []string
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func selfSignedPair(t *testing.T, dir string) (cert, key string, pool *x509.CertPool) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "shepherd.example.com"},
		DNSNames: []string{"shepherd.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	cert, key = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	c, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(c)
	return
}

func startHosted(t *testing.T) *hosted {
	t.Helper()
	auth.UseCheapHashingForTests()
	home := t.TempDir()
	cert, key, pool := selfSignedPair(t, home)
	keyFile := filepath.Join(home, "totp-vault.key")
	os.WriteFile(keyFile, []byte("0123456789abcdef0123456789abcdef"), 0o600)
	addr := freeAddr(t)
	_, port, _ := net.SplitHostPort(addr)
	url := "https://shepherd.example.com:" + port
	cfg := config.Default()
	cfg.Daemon.Listen = "127.0.0.1:0"
	cfg.Daemon.Public = &config.Public{Listen: addr, URL: url, TLS: config.PublicTLS{Cert: cert, Key: key}, TOTPKeyFile: keyFile}
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "shepherd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := daemon.NewServer(st, cfg, "cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.Runner = &dispatch.Runner{Store: st, Config: cfg, Dir: filepath.Join(home, "runs"), Log: srv.Log}
	ctx, cancel := context.WithCancel(context.Background())
	h := &hosted{t: t, srv: srv, st: st, home: home, url: url, cancel: cancel, done: make(chan error, 1)}
	l := paths.Layout{Home: home}
	go func() { h.done <- srv.Run(ctx, l.Runtime()) }()
	t.Cleanup(func() { h.stop() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := daemon.ReadRuntime(l.Runtime()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
	}
	// The client reaches shepherd.example.com at the test listener, trusting only its certificate.
	old := hostedHTTP
	hostedHTTP = &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, n, addr)
			},
		},
	}
	t.Cleanup(func() { hostedHTTP = old })
	return h
}

func (h *hosted) stop() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	<-h.done
	h.cancel = nil
}

// setAccount runs `shepherd account set` on the daemon's own machine.
func (h *hosted) setAccount(password string) {
	h.t.Helper()
	out := &bytes.Buffer{}
	env := Env{Stdin: strings.NewReader(password + "\n"), Stdout: out, Stderr: io.Discard, Layout: paths.Layout{Home: h.home}, Cwd: h.home}
	if err := runAccount(context.Background(), env, []string{"set", "--password-stdin"}); err != nil {
		h.t.Fatal(err)
	}
	h.setup = out.String()
	for _, line := range strings.Split(h.setup, "\n") {
		f := strings.TrimSpace(line)
		if s, ok := strings.CutPrefix(f, "or type the key: "); ok {
			h.secret = s
		}
		if len(f) == 11 && f[5] == '-' {
			h.recover = append(h.recover, f)
		}
	}
	if h.secret == "" || len(h.recover) != 10 {
		h.t.Fatalf("setup output: %s", h.setup)
	}
}

func (h *hosted) waitListening() {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); h.srv.PublicAddr() == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if h.srv.PublicAddr() == "" {
		h.t.Fatal("public listener did not open")
	}
}

// clientEnv is a person's machine: its own home, no daemon of its own.
func clientEnv(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	return Env{Stdin: strings.NewReader(""), Stdout: out, Stderr: errb, Layout: paths.Layout{Home: t.TempDir()}, Cwd: "/"}, out, errb
}

func (h *hosted) code() string {
	c, err := auth.TOTPCodeAt(h.secret, time.Now())
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func TestLoginThenStatusAndDecisionAnswerOverTLS(t *testing.T) {
	h := startHosted(t)
	// No account yet: the listener stays closed, so nothing is reachable.
	time.Sleep(100 * time.Millisecond)
	if h.srv.PublicAddr() != "" {
		t.Fatal("the public listener opened with no account")
	}
	h.setAccount(ownPassword)
	h.waitListening()
	env, out, errb := clientEnv(t)
	ctx := context.Background()

	// Not logged in: the one message.
	if code := Run(ctx, env, []string{"--host", h.url + "/", "status"}); code != 1 || !strings.Contains(errb.String(), "log in again: shepherd login "+h.url) {
		t.Fatalf("status before login: %d %q", code, errb.String())
	}

	// A wrong code is refused and leaves nothing behind.
	env.Stdin = strings.NewReader(ownPassword + "\n000000\n")
	if err := runLogin(ctx, env, []string{"--password-stdin", h.url}); err == nil || !strings.Contains(err.Error(), "wrong password or code") {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tokenDir(env))); err == nil {
		t.Error("a failed login left a token directory")
	}

	env.Stdin = strings.NewReader(ownPassword + "\n" + h.code() + "\n")
	if err := runLogin(ctx, env, []string{"--password-stdin", "--name", "test-laptop", h.url + "/"}); err != nil {
		t.Fatal(err)
	}
	creds, err := client.LoadCredentials(tokenDir(env), h.url)
	if err != nil || creds.Token == "" || creds.Name != "test-laptop" {
		t.Fatalf("saved login: %+v %v", creds, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(client.CredentialsPath(tokenDir(env), h.url))
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("token file mode %v", fi.Mode().Perm())
		}
		di, _ := os.Stat(tokenDir(env))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("token directory mode %v", di.Mode().Perm())
		}
	}

	out.Reset()
	errb.Reset()
	if code := Run(ctx, env, []string{"--host", h.url, "status"}); code != 0 {
		t.Fatalf("status after login: %d %s", code, errb.String())
	}
	// The token is in no output, in no hosts.yaml, and in no file but its own.
	all := out.String() + errb.String()
	if strings.Contains(all, creds.Token) {
		t.Error("the token was printed")
	}
	if _, err := os.Stat(env.Layout.Hosts()); err == nil {
		t.Error("a login wrote hosts.yaml")
	}

	// decision answer works with the device token (the web role is the person's).
	root := t.TempDir()
	if _, err := h.st.SaveWorkspace(ctx, store.Workspace{Name: "git", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app")}}); err != nil {
		t.Fatal(err)
	}
	repos, _ := h.st.Repos(ctx, 1)
	lane, err := h.st.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "l", Branch: "l", Base: "main", Worktree: filepath.Join(root, "l"), State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.st.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: "x", State: store.RunRunning, Log: filepath.Join(root, "log")})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.st.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "which?", Options: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	Run(ctx, env, []string{"--host", h.url, "decision", "answer", fmt.Sprint(d.ID), "a"})
	got, _ := h.st.Decision(ctx, d.ID)
	if got.State != store.DecisionAnswered || strings.Contains(errb.String(), "log in again") || strings.Contains(errb.String(), "forbidden") {
		t.Fatalf("decision answer over https: state %q, stderr %q", got.State, errb.String())
	}

	// chat is refused over https, with the reason.
	errb.Reset()
	if code := Run(ctx, env, []string{"--host", h.url, "chat"}); code != 2 || !strings.Contains(errb.String(), "chat cannot run against") {
		t.Errorf("chat: %d %q", code, errb.String())
	}
}

const ownPassword = "correct horse battery staple"

func TestLoginWithARecoveryCodeAndLogoutRevokes(t *testing.T) {
	h := startHosted(t)
	h.setAccount(ownPassword)
	h.waitListening()
	env, out, errb := clientEnv(t)
	ctx := context.Background()
	env.Stdin = strings.NewReader(ownPassword + "\n" + h.recover[0] + "\n")
	if err := runLogin(ctx, env, []string{"--password-stdin", h.url}); err != nil {
		t.Fatal(err)
	}
	creds, _ := client.LoadCredentials(tokenDir(env), h.url)
	// The same recovery code works once.
	env2, _, _ := clientEnv(t)
	env2.Stdin = strings.NewReader(ownPassword + "\n" + h.recover[0] + "\n")
	if err := runLogin(ctx, env2, []string{"--password-stdin", h.url}); err == nil {
		t.Error("a recovery code worked twice")
	}

	if err := runLogout(ctx, env, []string{h.url}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "token is revoked") {
		t.Errorf("logout: %q %q", out.String(), errb.String())
	}
	if _, err := os.Stat(client.CredentialsPath(tokenDir(env), h.url)); err == nil {
		t.Error("logout left the token file")
	}
	// The token itself no longer works anywhere: revoked on the daemon, not just forgotten.
	c := client.NewHTTPS(h.url, creds.Token, hostedHTTP)
	if _, err := c.Status(ctx); err == nil || err.Error() != "log in again: shepherd login "+h.url {
		t.Errorf("revoked token: %v", err)
	}
	// Logging out again is harmless.
	out.Reset()
	if err := runLogout(ctx, env, []string{h.url}); err != nil || !strings.Contains(out.String(), "Not logged in") {
		t.Errorf("second logout: %v %q", err, out.String())
	}
}

func TestExpiredDeviceTokenGetsTheOneMessage(t *testing.T) {
	h := startHosted(t)
	h.setAccount(ownPassword)
	h.waitListening()
	env, _, errb := clientEnv(t)
	ctx := context.Background()
	env.Stdin = strings.NewReader(ownPassword + "\n" + h.code() + "\n")
	if err := runLogin(ctx, env, []string{"--password-stdin", h.url}); err != nil {
		t.Fatal(err)
	}
	// Expire every device token in the daemon's store.
	ac := h.st.Accounts()
	ds, err := ac.Devices(ctx)
	if err != nil || len(ds) != 1 {
		t.Fatalf("devices: %v %v", ds, err)
	}
	if err := ac.TouchDevice(ctx, ds[0].ID, time.Now().Add(-40*24*time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if code := Run(ctx, env, []string{"--host", h.url, "status"}); code != 1 || strings.TrimSpace(errb.String()) != "shepherd: log in again: shepherd login "+h.url {
		t.Fatalf("expired: %d %q", code, errb.String())
	}
}

func TestPlainHTTPHostIsRefused(t *testing.T) {
	env, _, errb := clientEnv(t)
	if code := Run(context.Background(), env, []string{"--host", "http://shepherd.example.com", "status"}); code != 2 || !strings.Contains(errb.String(), "https") {
		t.Errorf("http host: %d %q", code, errb.String())
	}
}
