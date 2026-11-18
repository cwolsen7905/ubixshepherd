package daemon

// Hosted mode: the public listener, the owner account behind it, and the routes that
// manage the account's sign-ins. Without daemon.public none of this runs.
//
// The public listener serves the same handler as loopback (Handler), marked public, so
// every route keeps its role check. What changes on a marked request: the session cookie
// is the __Host- one, the Origin must be the public URL, device tokens are the only
// bearer tokens, and the sign-in link is refused. login.Guard in front answers the
// operator token 404.

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/login"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// publicPort is the "port" a session started on the public listener records, so the
// loopback listener's port reuse (lastPort) never picks it.
const publicPort = "public"

type publicKey struct{}

// isPublic says the request came in on the public listener.
func isPublic(r *http.Request) bool {
	v, _ := r.Context().Value(publicKey{}).(*hosted)
	return v != nil
}

func hostedOf(r *http.Request) *hosted {
	v, _ := r.Context().Value(publicKey{}).(*hosted)
	return v
}

// publicOrigin says the request's Origin, when it has one, is the public URL.
func publicOrigin(r *http.Request) bool {
	h := hostedOf(r)
	return h != nil && strings.EqualFold(r.Header.Get("Origin"), h.pub.URL)
}

// hosted is the running hosted mode: its settings, the login and the key.
type hosted struct {
	pub *config.Public
	svc *auth.Service
	key *auth.Key
	log func(msg string, args ...any)
	// accountSet is closed when an account exists.
	accountSet chan struct{}
	once       sync.Once
	// addr is the public listener's address once it listens.
	addr atomic.Pointer[string]
}

// PublicAddr is where the public listener listens, "" before it does.
func (s *Server) PublicAddr() string {
	if h := s.hostedNow(); h != nil {
		if a := h.addr.Load(); a != nil {
			return *a
		}
	}
	return ""
}

func contextWithHosted(ctx context.Context, h *hosted) context.Context {
	return context.WithValue(ctx, publicKey{}, h)
}

// hostedState is kept on webAuth, beside the sessions it ends.
type hostedState struct {
	mu sync.Mutex
	h  *hosted
}

// PreparePublic checks everything the public listener needs that can be checked without
// an account: the settings, a key (from totp_key_file, or totp.key in home), and a store
// that keeps accounts. An error stops the daemon's start. It logs the key fallback
// warning on every start, and files one feed item the first time.
func (s *Server) PreparePublic(ctx context.Context, home string) error {
	cfg := s.LiveConfig()
	pub := cfg.Daemon.Public
	if !pub.Enabled() {
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	ap, ok := s.Store.(interface{ Accounts() auth.Store })
	if !ok {
		return errors.New("daemon.public: this store does not keep accounts")
	}
	_, statErr := os.Stat(filepath.Join(home, auth.KeyFileName))
	fresh := pub.TOTPKeyFile == "" && statErr != nil
	key, err := auth.LoadKey(pub.TOTPKeyFile, home)
	if err != nil {
		return err
	}
	if key.Fallback {
		s.Log.Warn(auth.KeyFallbackWarning)
		if fresh {
			s.Store.AddFeed(ctx, store.FeedSession, auth.KeyFallbackWarning, 0)
		}
	}
	st := ap.Accounts()
	h := &hosted{pub: pub, key: key, accountSet: make(chan struct{}), log: func(msg string, a ...any) { s.Log.Info(msg, a...) }}
	h.svc = &auth.Service{
		Store:    st,
		Factor:   &auth.TOTP{Store: st, Key: key},
		Limiter:  auth.NewLimiter(auth.Limits{}),
		Sessions: sessionEnder{s},
		OnLock: func(addr netip.Addr, until time.Time) {
			msg := fmt.Sprintf("sign-in locked until %s after repeated failures", until.UTC().Format("15:04 MST"))
			s.Log.Warn("sign-in locked", "until", until.UTC().Format(time.RFC3339), "addr", addr.String())
			s.Store.AddFeed(context.Background(), store.FeedSession, msg, 0)
		},
	}
	if _, err := st.Account(ctx); err == nil {
		h.once.Do(func() { close(h.accountSet) })
	}
	s.web.hosted.mu.Lock()
	s.web.hosted.h = h
	s.web.hosted.mu.Unlock()
	return nil
}

type sessionEnder struct{ s *Server }

func (e sessionEnder) EndAllSessions(context.Context) (int, error) { return e.s.web.end(nil), nil }

func (s *Server) hostedNow() *hosted {
	s.web.hosted.mu.Lock()
	defer s.web.hosted.mu.Unlock()
	return s.web.hosted.h
}

// PublicHandler is the handler of the public listener: the guard, then the daemon's own
// handler marked public. PreparePublic must have run.
func (s *Server) PublicHandler() http.Handler {
	h := s.hostedNow()
	if h == nil {
		return http.NotFoundHandler()
	}
	want := []byte("Bearer " + s.Token)
	isOperator := func(r *http.Request) bool {
		return subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) == 1
	}
	inner := s.Handler()
	return login.Guard(h.pub, isOperator, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(contextWithHosted(r.Context(), h)))
	}))
}

