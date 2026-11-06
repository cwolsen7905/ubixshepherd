package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/login"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func init() { auth.UseCheapHashingForTests() }

const (
	pubURL = "https://shepherd.example.com"
	ownPW  = "correct horse battery staple"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// hrig is a daemon in hosted mode behind a trusted proxy (127.0.0.1): the public handler
// on one test server, the loopback handler on another, one store, every log line kept.
type hrig struct {
	t     *testing.T
	st    *sqlite.DB
	home  string
	keyf  string
	logs  *syncBuf
	s     *Server
	pub   *httptest.Server
	loop  *httptest.Server
	setup api.AccountSetup
	cfg   config.Config
}

func hostedConfig(keyFile string) config.Config {
	c := config.Default()
	c.Daemon.Public = &config.Public{Listen: "0.0.0.0:8443", URL: pubURL, TrustedProxies: []string{"127.0.0.1"}, TOTPKeyFile: keyFile}
	return c
}

func newHRig(t *testing.T) *hrig {
	t.Helper()
	home := t.TempDir()
	keyf := filepath.Join(home, "vault-totp.key")
	if err := os.WriteFile(keyf, []byte("0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &hrig{t: t, st: st, home: home, keyf: keyf, logs: &syncBuf{}, cfg: hostedConfig(keyf)}
	r.start()
	return r
}

// start brings up a daemon on the store, as a restart does.
func (r *hrig) start() {
	r.t.Helper()
	log := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s, err := NewServer(r.st, r.cfg, "/cfg", log)
	if err != nil {
		r.t.Fatal(err)
	}
	s.DeskAgent = nopDesk{}
	if err := s.PreparePublic(context.Background(), r.home); err != nil {
		r.t.Fatal(err)
	}
	if r.s != nil {
		r.pub.Close()
		r.loop.Close()
	}
	r.s = s
	r.pub = httptest.NewServer(s.PublicHandler())
	r.loop = httptest.NewServer(s.Handler())
	r.t.Cleanup(r.pub.Close)
	r.t.Cleanup(r.loop.Close)
	r.t.Cleanup(s.Close)
}

// req sends a request to the public handler as the trusted proxy would, naming the
// public host and marking it https.
func (r *hrig) req(method, path string, hdr map[string]string, body any) (*http.Response, []byte) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.pub.URL+path, rd)
	req.Host = "shepherd.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (r *hrig) setAccount() {
	r.t.Helper()
	if code := call(r.t, r.loop, r.s.Token, "POST", api.PathAccount, api.AccountSet{Password: ownPW}, &r.setup); code != 200 {
		r.t.Fatalf("account set: %d", code)
	}
	if len(r.setup.RecoveryCodes) != 10 || r.setup.Secret == "" {
		r.t.Fatalf("setup: %+v", r.setup)
	}
}

// codes: the first login uses the TOTP code, later ones a recovery code (a TOTP code
// cannot be used twice in its 30 seconds).
func (r *hrig) totp() string {
	c, err := auth.TOTPCodeAt(r.setup.Secret, time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

type session struct{ cookie, csrf string }

func (r *hrig) browserLogin(code string) session {
	r.t.Helper()
	resp, b := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"password": ownPW, "code": code})
	if resp.StatusCode != 200 {
		r.t.Fatalf("browser login: %d %s", resp.StatusCode, b)
	}
	var out struct{ CSRF string }
	json.Unmarshal(b, &out)
	for _, c := range resp.Cookies() {
		if c.Name == login.CookieName {
			return session{cookie: c.Name + "=" + c.Value, csrf: out.CSRF}
		}
	}
	r.t.Fatal("no session cookie")
	return session{}
}

func (r *hrig) deviceLogin(code, name string) string {
	r.t.Helper()
	resp, b := r.req("POST", api.PathDeviceLogin, nil, map[string]string{"password": ownPW, "code": code, "name": name})
	if resp.StatusCode != 200 {
		r.t.Fatalf("device login: %d %s", resp.StatusCode, b)
	}
	var out struct{ Token string }
	json.Unmarshal(b, &out)
	return out.Token
}

func (s session) h() map[string]string {
	return map[string]string{"Cookie": s.cookie, "Origin": pubURL}
}

func TestHostedPublicListenerNeedsPrepare(t *testing.T) {
	s, _ := newServer(t)
	if err := s.PreparePublic(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("without daemon.public nothing is prepared and nothing fails: %v", err)
	}
	if s.hostedNow() != nil {
		t.Error("hosted mode on without daemon.public")
	}
	if code := call(t, httptest.NewServer(s.Handler()), s.Token, "POST", api.PathAccount, api.AccountSet{Password: ownPW}, nil); code != http.StatusNotFound {
		t.Errorf("account set without hosted mode: %d", code)
	}
}

func TestHostedStartStopsOnABadKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	home := t.TempDir()
	st, _ := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	try := func(file string) error {
		s, _ := NewServer(st, hostedConfig(file), "/cfg", log)
		defer s.Close()
		return s.PreparePublic(context.Background(), home)
	}
	if err := try(filepath.Join(home, "missing.key")); err == nil {
		t.Error("a missing key file did not stop the start")
	}
	open := filepath.Join(home, "open.key")
	os.WriteFile(open, []byte("0123456789abcdef0123456789abcdef"), 0o640)
	os.Chmod(open, 0o640)
	if err := try(open); err == nil || !strings.Contains(err.Error(), "readable by others") {
		t.Errorf("a group-readable key file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "totp.key")); err == nil {
		t.Error("fell back to totp.key after a bad key file")
	}
	good := filepath.Join(home, "good.key")
	os.WriteFile(good, []byte("0123456789abcdef0123456789abcdef"), 0o600)
	if err := try(good); err != nil {
		t.Errorf("a good key file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "totp.key")); err == nil {
		t.Error("totp.key made although a key file is set")
	}
}

func TestHostedFallbackKeyWarnsEveryStartAndFilesOneFeedItem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	home := t.TempDir()
	st, _ := sqlite.Open(context.Background(), filepath.Join(home, "s.db"))
	defer st.Close()
	for i := 0; i < 2; i++ {
		logs := &syncBuf{}
		s, _ := NewServer(st, hostedConfig(""), "/cfg", slog.New(slog.NewTextHandler(logs, nil)))
		if err := s.PreparePublic(context.Background(), home); err != nil {
			t.Fatal(err)
		}
		s.Close()
		if !strings.Contains(logs.String(), "beside the store") {
			t.Errorf("start %d did not warn: %s", i, logs.String())
		}
	}
	fi, err := os.Stat(filepath.Join(home, "totp.key"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("totp.key: %v %v", err, fi)
	}
	items, _ := st.Feed(context.Background(), 0, 100)
	n := 0
	for _, it := range items {
		if strings.Contains(it.Text, "beside the store") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d feed items about the key, want 1", n)
	}
}

func TestHostedBrowserLoginEndToEnd(t *testing.T) {
	r := newHRig(t)
	r.setAccount()

	// No session: the API says so; the login route has the headers.
	if resp, _ := r.req("GET", api.PathStatus, nil, nil); resp.StatusCode != 401 {
		t.Fatalf("no session: %d", resp.StatusCode)
	}
	resp, _ := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"password": ownPW, "code": "000000"})
	if resp.StatusCode != 401 || resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("wrong code: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"password": "not the password!!!", "code": r.totp()}); resp.StatusCode != 401 {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}

	code := r.totp()
	sess := r.browserLogin(code)
	// The same code again is refused.
	if resp, _ := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"password": ownPW, "code": code}); resp.StatusCode != 401 {
		t.Fatalf("a reused code: %d", resp.StatusCode)
	}
	// A recovery code works once.
	rc := r.setup.RecoveryCodes[0]
	r.browserLogin(rc)
	if resp, _ := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"password": ownPW, "code": rc}); resp.StatusCode != 401 {
		t.Fatalf("a reused recovery code: %d", resp.StatusCode)
	}

	// The session reads; a write needs its CSRF token and the public Origin.
	if resp, b := r.req("GET", api.PathWebSession, sess.h(), nil); resp.StatusCode != 200 || !strings.Contains(string(b), `"role":"web"`) {
		t.Fatalf("session read: %d %s", resp.StatusCode, b)
	}
	h := sess.h()
	if resp, _ := r.req("POST", api.PathWebSignout, h, nil); resp.StatusCode != 403 {
		t.Errorf("a write without CSRF: %d", resp.StatusCode)
	}
	h["Origin"] = "https://evil.example"
	h[api.CSRFHeader] = sess.csrf
	if resp, _ := r.req("POST", api.PathWebSignout, h, nil); resp.StatusCode != 403 {
		t.Errorf("a write from another origin: %d", resp.StatusCode)
	}
	// A sign-in link has no place on the public listener.
	link := struct{ Path string }{}
	call(t, r.loop, r.s.Token, "POST", api.PathWebSignin, nil, &link)
	if resp, _ := r.req("GET", link.Path, nil, nil); resp.StatusCode != 404 {
		t.Errorf("a sign-in link on the public listener: %d", resp.StatusCode)
	}
}

