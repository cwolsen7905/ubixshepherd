package api

import "time"

// The browser's way in. The daemon serves the web UI at / on its own address, and a
// browser reaches the API with a session cookie instead of a token:
//
//	POST   /v1/web/signin         operator: a one-time sign-in link, WebSignin
//	GET    /v1/web/signin?code=C  the browser opens the link: the code is spent, a session
//	                              cookie is set, and it is sent on to /
//	GET    /v1/web/session        the browser's session: WebSession (its CSRF token)
//	POST   /v1/web/session/extend the browser asks for more time: WebSession (CSRF-checked)
//	POST   /v1/web/session/active the page saw the person act: slides the expiry, WebSession
//	POST   /v1/web/signout        the browser ends its own session
//	DELETE /v1/web/sessions       operator: end every browser session, WebSignedOut
//
// A browser session has the web role: everything the front desk may do, the front desk
// conversation (api/desk.go), and answering decisions as the person. It cannot change
// the daemon's setup (workspaces, scopes, hooks, imports, settings, shutdown) or mint
// sign-in links; the shepherd command does those with the operator token. It may read
// and preview config (api/config.go), and save it only when its link was minted with
// WebSigninRequest.AllowSettings.
//
// A request with the cookie must come from the daemon's own origin: its Origin header,
// when present, must be http://<the Host it was sent to>, and Sec-Fetch-Site, when
// present, same-origin (or none, for a link the person opened). A request that changes
// anything (not GET or HEAD) must also carry Origin and CSRFHeader with the session's
// CSRF token. The daemon never sends CORS headers. Every request, with a token or not,
// must name a loopback host (localhost, 127.0.0.0/8, ::1) in its Host header, so a DNS
// name rebound to 127.0.0.1 cannot reach the API from a web page.
const (
	PathWebSignin  = "/v1/web/signin"
	PathWebSession = "/v1/web/session"
	PathWebSignout = "/v1/web/signout"
	// PathWebSessionExtend pushes the session's expiry out, up to its absolute cap.
	PathWebSessionExtend = "/v1/web/session/extend"
	// PathWebSessionActive reports that the person used the page: the session slides.
	PathWebSessionActive = "/v1/web/session/active"
	PathWebSessions      = "/v1/web/sessions"
)

// Hosted mode (daemon.public, docs/hosted.md): the owner account and what hangs from it.
//
//	POST /v1/account              operator, loopback only: AccountSet in, AccountSetup out
//	POST /v1/web/login            public listener: a page's password and code for a session
//	POST /v1/device/login         public listener: the CLI's password and code for a device token
//	GET  /v1/devices              the device tokens: DeviceList
//	POST /v1/devices/{id}/revoke  end one: DeviceRevoked
//	POST /v1/web/signout-all      end every session and device token: SignedOutEverywhere
const (
	PathAccount       = "/v1/account"
	PathWebLogin      = "/v1/web/login"
	PathDeviceLogin   = "/v1/device/login"
	PathDevices       = "/v1/devices"
	PathWebSignoutAll = "/v1/web/signout-all"
)

// AccountSet is the body of POST /v1/account.
type AccountSet struct {
	Password string `json:"password"`
}

// AccountSetup is shown once, when an account is set: the authenticator's enrolment
// address and typed secret, and ten single-use recovery codes.
type AccountSetup struct {
	URI           string   `json:"uri"`
	Secret        string   `json:"secret"`
	RecoveryCodes []string `json:"recovery_codes"`
}

// Device is a device token as listed: never the token or its hash.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Created    time.Time `json:"created"`
	LastUsed   time.Time `json:"last_used"`
	Expires    time.Time `json:"expires"`
	MaxExpires time.Time `json:"max_expires"`
}

// DeviceList answers GET /v1/devices.
type DeviceList struct {
	Devices []Device `json:"devices"`
}

// DeviceRevoked answers POST /v1/devices/{id}/revoke.
type DeviceRevoked struct {
	Revoked bool `json:"revoked"`
}

// SignedOutEverywhere answers POST /v1/web/signout-all.
type SignedOutEverywhere struct {
	Sessions int `json:"sessions"`
	Devices  int `json:"devices"`
}

// CSRFHeader carries a browser session's CSRF token on every request that changes
// something.
const CSRFHeader = "X-Shepherd-CSRF"

// WebSignin answers POST /v1/web/signin. Path is the link without its scheme and host,
// for the client to put on the address it reaches the daemon at; it works once, until
// Expires.
type WebSignin struct {
	Path    string    `json:"path"`
	Expires time.Time `json:"expires"`
}

// WebSigninRequest is the optional body of POST /v1/web/signin. AllowSettings makes the
// session the link starts able to save config changes (PUT /v1/config); without it the
// browser may read and preview settings only. It lasts as long as the session, and not
// past a daemon restart: a session the daemon reloads from its store reads only.
type WebSigninRequest struct {
	AllowSettings bool `json:"allow_settings,omitempty"`
}

// WebSession answers GET /v1/web/session. CSRF is set for a browser session only.
type WebSession struct {
	Role    string    `json:"role"`
	CSRF    string    `json:"csrf,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
	// MaxExpires is the latest the session can last, however it is extended.
	MaxExpires time.Time `json:"max_expires,omitzero"`
	// AllowSettings: this browser session may save config changes.
	AllowSettings bool `json:"allow_settings,omitempty"`
}

// WebSignedOut answers POST /v1/web/signout and DELETE /v1/web/sessions: how many
// sessions ended.
type WebSignedOut struct {
	Ended int `json:"ended"`
}