// ServePublic opens the public listener and serves it until ctx ends. It waits for the
// owner account first: with none, the listener stays closed (the loopback one is up, and
// `shepherd account set` is how an account is made), so nothing is reachable without a
// login to reach it with.
func (s *Server) ServePublic(ctx context.Context) error {
	h := s.hostedNow()
	if h == nil {
		return nil
	}
	select {
	case <-h.accountSet:
	case <-ctx.Done():
		return nil
	}
	ln, err := login.Listener(h.pub)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.PublicHandler(), ReadHeaderTimeout: 10 * time.Second}
	if h.pub.ServesTLS() {
		cert, err := tls.LoadX509KeyPair(h.pub.TLS.Cert, h.pub.TLS.Key)
		if err != nil {
			ln.Close()
			return fmt.Errorf("daemon.public.tls: %w", err)
		}
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	a := ln.Addr().String()
	h.addr.Store(&a)
	s.Log.Info("shepherd public listener", "addr", ln.Addr().String(), "url", h.pub.URL)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		sd, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sd)
	case err := <-errc:
		return err
	}
}

// deviceAuth resolves a bearer token on the public listener to a device principal. On
// failure it has written the answer. A token that is unknown, revoked or expired gets
// the one "log in again" message.
func (s *Server) deviceAuth(w http.ResponseWriter, r *http.Request, header string) (principal, bool) {
	h := hostedOf(r)
	tok, _ := strings.CutPrefix(header, "Bearer ")
	d, err := h.svc.CheckDevice(r.Context(), tok)
	switch {
	case errors.Is(err, auth.ErrLoginAgain):
		writeError(w, http.StatusUnauthorized, errors.New("log in again: shepherd login "+h.pub.URL))
		return principal{}, false
	case err != nil:
		writeError(w, http.StatusInternalServerError, errors.New("could not check the token"))
		return principal{}, false
	}
	return principal{Role: RoleWeb, Human: true, device: d.ID}, true
}

// StartSession starts a browser session on the public listener after a good login.
func (s *Server) StartSession(context.Context) (string, time.Duration, string, error) {
	val, ws, err := s.web.start(publicPort, true)
	if err != nil {
		return "", 0, "", err
	}
	return val, webSessionTTL, ws.csrf, nil
}

