package login

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

const (
	pw  = "correct horse battery"
	pub = "https://shepherd.example.com"
	op  = "operator-token-value"
)

type starter struct{ n int }

func (s *starter) StartSession(context.Context) (string, time.Duration, string, error) {
	s.n++
	return "cookie-value", 12 * time.Hour, "csrf-value", nil
}

type rig struct {
	h      *Handler
	secret string
	rec    []string
	ses    *starter
}

func newRig(t *testing.T, p *config.Public) *rig {
	t.Helper()
	key, err := auth.LoadKey("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := auth.NewMemStore()
	svc := &auth.Service{Store: st, Factor: &auth.TOTP{Store: st, Key: key}, Limiter: auth.NewLimiter(auth.Limits{})}
	setup, err := svc.SetPassword(context.Background(), pw)
	if err != nil {
		t.Fatal(err)
	}
	ses := &starter{}
	return &rig{h: &Handler{Auth: svc, Public: p, Sessions: ses}, secret: setup.Enrolment.Secret, rec: setup.RecoveryCodes, ses: ses}
}

func (r *rig) code(t *testing.T) string {
	c, err := auth.TOTPCodeAt(r.secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func proxyCfg() *config.Public {
	return &config.Public{Listen: "0.0.0.0:8443", URL: pub, TrustedProxies: []string{"10.0.0.0/24"}}
}

func (r *rig) server(p *config.Public) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/web/login", r.h.Browser)
	mux.HandleFunc("/v1/device/login", r.h.Device)
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "status") })
	return Guard(p, func(req *http.Request) bool { return req.Header.Get("Authorization") == "Bearer "+op }, mux)
}

func do(h http.Handler, method, target, bodyJSON string, hdr map[string]string, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(bodyJSON))
	req.RemoteAddr = remote
	req.Host = "shepherd.example.com"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func viaProxy(extra map[string]string) map[string]string {
	h := map[string]string{"X-Forwarded-Proto": "https", "Origin": pub}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func TestBrowserLoginSetsHostCookieAndHeaders(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	w := do(h, "POST", "/v1/web/login", `{"password":"`+pw+`","code":"`+r.code(t)+`"}`, viaProxy(nil), "10.0.0.5:1234")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers: %v", w.Header())
	}
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].Name != "__Host-shepherd_session" || !c[0].Secure || !c[0].HttpOnly || c[0].Path != "/" || c[0].Domain != "" || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", c)
	}
	var out reply
	json.NewDecoder(w.Body).Decode(&out)
	if out.CSRF != "csrf-value" || r.ses.n != 1 {
		t.Errorf("%+v", out)
	}
}

func TestLoginRouteHeadersOnFailureToo(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	for name, c := range map[string]struct{ method, body string }{
		"wrong code": {"POST", `{"password":"` + pw + `","code":"000000"}`},
		"bad json":   {"POST", `{`},
		"wrong verb": {"GET", ``},
	} {
		w := do(h, c.method, "/v1/web/login", c.body, viaProxy(nil), "10.0.0.5:1")
		if w.Code < 400 || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("%s set a cookie", name)
		}
	}
}

func TestRecoveryCodeBrowserLoginOnce(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	body := `{"password":"` + pw + `","code":"` + r.rec[0] + `"}`
	if w := do(h, "POST", "/v1/web/login", body, viaProxy(nil), "10.0.0.5:1"); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	if w := do(h, "POST", "/v1/web/login", body, viaProxy(nil), "10.0.0.5:1"); w.Code != 401 {
		t.Fatalf("reused recovery code: %d", w.Code)
	}
}

func TestBrowserLoginNeedsThePublicOrigin(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	b := `{"password":"` + pw + `","code":"` + r.code(t) + `"}`
	for _, o := range []string{"", "https://evil.example", "http://shepherd.example.com", pub + "/"} {
		hdr := viaProxy(map[string]string{"Origin": o})
		if o == "" {
			delete(hdr, "Origin")
		}
		if w := do(h, "POST", "/v1/web/login", b, hdr, "10.0.0.5:1"); w.Code != 403 {
			t.Errorf("origin %q: %d", o, w.Code)
		}
	}
	if r.ses.n != 0 {
		t.Error("a session started for a refused origin")
	}
}

func TestBurstIsRefusedPerAddressAndForwardedForFromStrangersIgnored(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	bad := `{"password":"` + pw + `","code":"000000"}`
	// Through the proxy, each client address is its own: ten wrong tries from one lock it.
	for i := 0; i < 5; i++ {
		do(h, "POST", "/v1/web/login", bad, viaProxy(map[string]string{"X-Forwarded-For": "198.51.100.1"}), "10.0.0.5:1")
	}
	good := `{"password":"` + pw + `","code":"` + r.code(t) + `"}`
	w := do(h, "POST", "/v1/web/login", good, viaProxy(map[string]string{"X-Forwarded-For": "198.51.100.1"}), "10.0.0.5:1")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("a burst was not refused: %d %v", w.Code, w.Header())
	}
}