func TestHostedOperatorTokenIs404PublicAndWorksOnLoopback(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	op := map[string]string{"Authorization": "Bearer " + r.s.Token}
	resp, b := r.req("GET", api.PathStatus, op, nil)
	miss, mb := r.req("GET", "/v1/no-such-route", nil, nil)
	if resp.StatusCode != 404 || miss.StatusCode != 404 && miss.StatusCode != 401 {
		t.Fatalf("operator token on public: %d %s; missing route %d %s", resp.StatusCode, b, miss.StatusCode, mb)
	}
	if resp.StatusCode != 404 || strings.Contains(string(b), "token") {
		t.Errorf("the public side says something about the token: %s", b)
	}
	if code := call(t, r.loop, r.s.Token, "GET", api.PathStatus, nil, nil); code != 200 {
		t.Errorf("operator token on loopback: %d", code)
	}
	// Worker and desk tokens do not work on the public listener either.
	worker, _ := r.s.MintWorker(1)
	desk, _ := r.s.MintDesk(true, "s")
	for _, tok := range []string{worker, desk} {
		if resp, _ := r.req("GET", api.PathStatus, map[string]string{"Authorization": "Bearer " + tok}, nil); resp.StatusCode != 401 {
			t.Errorf("a scoped token on the public listener: %d", resp.StatusCode)
		}
	}
	// Nor can the account be set from the public side, even with the operator token.
	if resp, _ := r.req("POST", api.PathAccount, op, api.AccountSet{Password: "another long password"}); resp.StatusCode != 404 {
		t.Errorf("account set on public: %d", resp.StatusCode)
	}
}

