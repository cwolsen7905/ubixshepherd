package login

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardHealthBypass(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "up")
	})
	g := Guard(proxyCfg(), nil, next)
	probe := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "192.168.7.9:5555" // untrusted, no X-Forwarded-Proto
		req.Host = "10.42.0.17:8443"        // pod IP, not the public host
		w := httptest.NewRecorder()
		g.ServeHTTP(w, req)
		return w
	}
	for _, m := range []string{"GET", "HEAD"} {
		w := probe(m, "/v1/health")
		if w.Code != 200 {
			t.Errorf("%s /v1/health: %d", m, w.Code)
		}
		if m == "GET" && w.Body.String() != "up" {
			t.Errorf("body %q", w.Body)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Strict-Transport-Security") == "" {
			t.Errorf("%s headers: %v", m, w.Header())
		}
	}
	if w := probe("POST", "/v1/health"); w.Code != http.StatusForbidden {
		t.Errorf("POST /v1/health: %d", w.Code)
	}
	for _, p := range []string{"/v1/status", "/v1/health/x", "/v1/healthz"} {
		if w := probe("GET", p); w.Code != http.StatusForbidden {
			t.Errorf("GET %s: %d", p, w.Code)
		}
	}
}