func TestAddressLimitIgnoresForgedHeaderFromUntrusted(t *testing.T) {
	// TLS mode, no proxies: X-Forwarded-For is never read. Ten tries from one connection,
	// each claiming another client, still lock that connection's address.
	r := newRig(t, tlsCfg())
	r.h.Auth.Limiter = auth.NewLimiter(auth.Limits{AccountMax: 1000})
	h := r.server(r.h.Public)
	bad := `{"password":"` + pw + `","code":"000000"}`
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/web/login", strings.NewReader(bad))
		req.TLS = tlsState()
		req.Host, req.RemoteAddr = "shepherd.example.com", "203.0.113.50:9"
		req.Header.Set("Origin", pub)
		req.Header.Set("X-Forwarded-For", "192.0.2."+string(rune('1'+i)))
		h.ServeHTTP(w, req)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/web/login", strings.NewReader(`{"password":"`+pw+`","code":"`+r.code(t)+`"}`))
	req.TLS = tlsState()
	req.Host, req.RemoteAddr = "shepherd.example.com", "203.0.113.50:9"
	req.Header.Set("Origin", pub)
	req.Header.Set("X-Forwarded-For", "192.0.2.99")
	h.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("a forged X-Forwarded-For dodged the address limit: %d", w.Code)
	}
}

func TestAccountBurstRefused(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	bad := `{"password":"` + pw + `","code":"000000"}`
	for i := 0; i < 5; i++ { // five different addresses: only the account limit can catch it
		do(h, "POST", "/v1/web/login", bad, viaProxy(map[string]string{"X-Forwarded-For": "198.51.100." + string(rune('1'+i))}), "10.0.0.5:1")
	}
	good := `{"password":"` + pw + `","code":"` + r.code(t) + `"}`
	if w := do(h, "POST", "/v1/web/login", good, viaProxy(map[string]string{"X-Forwarded-For": "192.0.2.77"}), "10.0.0.5:1"); w.Code != 429 {
		t.Fatalf("account not locked: %d", w.Code)
	}
}

func TestOperatorTokenIs404OnPublicAndSameAsMissingRoute(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	auth := map[string]string{"Authorization": "Bearer " + op, "X-Forwarded-Proto": "https"}
	w := do(h, "GET", "/v1/status", "", auth, "10.0.0.5:1")
	missing := do(h, "GET", "/v1/no-such-route", "", map[string]string{"X-Forwarded-Proto": "https"}, "10.0.0.5:1")
	if w.Code != 404 || missing.Code != 404 || w.Body.String() != missing.Body.String() {
		t.Fatalf("operator token: %d %q; missing route: %d %q", w.Code, w.Body, missing.Code, missing.Body)
	}
	if w := do(h, "GET", "/v1/status", "", map[string]string{"X-Forwarded-Proto": "https"}, "10.0.0.5:1"); w.Code != 200 {
		t.Errorf("a plain request: %d", w.Code)
	}
}

func TestProxyModeRefusals(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	for name, c := range map[string]struct {
		hdr    map[string]string
		remote string
		want   int
	}{
		"untrusted source":       {map[string]string{"X-Forwarded-Proto": "https"}, "203.0.113.9:1", 403},
		"no forwarded proto":     {nil, "10.0.0.5:1", 403},
		"forwarded proto http":   {map[string]string{"X-Forwarded-Proto": "http"}, "10.0.0.5:1", 403},
		"trusted and https":      {map[string]string{"X-Forwarded-Proto": "https"}, "10.0.0.5:1", 200},
		"mixed-case proto value": {map[string]string{"X-Forwarded-Proto": "HTTPS"}, "10.0.0.5:1", 200},
	} {
		if w := do(h, "GET", "/v1/status", "", c.hdr, c.remote); w.Code != c.want {
			t.Errorf("%s: %d, want %d", name, w.Code, c.want)
		}
	}
}

func TestHostCheckAcceptsEitherURLFormAndNothingElse(t *testing.T) {
	for _, u := range []string{"https://shepherd.example.com", "https://shepherd.example.com/"} {
		p := proxyCfg()
		norm, err := config.NormalizeURL(u)
		if err != nil {
			t.Fatal(err)
		}
		p.URL = norm
		r := newRig(t, p)
		h := r.server(p)
		for _, host := range []string{"shepherd.example.com", "Shepherd.Example.com", "shepherd.example.com:443"} {
			req := httptest.NewRequest("GET", "/v1/status", nil)
			req.Host, req.RemoteAddr = host, "10.0.0.5:1"
			req.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("url %q host %q: %d", u, host, w.Code)
			}
		}
		for _, host := range []string{"evil.example", "127.0.0.1", "shepherd.example.com.evil.example", "shepherd.example.com:8443"} {
			req := httptest.NewRequest("GET", "/v1/status", nil)
			req.Host, req.RemoteAddr = host, "10.0.0.5:1"
			req.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code == 200 {
				t.Errorf("url %q host %q passed", u, host)
			}
		}
	}
}

func TestDeviceLogin(t *testing.T) {
	r := newRig(t, proxyCfg())
	h := r.server(r.h.Public)
	hdr := map[string]string{"X-Forwarded-Proto": "https"}
	w := do(h, "POST", "/v1/device/login", `{"password":"`+pw+`","code":"`+r.code(t)+`","name":"laptop"}`, hdr, "10.0.0.5:1")
	var out reply
	json.NewDecoder(w.Body).Decode(&out)
	if w.Code != 200 || !strings.HasPrefix(out.Token, "shd_") || out.DeviceName != "laptop" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d %+v", w.Code, out)
	}
	if !out.MaxExpires.After(out.Expires) {
		t.Errorf("expiry %v, cap %v", out.Expires, out.MaxExpires)
	}
	if _, err := r.h.Auth.CheckDevice(context.Background(), out.Token); err != nil {
		t.Error(err)
	}
	// A page's request carries Origin; the CLI's route refuses it.
	hdr["Origin"] = pub
	if w := do(h, "POST", "/v1/device/login", `{"password":"`+pw+`","code":"`+r.code(t)+`"}`, hdr, "10.0.0.5:1"); w.Code != 403 {
		t.Errorf("device login from a page: %d", w.Code)
	}
}
