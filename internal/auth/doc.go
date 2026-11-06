// Package auth is the daemon's own login: one owner account with a password and a
// second factor (TOTP first), recovery codes, device tokens for the CLI, and the rate
// limits around them. It knows nothing of HTTP sessions or the database: the daemon
// supplies a Store and, to end browser sessions, a SessionEnder.
//
// Nothing here logs or returns a secret: the password, recovery codes, TOTP secret and
// its key, and device tokens appear only in the value handed back once to the person.
package auth