func TestHostedDeviceTokenLifecycle(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	tok := r.deviceLogin(r.totp(), "laptop")
	bearer := map[string]string{"Authorization": "Bearer " + tok}
	if resp, b := r.req("GET", api.PathStatus, bearer, nil); resp.StatusCode != 200 {
		t.Fatalf("a device token: %d %s", resp.StatusCode, b)
	}
	// The web role, never the operator's: no account or sign-in minting.
	if resp, _ := r.req("POST", api.PathWebSignin, bearer, nil); resp.StatusCode != 403 {
		t.Errorf("a device token minted a sign-in link: %d", resp.StatusCode)
	}
	var list api.DeviceList
	resp, b := r.req("GET", api.PathDevices, bearer, nil)
	json.Unmarshal(b, &list)
	if resp.StatusCode != 200 || len(list.Devices) != 1 || list.Devices[0].Name != "laptop" || strings.Contains(string(b), tok) || strings.Contains(string(b), auth.HashToken(tok)) {
		t.Fatalf("device list: %d %s", resp.StatusCode, b)
	}
	if want := list.Devices[0].Created.Add(auth.DeviceCap); !list.Devices[0].MaxExpires.Equal(want) {
		t.Errorf("max expires %v want %v", list.Devices[0].MaxExpires, want)
	}
	// A device token is no operator token on loopback.
	if code := call(t, r.loop, tok, "GET", api.PathStatus, nil, nil); code != 401 {
		t.Errorf("a device token on the loopback listener: %d", code)
	}
	// Revoked: exactly the one message, with the URL.
	if resp, _ := r.req("POST", "/v1/devices/"+list.Devices[0].ID+"/revoke", bearer, struct{}{}); resp.StatusCode != 200 {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	resp, b = r.req("GET", api.PathStatus, bearer, nil)
	if resp.StatusCode != 401 || !strings.Contains(string(b), "log in again: shepherd login "+pubURL) {
		t.Fatalf("a revoked token: %d %s", resp.StatusCode, b)
	}
	resp2, b2 := r.req("GET", api.PathStatus, map[string]string{"Authorization": "Bearer shd_never-issued"}, nil)
	if resp2.StatusCode != 401 || string(b2) != string(b) {
		t.Errorf("an unknown token answers differently from a revoked one: %s vs %s", b2, b)
	}
}

func TestHostedSignOutEverywhereAndRestart(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	sess := r.browserLogin(r.totp())
	tok := r.deviceLogin(r.setup.RecoveryCodes[1], "laptop")
	bearer := map[string]string{"Authorization": "Bearer " + tok}
	alive := func(label string, want int) {
		t.Helper()
		if resp, _ := r.req("GET", api.PathWebSession, sess.h(), nil); resp.StatusCode != want {
			t.Errorf("%s: session %d, want %d", label, resp.StatusCode, want)
		}
		if resp, _ := r.req("GET", api.PathStatus, bearer, nil); resp.StatusCode != want {
			t.Errorf("%s: device token %d, want %d", label, resp.StatusCode, want)
		}
	}
	alive("after login", 200)
	r.start() // a restart: both are rows in the store
	alive("after a restart", 200)

	h := sess.h()
	h[api.CSRFHeader] = sess.csrf
	resp, b := r.req("POST", api.PathWebSignoutAll, h, struct{}{})
	var out api.SignedOutEverywhere
	json.Unmarshal(b, &out)
	if resp.StatusCode != 200 || out.Sessions < 1 || out.Devices != 1 {
		t.Fatalf("sign out everywhere: %d %s", resp.StatusCode, b)
	}
	alive("after sign out everywhere", 401)
	r.start()
	alive("after sign out everywhere and a restart", 401)

	// One logout ends one session across a restart too.
	a := r.browserLogin(r.setup.RecoveryCodes[2])
	b2 := r.browserLogin(r.setup.RecoveryCodes[3])
	ha := a.h()
	ha[api.CSRFHeader] = a.csrf
	if resp, _ := r.req("POST", api.PathWebSignout, ha, struct{}{}); resp.StatusCode != 200 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	r.start()
	if resp, _ := r.req("GET", api.PathWebSession, a.h(), nil); resp.StatusCode != 401 {
		t.Errorf("a logged-out session after a restart: %d", resp.StatusCode)
	}
	if resp, _ := r.req("GET", api.PathWebSession, b2.h(), nil); resp.StatusCode != 200 {
		t.Errorf("the other session after a restart: %d", resp.StatusCode)
	}
}

func TestHostedPasswordChangeSignsEverythingOut(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	sess := r.browserLogin(r.totp())
	tok := r.deviceLogin(r.setup.RecoveryCodes[0], "laptop")
	if code := call(t, r.loop, r.s.Token, "POST", api.PathAccount, api.AccountSet{Password: "a brand new password!"}, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if resp, _ := r.req("GET", api.PathWebSession, sess.h(), nil); resp.StatusCode != 401 {
		t.Errorf("session after a password change: %d", resp.StatusCode)
	}
	if resp, _ := r.req("GET", api.PathStatus, map[string]string{"Authorization": "Bearer " + tok}, nil); resp.StatusCode != 401 {
		t.Errorf("device token after a password change: %d", resp.StatusCode)
	}
}

func TestHostedBurstIsRefusedAndFiled(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	bad := map[string]string{"password": ownPW, "code": "000000"}
	for i := 0; i < 5; i++ {
		r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL, "X-Forwarded-For": "198.51.100." + string(rune('1'+i))}, bad)
	}
	resp, _ := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL, "X-Forwarded-For": "192.0.2.50"}, map[string]string{"password": ownPW, "code": r.totp()})
	if resp.StatusCode != 429 {
		t.Fatalf("after a burst: %d", resp.StatusCode)
	}
	items, _ := r.st.Feed(context.Background(), 0, 100)
	n := 0
	for _, it := range items {
		if strings.Contains(it.Text, "sign-in locked") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d feed items for the lock, want 1", n)
	}
}

