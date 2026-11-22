# Phase 0: a reachable daemon with a login

Part of the [cluster design](../cluster.md). Phase 0 is unchanged by the 2026-11-22 revision except
for names (hub is now controller, member is now worker) and one deployment note.

## Hosted mode

Hosted mode is not a kind of daemon. It is three settings, all opt-in, on any daemon:

| Setting | What it does |
|---|---|
| A non-loopback listener (`daemon.public.listen`, with `daemon.public.url` naming the public address) | The daemon accepts connections beyond loopback and accepts the public name in the Host check. `url` must be `https://` with no path other than `/`, and no query or fragment; a trailing slash is dropped as it is for `hosts.yaml`, so `https://shepherd.example.com` and `https://shepherd.example.com/` are the same setting and the same Host-check name |
| TLS: a certificate and key (`daemon.public.tls`), or a TLS-terminating proxy (`daemon.public.trusted_proxies`) | Without a proxy the daemon serves HTTPS itself. With one, it accepts plain HTTP only from the listed proxy addresses, refuses a request the proxy did not mark `X-Forwarded-Proto: https`, and reads the client address from `X-Forwarded-For` only on those requests |
| A login (an owner account, set with `shepherd account set`) | Browser sessions and device tokens come from the login, not the operator token |

The daemon refuses to start with a non-loopback listener unless the other two are present.
Without `daemon.public`, nothing changes: loopback only, as now. The original loopback
listener stays up beside the public one, for the CLI on the same machine and for the
operator token.

