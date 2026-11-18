package login

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

// CookieName is the session cookie on the public listener. The __Host- prefix makes a
// browser accept it only with Secure, Path=/ and no Domain.
const CookieName = "__Host-shepherd_session"

// SessionStarter begins a browser session after a good login, in the daemon's
// web_sessions: the random cookie value (kept only as a hash), how long it lasts, and
// the CSRF token the page sends on every write.
type SessionStarter interface {
	StartSession(ctx context.Context) (value string, ttl time.Duration, csrf string, err error)
}

// Cookie is the public listener's session cookie.
func Cookie(value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: value, Path: "/", MaxAge: max(int(ttl/time.Second), 1),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

// ClearCookie is the cookie that ends one.
func ClearCookie() *http.Cookie {
	c := Cookie("", time.Second)
	c.MaxAge = -1
	return c
}

// Handler serves the two login routes. Mount Browser at POST /v1/web/login and Device at
// POST /v1/device/login, both behind Guard.
type Handler struct {
	Auth     *auth.Service
	Public   *config.Public
	Sessions SessionStarter
}

type body struct {
	Password string `json:"password"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

type reply struct {
	Error      string    `json:"error,omitempty"`
	CSRF       string    `json:"csrf,omitempty"`
	Expires    time.Time `json:"expires,omitzero"`
	Token      string    `json:"token,omitempty"`
	DeviceID   string    `json:"device_id,omitempty"`
	DeviceName string    `json:"device_name,omitempty"`
	MaxExpires time.Time `json:"max_expires,omitzero"`
}

func send(w http.ResponseWriter, code int, v reply) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// read checks the request is a JSON POST and decodes it, with the headers the login
// route always sends set first.
func (h *Handler) read(w http.ResponseWriter, r *http.Request) (body, bool) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	var b body
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		send(w, http.StatusMethodNotAllowed, reply{Error: "POST only"})
		return b, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil || b.Password == "" || b.Code == "" {
		send(w, http.StatusBadRequest, reply{Error: "a password and a code are needed"})
		return b, false
	}
	return b, true
}

func (h *Handler) attempt(w http.ResponseWriter, r *http.Request, b body) bool {
	err := h.Auth.Login(r.Context(), auth.Attempt{Password: b.Password, Code: b.Code, Addr: ClientAddr(r)})
	var locked *auth.LockedError
	switch {
	case err == nil:
		return true
	case errors.As(err, &locked):
		w.Header().Set("Retry-After", strconv.Itoa(max(int(time.Until(locked.Until)/time.Second), 1)))
		send(w, http.StatusTooManyRequests, reply{Error: locked.Error()})
	case errors.Is(err, auth.ErrInvalid):
		send(w, http.StatusUnauthorized, reply{Error: auth.ErrInvalid.Error()})
	case errors.Is(err, auth.ErrNoAccount):
		send(w, http.StatusServiceUnavailable, reply{Error: err.Error()})
	default:
		send(w, http.StatusInternalServerError, reply{Error: "login failed"})
	}
	return false
}

// Browser is POST /v1/web/login: password and code for a session cookie. The page's
// Origin must be the public URL; a request with none, or another, is refused.
func (h *Handler) Browser(w http.ResponseWriter, r *http.Request) {
	b, ok := h.read(w, r)
	if !ok {
		return
	}
	if r.Header.Get("Origin") != h.Public.URL {
		send(w, http.StatusForbidden, reply{Error: "cross-origin request refused"})
		return
	}
	if !h.attempt(w, r, b) {
		return
	}
	val, ttl, csrf, err := h.Sessions.StartSession(r.Context())
	if err != nil {
		send(w, http.StatusInternalServerError, reply{Error: "login failed"})
		return
	}
	http.SetCookie(w, Cookie(val, ttl))
	send(w, http.StatusOK, reply{CSRF: csrf, Expires: time.Now().Add(ttl).UTC()})
}

// Device is POST /v1/device/login: the CLI's way in, for a named device token. It is no
// browser route, so an Origin header (a page's request) is refused.
func (h *Handler) Device(w http.ResponseWriter, r *http.Request) {
	b, ok := h.read(w, r)
	if !ok {
		return
	}
	if r.Header.Get("Origin") != "" {
		send(w, http.StatusForbidden, reply{Error: "not for browsers"})
		return
	}
	if !h.attempt(w, r, b) {
		return
	}
	tok, d, err := h.Auth.IssueDevice(r.Context(), b.Name)
	if err != nil {
		send(w, http.StatusBadRequest, reply{Error: err.Error()})
		return
	}
	send(w, http.StatusOK, reply{Token: tok, DeviceID: d.ID, DeviceName: d.Name, Expires: d.Expires.UTC(), MaxExpires: d.Created.Add(auth.DeviceCap).UTC()})
}
