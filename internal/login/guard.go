package login

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

type ctxKey struct{}

// ClientAddr is the address the guard decided a request counts against: the
// connection's, or the first untrusted hop of X-Forwarded-For from a trusted proxy.
func ClientAddr(r *http.Request) netip.Addr {
	a, _ := r.Context().Value(ctxKey{}).(netip.Addr)
	return a
}

// Guard wraps the public listener's handler. In order, a request must:
//   - arrive over TLS (the daemon's own), or from a trusted proxy that marked it
//     X-Forwarded-Proto: https; a plain-HTTP request from anyone else is refused;
//   - name the public host in Host;
//   - not carry the operator token, which is answered 404 exactly as a route that does
//     not exist is, so the public side does not confirm the token's existence.
//
// IsOperator says whether a request presents the operator token; the daemon supplies it.
func Guard(p *config.Public, isOperator func(*http.Request) bool, next http.Handler) http.Handler {
	hosts := map[string]bool{}
	for _, h := range p.HostNames() {
		hosts[h] = true
	}
	proxies := p.ProxyPrefixes()
	viaProxy := !p.ServesTLS()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		conn, _, _ := strings.Cut(r.RemoteAddr, "%")
		var ip netip.Addr
		if ap, err := netip.ParseAddrPort(conn); err == nil {
			ip = ap.Addr().Unmap()
		}
		if viaProxy {
			trusted := false
			for _, pr := range proxies {
				trusted = trusted || (ip.IsValid() && pr.Contains(ip))
			}
			if !trusted {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
				http.Error(w, "this address is https only", http.StatusForbidden)
				return
			}
		} else if r.TLS == nil {
			http.Error(w, "this address is https only", http.StatusBadRequest)
			return
		}
		if !hosts[strings.ToLower(r.Host)] {
			http.Error(w, "unexpected Host", http.StatusMisdirectedRequest)
			return
		}
		if isOperator != nil && isOperator(r) {
			http.NotFound(w, r)
			return
		}
		addr := auth.ClientAddr(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), proxies)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, addr)))
	})
}

// Listener is a tcp listener for the public address.
func Listener(p *config.Public) (net.Listener, error) { return net.Listen("tcp", p.Listen) }
