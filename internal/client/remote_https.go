package client

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewHTTPClient is the HTTP client for a hosted daemon: the system's trust roots, TLS 1.2
// or newer, and no redirects followed (a hosted daemon never redirects an API call, and a
// redirect must not carry the device token anywhere else). There is no switch to skip
// certificate checks.
func NewHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{
		Transport:     tr,
		Timeout:       5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// NewHTTPS returns a client for a hosted daemon at url (https://host[:port], normalised),
// signed in with a device token. hc carries the TLS settings; nil is NewHTTPClient. An
// answer of 401 is the one message "log in again: shepherd login <url>", whatever the
// cause (unknown, revoked or expired), and it says nothing of the token's age.
func NewHTTPS(url, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = NewHTTPClient()
	}
	c := New(url, token)
	c.http = hc
	c.host = url
	c.hostedURL = url
	return c
}

// LoginAgain is a hosted daemon's refusal of the device token.
type LoginAgain struct{ URL string }

func (e *LoginAgain) Error() string { return "log in again: shepherd login " + e.URL }
