package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// No credential of any kind reaches the health endpoint, and it answers only "up".
func TestHealthNoAuth(t *testing.T) {
	s, base, _ := webDaemon(t)
	for _, hdr := range []map[string]string{nil, {"Authorization": "Bearer wrong"}, {"Authorization": "Bearer " + s.Token}} {
		req, _ := http.NewRequest(http.MethodGet, base+healthPath, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("headers %v: status %d", hdr, resp.StatusCode)
		}
		if string(body) != "up\n" {
			t.Errorf("headers %v: body %q", hdr, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("headers %v: Content-Type %q", hdr, ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("headers %v: Cache-Control %q", hdr, cc)
		}
		if no := resp.Header.Get("X-Content-Type-Options"); no != "nosniff" {
			t.Errorf("headers %v: X-Content-Type-Options %q", hdr, no)
		}
	}
	// Nothing signed in, nothing slid: no session exists.
	s.web.mu.Lock()
	n := len(s.web.sessions)
	s.web.mu.Unlock()
	if n != 0 {
		t.Errorf("a health check left %d sessions", n)
	}
}

// Only GET and HEAD; anything else is 405 with an Allow header, and the body says no
// more than the status does.
func TestHealthMethods(t *testing.T) {
	_, base, _ := webDaemon(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		req, _ := http.NewRequest(method, base+healthPath, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d", method, resp.StatusCode)
		}
		if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("%s: Allow %q", method, allow)
		}
		if strings.Contains(string(body), "up") {
			t.Errorf("%s: refused body says up: %q", method, body)
		}
	}
	req, _ := http.NewRequest(http.MethodHead, base+healthPath, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) != 0 {
		t.Errorf("HEAD: status %d body %q", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("HEAD: Cache-Control %q", cc)
	}
}

// The loopback Host check still guards the endpoint: a rebound name does not reach it.
func TestHealthLoopbackHostCheck(t *testing.T) {
	_, base, _ := webDaemon(t)
	req, _ := http.NewRequest(http.MethodGet, base+healthPath, nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("rebound host: status %d", resp.StatusCode)
	}
}

// On the public listener the endpoint answers for the public URL and for a bare pod
// or IP address (what a kubelet sends), still with no credential; every other route
// keeps its auth exactly as before. The request goes straight at the daemon's handler
// marked public, past the guard: the guard's own bypass for this path is a hunk on
// another lane (internal/login/guard.go), covered by its tests.
func TestHealthPublicListener(t *testing.T) {
	r := newHRig(t)
	h := r.s.hostedNow()
	if h == nil {
		t.Fatal("hosted mode not prepared")
	}
	serve := func(t *testing.T, method, host, path string, hdr map[string]string) (int, string, http.Header) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://x"+path, nil)
		req.Host = host
		req = req.WithContext(contextWithHosted(req.Context(), h))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		r.s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String(), rec.Header()
	}
	for _, host := range []string{"shepherd.example.com", "shepherd.example.com:443", "10.244.1.23:8443", "10.244.1.23"} {
		code, body, hdr := serve(t, http.MethodGet, host, healthPath, nil)
		if code != http.StatusOK || body != "up\n" {
			t.Errorf("host %s: status %d body %q", host, code, body)
		}
		if cc := hdr.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("host %s: Cache-Control %q", host, cc)
		}
	}
	if code, _, _ := serve(t, http.MethodPost, "10.244.1.23:8443", healthPath, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("public POST: status %d", code)
	}
	// Other routes are untouched: still 401 without a credential, on both listeners.
	if code, _, _ := serve(t, http.MethodGet, "10.244.1.23:8443", api.PathStatus, nil); code != http.StatusUnauthorized {
		t.Errorf("public status without a token: status %d", code)
	}
	req, _ := http.NewRequest(http.MethodGet, r.loop.URL+api.PathStatus, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("loopback status without a token: status %d", resp.StatusCode)
	}
}
