package daemon

import (
	"errors"
	"net/http"
)

// healthPath is the daemon's liveness endpoint. It answers only "up": no version, no
// counts, no ids, nothing a probe or a stranger could learn from. Probes (Kubernetes
// httpGet, Docker HEALTHCHECK, curl) need it without a credential, so browserGate
// answers it before auth; the public listener's guard lets the path through for the
// same reason, including for a pod or IP address Host, which is what kubelets send.
const healthPath = "/v1/health"

// health answers GET and HEAD /v1/health with 200 and the two characters "up".
// Anything else is 405. It touches no store, no session and no token, so there is
// nothing to leak and nothing to slide or extend.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, errors.New("only GET and HEAD are allowed"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// A HEAD write would be discarded by the server; say it plainly.
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte("up\n"))
	}
}