// accountRoute serves the account's routes, which sit outside the API mux so that
// browserGate can reach them on both listeners. It reports whether it took the request.
//
//	POST /v1/account              operator, loopback: set the password; the reply shows the
//	                              TOTP enrolment and recovery codes once
//	POST /v1/web/login            public: password and code for a session
//	POST /v1/device/login         public: password and code for a device token
//	GET  /v1/devices              session or device token: the device tokens, listed
//	POST /v1/devices/{id}/revoke  session or device token: end one
//	POST /v1/web/signout-all      session or device token: end every session and token
func (s *Server) accountRoute(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == api.PathAccount && r.Method == http.MethodPost:
		w.Header().Set("Cache-Control", "no-store")
		s.auth(guard("POST "+api.PathAccount, operatorOnly, s.accountSet)).ServeHTTP(w, r)
	case path == api.PathWebLogin || path == api.PathDeviceLogin:
		h := hostedOf(r)
		if h == nil {
			http.NotFound(w, r)
			return true
		}
		lh := &login.Handler{Auth: h.svc, Public: h.pub, Sessions: s}
		if path == api.PathWebLogin {
			lh.Browser(w, r)
		} else {
			lh.Device(w, r)
		}
	case path == api.PathDevices && r.Method == http.MethodGet:
		s.auth(guard("GET "+api.PathDevices, forWeb, s.deviceList)).ServeHTTP(w, r)
	case path == api.PathWebSignoutAll && r.Method == http.MethodPost:
		s.auth(guard("POST "+api.PathWebSignoutAll, forWeb, s.signOutAll)).ServeHTTP(w, r)
	case strings.HasPrefix(path, api.PathDevices+"/") && strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		r.SetPathValue("id", strings.TrimSuffix(strings.TrimPrefix(path, api.PathDevices+"/"), "/revoke"))
		s.auth(guard("POST "+api.PathDevices+"/{id}/revoke", forWeb, s.deviceRevoke)).ServeHTTP(w, r)
	default:
		return false
	}
	return true
}

// service is the login a request is served by, or an error written to w.
func (s *Server) service(w http.ResponseWriter, r *http.Request) *auth.Service {
	if h := hostedOf(r); h != nil {
		return h.svc
	}
	if h := s.hostedNow(); h != nil {
		return h.svc
	}
	writeError(w, http.StatusNotFound, errors.New("hosted mode is not on: set daemon.public in the config"))
	return nil
}

// accountSet is POST /v1/account (operator, and only on loopback: the public guard
// answers the operator token 404 first).
func (s *Server) accountSet(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	var req api.AccountSet
	if !decode(w, r, &req) {
		return
	}
	setup, err := svc.SetPassword(r.Context(), req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if h := s.hostedNow(); h != nil {
		h.once.Do(func() { close(h.accountSet) })
	}
	s.Log.Info("owner account set; every session and device token ended")
	writeJSON(w, http.StatusOK, api.AccountSetup{URI: setup.Enrolment.URI, Secret: setup.Enrolment.Secret, RecoveryCodes: setup.RecoveryCodes})
}

func (s *Server) deviceList(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	ds, err := svc.Devices(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := api.DeviceList{Devices: []api.Device{}}
	for _, d := range ds {
		out.Devices = append(out.Devices, api.Device{ID: d.ID, Name: d.Name, Created: d.Created.UTC(), LastUsed: d.LastUsed.UTC(), Expires: d.Expires.UTC(), MaxExpires: d.Created.Add(auth.DeviceCap).UTC()})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deviceRevoke(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	ok, err := svc.RevokeDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("no such device"))
		return
	}
	s.Log.Info("device token revoked", "id", r.PathValue("id"))
	writeJSON(w, http.StatusOK, api.DeviceRevoked{Revoked: true})
}

func (s *Server) signOutAll(w http.ResponseWriter, r *http.Request) {
	svc := s.service(w, r)
	if svc == nil {
		return
	}
	sessions, devices, err := svc.SignOutEverywhere(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if isPublic(r) {
		http.SetCookie(w, login.ClearCookie())
	}
	s.Log.Info("signed out everywhere", "sessions", sessions, "devices", devices)
	writeJSON(w, http.StatusOK, api.SignedOutEverywhere{Sessions: sessions, Devices: devices})
}
