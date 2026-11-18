// Package login is the HTTP side of hosted mode: the guard every request on the public
// listener passes (TLS or trusted proxy, the public Host, no operator token), and the
// login routes for a browser and for the CLI. It holds no state of its own: the account,
// limits and device tokens are internal/auth's, and the browser session behind the
// cookie is the daemon's, supplied as a SessionStarter.
package login
