# Hosted mode: a daemon reachable over HTTPS with a login

Status: **in progress.** The settings, the account, the second factor, the device tokens,
the rate limits and the login routes are built and tested as packages; the daemon does not
serve them yet. Until it does, a daemon refuses to start with `daemon.public` set, and
nothing below changes how a daemon runs today. This page says what the finished feature
does, and marks what is not wired yet.

A hosted daemon is any daemon with three opt-in settings. Without `daemon.public` nothing
changes: it listens on loopback only, as before.

| Setting | What it does |
|---|---|
| `daemon.public.listen` and `daemon.public.url` | A second listener beyond loopback, and the public address people use |
| `daemon.public.tls`, or `daemon.public.trusted_proxies` | Either the daemon serves HTTPS itself from a certificate and key, or a proxy in front ends TLS and the daemon takes plain HTTP only from it |
| An owner account | Set with `shepherd account set`; browser sessions and device tokens come from this login |

The daemon refuses to start with a public listener unless TLS (or a trusted proxy) and an
account are both present. The loopback listener stays up beside it, for the CLI on the
same machine and for the operator token.

## Configuration

```yaml
daemon:
  public:
    listen: 0.0.0.0:8443
    url: https://shepherd.example.com
    tls: {cert: /etc/shepherd/tls.crt, key: /etc/shepherd/tls.key}
    # or, instead of tls:
    # trusted_proxies: [10.0.0.5, 192.168.0.0/16]
    totp_key_file: /run/secrets/shepherd-totp-key
```

| Key | Rule |
|---|---|
| `listen` | `host:port` with a fixed port, not the port of `daemon.listen` |
| `url` | `https://` only, no path other than `/`, no query, fragment or user. A trailing slash is dropped and the host lower-cased, so `https://shepherd.example.com` and `https://shepherd.example.com/` are one setting. The Host header may be the name with or without `:443` |
| `tls.cert`, `tls.key` | Both or neither. Mutually exclusive with `trusted_proxies` |
| `trusted_proxies` | IP addresses or CIDRs. The daemon refuses a connection from any other address, and a request from a proxy that is not marked `X-Forwarded-Proto: https`. `X-Forwarded-For` is read only on requests from these addresses |
| `totp_key_file` | An absolute path to a file of mode 0600 (see below) |

## The login

The owner account has a password (at least 12 characters, kept as an argon2id hash) and a
second factor. TOTP is the first; the code behind it is a `SecondFactor` (enrol, verify,
list, remove), so a passkey can be added later without changing the login.

`shepherd account set` prints, once, the TOTP enrolment address and its typed secret, and
ten recovery codes. A recovery code works once in place of a TOTP code and is kept only as
a hash. A TOTP code cannot be used twice, and an earlier code than the last one used is
refused. Setting the password again, or changing the second factor, signs everything out.

| Way in | Route | Gives |
|---|---|---|
| Browser | `POST /v1/web/login`, from a page whose Origin is `daemon.public.url` | A session cookie named `__Host-shepherd_session` (Secure, HttpOnly, SameSite=Strict, Path `/`), and the CSRF token the page sends on every write |
| CLI | `POST /v1/device/login` (`shepherd login`, not yet built) | A named device token, shown once |

The login routes send `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`.
Every wrong password, wrong code and reused code gives the same answer: "wrong password or
code".

### Rate limiting

Both per account and per client address. Five failures on the account, or ten from one
address (a whole IPv6 /64 counts as one), within 15 minutes lock it for 15 minutes. A
locked account refuses the right password too, answers 429 with `Retry-After`, and puts one
item in the feed. The client address is the connection's, or, when the connection is from
a trusted proxy, the first address in `X-Forwarded-For` (walking from the right) that is not
itself a trusted proxy. From anyone else the header is ignored.

### Device tokens

A device token is named and kept only as a hash. It lasts 30 days from its last use (each
use pushes the expiry out), and never past 90 days from the login that made it, after which
a fresh login is needed however often it was used. A token that is unknown, revoked or
expired gets one answer, "log in again", which says nothing of its age.

Logging out ends one session. "Sign out everywhere" ends every session and every device
token. Both are rows in the store, so they hold across a restart.

## The TOTP key

The TOTP secret is encrypted at rest (AES-256-GCM) with a key kept apart from the store.
This protects a copy of the database without the key, such as a leaked backup.

| Setting | The key |
|---|---|
| `daemon.public.totp_key_file` set | That file, at least 16 bytes, mode 0600, synced in from your secret store. A missing, unreadable, too-short or group- or world-readable file stops the start. A copy of the whole Shepherd home does not hold the key |
| Not set | `totp.key` in the Shepherd home, created with mode 0600 on first need. The daemon logs a warning at every start, and files one feed item the first time, that the key sits beside the store: a reader of the whole home has both |

## The operator token

It works only on the loopback listener. On the public listener it gets `404 Not Found`,
the same answer as a route that does not exist.

## Never exposed

On the public listener, in logs, in the feed and in the web UI: the operator token, worker
and desk tokens, password and recovery-code hashes, the TOTP secret and its key, agent CLI
credentials, forge tokens and git push keys.

## Not wired yet

The packages `internal/auth` and `internal/login` and the web pages in `web/src/auth` and
`web/src/pages/Login.tsx` are in place. The store tables, the daemon's second listener and
startup checks, `shepherd account`, the Settings device list, and the end-to-end secret
test wait on the daemon, store and API changes of the same phase.