**A controller on a laptop.** Nothing in the plan needs the controller on a server. Its one
need is that workers can reach it: a public address, a VPN (Tailscale or similar) whose
address the workers can dial, or a tunnel. A laptop that sleeps is a controller that is
away; workers carry on alone and catch up when it wakes (see
[Failure modes](../cluster.md#failure-modes)).

## Login

| Option | For | Against |
|---|---|---|
| **Built-in owner account: password and TOTP** | No identity provider to run; the CLI logs in the same way; TOTP is HMAC from the standard library; one small, well-known dependency for password hashing (`golang.org/x/crypto`, argon2id) | Shepherd owns a password store and its rate limiting |
| Built-in passkeys (WebAuthn) | Phishing-resistant, no password | Needs a WebAuthn library (CBOR, COSE, attestation); the CLI still needs another way in |
| OIDC behind a proxy (oauth2-proxy) | Shepherd writes no login code; reuses an existing identity provider | Needs an identity provider; the daemon would trust a header set by the proxy, one misrouted request from a bypass; the CLI and workers need a non-browser path anyway |

**Decided: a built-in owner account with a password and TOTP.** The second factor is a
`SecondFactor` interface in the daemon (enrol, verify, list, remove), with TOTP its first
implementation, so WebAuthn and passkeys can be added later without changing the login
flow. A proxy in front (oauth2-proxy, an address allow list) is fine as defence in depth,
but the daemon never relies on it.

- **Setup.** `shepherd account set` (operator token, so only on the daemon's machine) sets
  the password and prints, once, the TOTP enrolment URI and ten recovery codes. The store
  keeps the argon2id hash of the password and of each recovery code; a recovery code works
  once in place of a TOTP code. The TOTP secret is encrypted at rest with a key kept in a
  separate file, chosen at start by `internal/daemon/account.go`:
  - If `daemon.public.totp_key_file` names a file (synced from uBixVault, mode 0600), that
    key is used, and a missing, unreadable or too-open file stops the start with a plain
    error.
  - Otherwise the daemon uses `totp.key` in the Shepherd home (`~/.shepherd/totp.key` by
    default), generating it with mode 0600 on first need, and logs a warning at every start
    that the key sits beside the store, with one feed item the first time.

  The threat it mitigates is a copy of the store without the key: a leaked or restored
  backup of the database, a store file copied off the machine. With the vault file the key
  is never in the home, so a copy of the whole home does not hold it either. With the
  fallback, a reader of the whole home has both, which the warning and the docs say plainly.
- **Logging in from a browser.** `POST /v1/web/login` with password and code, then the
  existing session path: a random cookie kept as a hash, now `Secure` with the `__Host-`
  prefix, the CSRF header on every write, the Origin check against the public URL. The login
  route sends `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`. A TOTP code
  cannot be used twice.
- **Rate limiting.** Per account and per client address, both. The client address is the
  connection's, or the first untrusted address in `X-Forwarded-For` when the connection is
  from a trusted proxy; the header is ignored from anyone else. Repeated failure locks the
  account for a while and puts one item in the feed.
- **Logging in from the CLI.** `shepherd login https://shepherd.example.com` asks for the
  password and code and gets a **device token**: named, kept only as a hash, valid for 30
  days on a sliding window (each use pushes the expiry out), and never past 90 days from the
  login that made it, after which a fresh login is required however often it was used. It is stored in the client's
  home, never in `hosts.yaml`. A revoked or expired token gets one message, "log in again:
  shepherd login https://shepherd.example.com", which says nothing of the token's age.
- **Revocation.** Logging out ends one session; "sign out everywhere" ends every session
  and device token; changing the password or the second factor does the same. All are rows
  in the store, so revocation survives a restart. Settings lists device tokens and workers
  and revokes each.
- **Roles and their tables.** Browser sessions stay in `web_sessions`; device tokens get
  their own `device_tokens` table and worker credentials (phase 1) a separate
  `worker_credentials` table, so no lookup can confuse one for another. Sessions and device
  tokens have the `web` role: the person's powers over the conversation, decisions,
  approvals and (with confirmation, as today) settings, never the operator's setup routes or
  token minting. Worker credentials have a new `link` role (phase 1). Agent worker tokens (the ones minted for agents Shepherd starts) and desk tokens
  are unchanged: minted in memory for agents and the desk, never sent off the machine.
- **Crook's person's turn.** A desk turn is the person's when it comes from a session or a
  device token, exactly as a web turn is today. A `[Shepherd]` wake-up is not, so the desk
  still cannot answer a decision in it.
- **The operator token.** It works only on the loopback listener. On the public listener it
  gets `404 Not Found`, the same answer as a route that does not exist, so the public side
  does not confirm what it is.

Never exposed on the public listener, in logs, in the feed or in the web UI: the operator
token, worker and desk tokens, password and recovery-code hashes, the TOTP secret and its
key, agent CLI credentials, forge tokens, git push keys. Run logs and artifacts are already
redacted (`internal/redact`); a public listener raises the stakes, so phase 0a tests it.

## Deployment examples

Each example runs the same binary with the same three settings. The repo ships them under
`deploy/` with a page describing them; a person applies them.

| Example | Shape |
|---|---|
| A single host (systemd or launchd) | The release binary as a service, which `internal/service` already registers; TLS from the daemon's own certificate files, or a local reverse proxy (Caddy, nginx) as the trusted proxy. The home and workspace are ordinary directories |
| Docker Compose | One container from the image, two volumes (home, workspace), a reverse-proxy container in front holding the certificate, credentials mounted read-only as files |
| Kubernetes | Everything in one namespace, `shepherd`, with no per-environment namespaces. A StatefulSet with one replica and a ReadWriteOnce volume claim, a Service, an Ingress with TLS, a NetworkPolicy limiting egress to the forges, model providers and DNS, and Secret templates with no values that point at an external-secrets operator or Vault |

Common to all of them:

| Concern | Design |
|---|---|
| State | One home (`config.yaml`, the store, `runs/` logs, artifacts) and one workspace (repo clones and every lane's worktree) on persistent storage. Unpushed lane work lives only here |
| One process | Exactly one daemon per home: the home lock refuses a second. More machines are the cluster's job, not scaling |
| Credentials | Files with 0600 permissions, owned by the daemon's user, never environment variables (which agents' processes would inherit and could print). That covers the Claude, OpenCode and Copilot credentials, the `glab` and `gh` configs, the git push key and its known hosts, the TOTP key. Their source of truth is uBixVault, synced in by whatever the machine already uses; Kubernetes Secrets mounted as files are one form of this. None is written into config or a repo. An agent CLI that logs in interactively is logged in once on the machine (or with `kubectl exec`), its state kept in the home |
| Probes | An unauthenticated `GET /v1/health` that says only "up" |
| Shutdown | A stop that waits long enough for the existing drain (`Draining`), so a restart lets runs finish or records them interrupted (`TimeoutStopSec` under systemd, `stop_grace_period` in Compose, `terminationGracePeriodSeconds` in Kubernetes) |
| Backups | Crash-consistent only. Order: first a SQLite online backup of the store into the home (never a file copy of a live database), then a snapshot of the filesystem or volume holding the home and workspace, so the snapshot contains a clean store copy. In Kubernetes that is a backup job, then a VolumeSnapshot. A worktree caught mid-write may be torn, so a restore runs `git fsck` on every worktree and lists the lanes that fail it. Repo clones can be cloned again; unpushed lanes and the store cannot |
| Deploys | Human: the person installs or bumps the version |

## Done when

Phase 0 is three merge requests, each reviewable alone.

**0a. Hosted mode and login.**
Scope: `internal/config` (`daemon.public`), `internal/daemon/web.go`, `internal/daemon/auth.go`,
a new `internal/daemon/account.go` (with the `SecondFactor` interface), `internal/store` and
`internal/store/sqlite` (an appended migration for the account, recovery codes and
`device_tokens`), `internal/api/web.go`, `internal/cli` (`shepherd account`), `web/src` (a
login page and the device list in Settings), `go.mod` (`golang.org/x/crypto`),
`docs/hosted.md`, `docs/config.md`.

- Done when the daemon refuses a non-loopback listener without TLS or a trusted proxy and an
  account, and without `daemon.public` nothing changes.
- `daemon.public.url` is refused unless it is `https://` with no path other than `/`;
  `https://shepherd.example.com` and `https://shepherd.example.com/` load as the same URL,
  and a request with either Host form passes the Host check.
- A browser logs in with password and TOTP, and with a recovery code once; a wrong code, a
  reused code and a burst of attempts per account and per address are refused; an
  `X-Forwarded-For` from an untrusted address is ignored.
- The operator token gets 404 on the public listener and works on loopback; the login route
  sends `nosniff` and `no-store`.
- Logout and "sign out everywhere" end sessions and device tokens across a restart.
- A fake agent writes a known secret into its output, and the test asserts it appears on no
  public route, in no feed item and in no daemon log line.
- The TOTP secret is not readable in the store without its key file. With
  `totp_key_file` set, that file is used and a missing or group-readable one stops the start;
  without it, `totp.key` is created in the home with mode 0600 and the start logs the
  fallback warning.
- A device token used every day still stops working 90 days after its login and gets the
  "log in again" message; one left unused stops after 30. Both themes checked.

**0b. The CLI over HTTPS.**
Status: built. `Run` opens the public listener (and restarts it when a reload changes
`daemon.public`), and `shepherd login`, `shepherd logout` and `--host https://...` work with a
device token kept in a 0600 file. Two parts are left for a follow-up: a `url` field for a
named host in `hosts.yaml` (the HTTPS client lives in `internal/client`, not a new
`internal/remote` transport, so only a literal address is accepted), and `chat` over https,
which stays refused until the desk moves into the daemon; `status` and `decision answer` work.
`docs/cli.md` is not updated yet. The `login` and `logout` code is written and tested but not
registered as commands; a follow-up adds the two entries.
Scope: `internal/remote` (an HTTPS `Transport` beside `SSH`), `internal/cli/host.go`
(`--host https://...`, a `url` field in `hosts.yaml`, normalised as `daemon.public.url` is), `internal/cli` (`shepherd login`,
`shepherd logout`), `internal/client`, `docs/cli.md`, `docs/hosted.md`.

- Done when `shepherd login` then `shepherd --host https://... status`, `chat` and
  `decision answer` work against a test daemon behind a TLS test server.
- `https://shepherd.example.com` and `https://shepherd.example.com/` in `hosts.yaml` name the
  same host.
- The device token is in the client's home and never in `hosts.yaml`, output or logs; using
  it pushes its expiry out.
- A revoked or expired token gives exactly "log in again: shepherd login <url>".

**0c. The image and the deployment examples.**
Scope: `Dockerfile`, `deploy/` (a systemd unit and launchd note, a Compose file, Kubernetes
manifests), `.github/workflows/release.yml` (build and sign the image for both registries),
`docs/hosted.md`.

- Done when the image runs as a non-root user under tini as PID 1, ships git 2.31 or newer,
  `glab`, `gh` and `ssh`, builds in CI for a release, starts with an empty volume and answers
  its health check.
- A fake-agent lane runs end to end in a local Kubernetes (kind) and under Compose, and
  survives a restart with its store and lanes intact.
- The Kubernetes Secrets are templates with no values, referencing an external-secrets
  operator or Vault.
- The backup order and the restore with `git fsck` have been run once by hand on one example.
