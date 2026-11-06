package login

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

func tlsCfg() *config.Public {
	return &config.Public{Listen: "0.0.0.0:8443", URL: pub, TLS: config.PublicTLS{Cert: "c", Key: "k"}}
}

func tlsState() *tls.ConnectionState { return &tls.ConnectionState{HandshakeComplete: true} }

func TestTLSModeRefusesPlainHTTPAndIgnoresProxyHeaders(t *testing.T) {
	r := newRig(t, tlsCfg())
	h := r.server(r.h.Public)
	req := httptest.NewRequest("GET", "/v1/status", strings.NewReader(""))
	req.Host, req.RemoteAddr = "shepherd.example.com", "203.0.113.9:1"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plain HTTP with a forged X-Forwarded-Proto: %d", w.Code)
	}
	req.TLS = tlsState()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}