func TestHostedTOTPSecretNotInTheStoreInTheClear(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	r.st.Close() // the file alone, as a copied backup
	raw, err := os.ReadFile(filepath.Join(r.home, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	wal, _ := os.ReadFile(filepath.Join(r.home, "s.db-wal"))
	for _, blob := range [][]byte{raw, wal} {
		for _, secret := range []string{r.setup.Secret, ownPW} {
			if bytes.Contains(blob, []byte(secret)) {
				t.Errorf("%q is readable in the store", secret)
			}
		}
	}
	st, _ := sqlite.Open(context.Background(), filepath.Join(r.home, "s.db"))
	defer st.Close()
	fs, _ := st.Accounts().Factors(context.Background())
	other, _ := auth.LoadKey("", t.TempDir())
	if len(fs) != 1 {
		t.Fatal("no factor")
	}
	if _, err := other.Open(fs[0].Secret, fs[0].ID); err == nil {
		t.Error("the secret opened without its key")
	}
}

func TestHostedServesTLSItself(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	dir := t.TempDir()
	certFile, keyFile, pool := selfSigned(t, dir)
	ln := freePort(t)
	cfg := hostedConfig(r.keyf)
	cfg.Daemon.Public = &config.Public{Listen: ln, URL: "https://shepherd.example.com:" + portOf(ln), TLS: config.PublicTLS{Cert: certFile, Key: keyFile}, TOTPKeyFile: r.keyf}
	r.cfg = cfg
	r.start()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.s.ServePublic(ctx) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); r.s.PublicAddr() == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if r.s.PublicAddr() == "" {
		t.Fatal("the public listener did not open although an account exists")
	}
	cl := tlsClient(pool, "shepherd.example.com", r.s.PublicAddr())
	resp, err := cl.Get("https://shepherd.example.com:" + portOf(ln) + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("over TLS, signed out: %d", resp.StatusCode)
	}
	// Plain HTTP to the TLS port gets no answer a client could use.
	if resp, err := http.Get("http://" + r.s.PublicAddr() + "/v1/status"); err == nil && resp.StatusCode == 200 {
		t.Errorf("plain HTTP answered 200")
	}
}

func TestHostedListenerStaysClosedWithoutAnAccount(t *testing.T) {
	r := newHRig(t)
	dir := t.TempDir()
	certFile, keyFile, _ := selfSigned(t, dir)
	ln := freePort(t)
	r.cfg.Daemon.Public = &config.Public{Listen: ln, URL: "https://shepherd.example.com", TLS: config.PublicTLS{Cert: certFile, Key: keyFile}, TOTPKeyFile: r.keyf}
	r.start()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.s.ServePublic(ctx) }()
	defer func() { cancel(); <-done }()
	time.Sleep(150 * time.Millisecond)
	if r.s.PublicAddr() != "" {
		t.Fatal("the public listener opened with no account")
	}
	r.setAccount()
	for deadline := time.Now().Add(5 * time.Second); r.s.PublicAddr() == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if r.s.PublicAddr() == "" {
		t.Error("the listener did not open once the account was set")
	}
}

// A fake agent prints secrets; the test reads everything a signed-in person can reach on
// the public listener, the feed and the daemon's log, and none of them may hold a secret.
func TestHostedNoSecretOnPublicRoutesFeedOrLogs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@e", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@e"} {
		t.Setenv(k, v)
	}
	root, _ := paths.Canonical(t.TempDir())
	g := func(dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	g(root, "init", "-q", "--bare", origin)
	app := filepath.Join(root, "ws", "app")
	g(root, "clone", "-q", origin, app)
	os.WriteFile(filepath.Join(app, "README.md"), []byte("hi\n"), 0o644)
	g(app, "add", ".")
	g(app, "commit", "-q", "-m", "init")
	g(app, "push", "-q", "origin", "HEAD:main")

	r := newHRig(t)
	r.setAccount()
	canary := "sk-ant-api03-CANARYCANARYCANARYCANARY0123456789"
	pwCanary := "password=CanaryHunter2Hunter2"
	agent := filepath.Join(root, "agent")
	os.WriteFile(agent, []byte("#!/bin/sh\ncat >/dev/null\necho 'key "+canary+"'\necho '"+pwCanary+"'\necho \"Authorization: Bearer $SHEPHERD_TOKEN\"\nenv | grep -i token\necho done\n"), 0o755)
	r.s.Runner = &dispatch.Runner{Store: r.st, Config: r.cfg, Dir: filepath.Join(root, "runs"), Log: r.s.Log}
	dispatch.SetLookPath(r.s.Runner, func(string) (string, error) { return agent, nil })
	ctx := context.Background()
	w, _ := r.st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: filepath.Join(root, "ws")}, []store.Repo{{Name: "app", Path: app}})
	repos, _ := r.st.Repos(ctx, w.ID)
	opened, err := r.s.Fold.Open(ctx, fold.OpenRequest{RepoID: repos[0].ID, Name: "work", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatal(err)
	}
	var run api.RunView
	if code := call(t, r.loop, r.s.Token, "POST", api.PathRuns, dispatch.StartRequest{LaneID: opened.Lane.ID, Agent: "claude", Prompt: "go"}, &run); code != http.StatusAccepted && code != 200 {
		t.Fatalf("start: %d", code)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if runs, _ := r.st.Runs(ctx, 0, store.RunSucceeded, 10); len(runs) == 1 {
			run.ID = runs[0].ID
			break
		}
	}
	if run.ID == 0 {
		t.Fatal("the fake agent's run did not finish")
	}

	// Every secret the daemon holds, and what the agent printed.
	sess := r.browserLogin(r.totp())
	tok := r.deviceLogin(r.setup.RecoveryCodes[0], "laptop")
	worker, _ := r.s.MintWorker(run.ID)
	desk, _ := r.s.MintDesk(true, "sess")
	acct, _ := r.st.Accounts().Account(ctx)
	rows, _ := r.st.Accounts().RecoveryCodes(ctx)
	fs, _ := r.st.Accounts().Factors(ctx)
	keyBytes, _ := os.ReadFile(r.keyf)
	secrets := []string{r.s.Token, worker, desk, ownPW, r.setup.Secret, acct.PasswordHash, tok, auth.HashToken(tok), sess.cookie[strings.Index(sess.cookie, "=")+1:],
		strings.TrimSpace(string(keyBytes)), canary, "CanaryHunter2Hunter2", string(fs[0].Secret)}
	for _, c := range r.setup.RecoveryCodes {
		secrets = append(secrets, c, strings.ReplaceAll(c, "-", ""))
	}
	for _, row := range rows {
		secrets = append(secrets, row.Hash)
	}

	var seen strings.Builder
	paths := []string{api.PathStatus, api.PathRuns, api.PathRuns + "/" + itoa(run.ID), api.PathRuns + "/" + itoa(run.ID) + "/log", api.PathFeed, api.PathConfig,
		api.PathLanes, api.PathDevices, api.PathWaiting, api.PathWebSession, "/v1/decisions", "/v1/requests", "/", "/v1/no-such-route"}
	for _, p := range paths {
		for _, h := range []map[string]string{sess.h(), {"Authorization": "Bearer " + tok}, nil, {"Authorization": "Bearer " + r.s.Token}, {"Authorization": "Bearer " + worker}} {
			resp, b := r.req("GET", p, h, nil)
			seen.WriteString(resp.Status + " " + p + "\n")
			seen.Write(b)
			for k, v := range resp.Header {
				seen.WriteString(k + ": " + strings.Join(v, ",") + "\n")
			}
		}
	}
	items, _ := r.st.Feed(ctx, 0, 1000)
	for _, it := range items {
		seen.WriteString(it.Text + "\n")
	}
	seen.WriteString(r.logs.String())
	if len(items) == 0 {
		t.Error("no feed items to check")
	}
	for _, sct := range secrets {
		if sct != "" && strings.Contains(seen.String(), sct) {
			t.Errorf("a secret reached a public route, the feed or the log: %.12q...", sct)
		}
	}
	// The canary reached the run log through the agent: it must be masked there.
	if _, b := r.req("GET", api.PathRuns+"/"+itoa(run.ID)+"/log", sess.h(), nil); !strings.Contains(string(b), "REDACTED") {
		t.Errorf("the run log shows no redaction: %s", b)
	}
}
