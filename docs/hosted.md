# Hosted mode: a daemon reachable over HTTPS with a login

Status: **implemented, with `chat` over https, `hosts.yaml` entries and the `login` and `logout` command registration still to come.** The
daemon opens the public listener at start, `shepherd login` and `--host https://...` work, and
the account, second factor, device tokens, rate limits, login routes, `shepherd account`, the
web sign-in page and the Settings device list are built and tested.

This page says what the finished feature does.

A hosted daemon is any daemon with three opt-in settings. Without `daemon.public` nothing
changes: it listens on loopback only, as before.

| Setting | What it does |
|---|---|
| `daemon.public.listen` and `daemon.public.url` | A second listener beyond loopback, and the public address people use |
| `daemon.public.tls`, or `daemon.public.trusted_proxies` | Either the daemon serves HTTPS itself from a certificate and key, or a proxy in front ends TLS and the daemon takes plain HTTP only from it |
| An owner account | Set with `shepherd account set`; browser sessions and device tokens come from this login |

The daemon refuses to start with a public listener unless TLS (or a trusted proxy) is
configured and the TOTP key can be read. An account cannot be made before the daemon runs,
so with no account yet the daemon starts on loopback only, says so in its log, and opens the
public listener the moment `shepherd account set` succeeds: nothing is reachable from
outside without a login to reach it with. The loopback listener stays up beside the public
one, for the CLI on the same machine and for the operator token.

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

## When the listener opens

`Run` checks `daemon.public` before it listens anywhere: a missing TLS or trusted-proxy
setting, or an unreadable key file, stops the start with the reason. With none configured
nothing changes. The loopback listener always comes up first. The public listener opens
only once an owner account exists, so with none the daemon says so in its log and
`shepherd account set` is what opens it.

A config reload (`shepherd daemon reload`, a SIGHUP, or saving settings) that changes
`daemon.public` closes the public listener (waiting up to five seconds for requests in
flight), prepares the new settings and opens it again, within about a second. A reload that
leaves it alone does not touch it. If the new settings cannot be prepared the listener stays
closed, and the log and the feed say why: it never falls back to the old settings. Removing
`daemon.public` closes it for good. A stop closes it with the daemon.

## The login

The owner account has a password (at least 12 characters, kept as an argon2id hash) and a
second factor. TOTP is the first; the code behind it is a `SecondFactor` (enrol, verify,
list, remove), so a passkey can be added later without changing the login.

`shepherd account set` (on the daemon's machine, with a password typed twice on a terminal or
one line from `--password-stdin`, never a flag) prints, once, the TOTP enrolment address and its typed secret, and
ten recovery codes. A recovery code works once in place of a TOTP code and is kept only as
a hash. A TOTP code cannot be used twice, and an earlier code than the last one used is
refused. Setting the password again, or changing the second factor, signs everything out.

| Way in | Route | Gives |
|---|---|---|
| Browser | `POST /v1/web/login`, from a page whose Origin is `daemon.public.url` | A session cookie named `__Host-shepherd_session` (Secure, HttpOnly, SameSite=Strict, Path `/`), and the CSRF token the page sends on every write |
| CLI | `POST /v1/device/login`, by `shepherd login` | A named device token, kept in a file in the client's home |

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

## Using the CLI against a hosted daemon

The login and logout code is built and tested, but the two commands are not registered in the
command table yet, so `shepherd login` is unknown until a follow-up lands. `--host https://...`
works once a token exists.

```
shepherd login https://shepherd.example.com        # or: shepherd --host https://shepherd.example.com login
shepherd --host https://shepherd.example.com status
shepherd --host https://shepherd.example.com decision answer 12 "go ahead"
shepherd logout https://shepherd.example.com
```

`shepherd login` asks for the password (not shown) and a code: an authenticator code, or one
recovery code. For a script, `--password-stdin` reads the password and then the code, one
line each. `--name` sets the name the device is listed under (the machine's name by default).
Neither secret is ever a flag or an environment variable.

| Thing | Rule |
|---|---|
| The token file | `tokens/<hash of the address>.json` in the client's Shepherd home, mode 0600 in a directory of mode 0700, written whole or not at all. It is never in `hosts.yaml`, the environment, output or a log |
| The address | Normalised as `daemon.public.url` is: with or without a trailing slash it is one host. `http://` is refused, and there is no way to skip certificate checks: the system's trust roots, TLS 1.2 or newer, and redirects are never followed |
| Expiry | Each use pushes the token's expiry out on the daemon, so the file never needs rewriting. It ends 30 days after its last use and 90 days after its login |
| A revoked, expired or missing token | Exactly one message: `log in again: shepherd login <address>` |
| `shepherd logout` | Asks the daemon to revoke the token, then deletes the file. If the daemon cannot be reached the file is still deleted, and the message says to revoke the token from Settings |
| What works | Every command that works over `--host` ssh except `run attach` and `session attach`. `shepherd chat` is refused for now: its front desk starts on the machine that runs the command, in a directory that exists only on the daemon's, so run it on the daemon's machine |

`--host` and `SHEPHERD_HOST` take a literal `https://` address. A `url` field for a named
entry in `hosts.yaml` is not built yet.

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

## Managing sign-ins

| Where | What |
|---|---|
| Settings, "Signed-in devices" | Lists each device token by name and times, revokes one, or signs out everywhere. Shown only on a hosted daemon |
| `shepherd account sign-out-all` | The same sign-out, from the daemon's machine |
| `GET /v1/devices`, `POST /v1/devices/{id}/revoke`, `POST /v1/web/signout-all` | The routes behind them, for a session or a device token |

A sign-in link (`shepherd web`) is not accepted on the public listener: sessions there come
only from the login. Worker and desk tokens are refused there too. A device token has the web
role, so it reads and answers like a session, but cannot save settings and never reaches the
operator's setup routes or sign-in minting.

## Health endpoint

`GET /v1/health` answers `200` with the two characters `up` (`text/plain`), on the
loopback listener and on the public listener. It needs no token, session or CSRF, and
it says nothing else: no version, no counts, no ids. Anything but GET or HEAD is
`405`. Like the login routes it sends `Cache-Control: no-store` (and `nosniff`, like
every response).

On the public listener it answers for the public URL's host and for a bare pod or IP
address, because Kubernetes probes hit the pod IP. Every other route still needs its
host and its login exactly as before.

| Probe | What it uses |
|---|---|
| Kubernetes readiness | `httpGet` on `/v1/health`, plain HTTP on port 8443: TLS ends at the Ingress, and the daemon takes plain HTTP from the proxy there. Liveness and startup stay `shepherd status`: the public listener opens only once an owner account exists, so readiness failing before then keeps the pod out of endpoints until there is a login to serve |
| Docker Compose | The shepherd service's `healthcheck` runs `shepherd status` in the container: the image ships no HTTP client, so a container-local check over loopback with the operator token is what it gets |
| `deploy/docker/check-image.sh` | Runs `shepherd status` inside the container: the daemon binds loopback only, which a published port cannot reach, and the image ships no HTTP client |

The image's own `HEALTHCHECK` stays `shepherd status`: it runs before any account
exists, and the public listener opens only once one does. If your cluster's
NetworkPolicy denies ingress by default, allow the kubelet to reach TCP 8443, or the
`httpGet` probes never arrive: the shipped policy opens that port to the ingress
controller only.

## What the tests check

Hosted mode's tests run a daemon with a fake agent that prints secrets, sign in over the
public handler, and read every route a signed-in person can, the feed and the daemon's log:
none of the daemon's tokens, the password, recovery codes, TOTP secret and key, their
hashes or the agent's secret appears. They also cover logout and sign-out-everywhere across a
restart, the 404 for the operator token, and a TLS listener started from a certificate.

## Deployment examples

`deploy/` holds three ways to run the same binary with the same three settings. You apply
them; Shepherd does not. Each keeps one home (`config.yaml`, the store, `runs/`, artifacts)
and one workspace (repo clones and every lane's worktree) on persistent storage. Unpushed lane
work lives only there.

| Example | Where | Shape |
|---|---|---|
| A single host | `deploy/systemd/shepherd.service`, `deploy/systemd/README-launchd.md` | The release binary as a system service under its own user, a stop timeout of two minutes. On a Mac, `shepherd daemon install` writes the launchd agent |
| Docker Compose | `deploy/compose/` | One container, a home volume and a workspace volume, a Caddy container in front holding the certificate, credentials bind-mounted read-only as files, `stop_grace_period: 2m` |
| Kubernetes | `deploy/kubernetes/` | One namespace, `shepherd`. A StatefulSet of one replica with a ReadWriteOnce claim, a headless Service, an Ingress with TLS, a NetworkPolicy, a Secret `shepherd-credentials` you create from files (or the optional `deploy/kubernetes/external-secrets` template, with no values, for the External Secrets Operator), and `terminationGracePeriodSeconds: 120`. Apply `deploy/kubernetes/namespace.yaml` and create the Secret first, then apply with `kubectl apply -k deploy/kubernetes` after editing the host name, image tag, ingress class, storage class and `SHEPHERD_TRUSTED_PROXIES` (the ingress controller's pod CIDR; the manifest ships a placeholder the image refuses, because every address listed may claim a request came over TLS) |

### The image

The `Dockerfile` builds the web UI and a static binary (no cgo), then a Debian 13 runtime
with git 2.47, `glab`, `gh`, `ssh`, `sqlite3` and tini. It runs as user 10001, with tini
as PID 1, so a stop reaches the daemon as SIGTERM and its drain runs. Releases publish it to
`ghcr.io/ubixsys/ubixshepherd` for `linux/amd64` and `linux/arm64`, signed keyless with
cosign: `cosign verify ghcr.io/ubixsys/ubixshepherd:<tag> --certificate-identity-regexp
'https://github.com/ubixsys/ubixshepherd/.github/workflows/release.yml@.*'
--certificate-oidc-issuer https://token.actions.githubusercontent.com`.
A repository variable, `IMAGE_MIRROR_REPOSITORY`, adds a second registry.

| Volume or path | Holds |
|---|---|
| `/data/home` | `SHEPHERD_HOME`: config, store, logs, artifacts |
| `/data/workspace` | The workspace: repo clones and worktrees |
| `/run/secrets/shepherd/` | Credentials as read-only files: `totp-key`, `ssh/id`, `ssh/known_hosts`, and seeds for `glab/`, `gh/` and `claude/` |
| `/data/home/cli/` | The `glab`, `gh` and agent CLI config directories. Seeded once from the seeds above, never overwritten, so a refreshed token or a login done with `exec` survives a restart; delete a file, and restart, to take a rotated seed |

On an empty home the entrypoint writes a `config.yaml` with `daemon.public` set from
`SHEPHERD_PUBLIC_URL` and `SHEPHERD_TRUSTED_PROXIES` (the reverse proxy's addresses); after
that the file is yours. The public listener opens once you run `shepherd account set` inside the
container (`docker compose exec shepherd shepherd account set`, or `kubectl -n shepherd exec -it
shepherd-0 -- shepherd account set`). An agent CLI that logs in interactively is logged in
once the same way, and its state kept in `/data/home/cli`.

The image's health check is `shepherd status`, which asks the daemon over loopback with the
operator token in the home. The public listener is not what it waits for, since that opens
only once an account exists.

### Concerns common to all three

| Concern | How the examples meet it |
|---|---|
| Credentials | Files of mode 0600 owned by the daemon's user, never environment variables, which agents would inherit. In Kubernetes the Secret is mounted into an init step, running as the daemon's user, that copies each key to an in-memory volume at 0600, because `fsGroup` (needed to write the volume without a root step) makes Secret files group-readable and the daemon refuses a group-readable TOTP key |
| One process | One replica, one home; the home lock refuses a second daemon |
| Shutdown | The daemon ends its agents on SIGTERM in about 20 seconds; the examples wait two minutes before SIGKILL |
| TLS | Compose: Caddy. Kubernetes: the Ingress, with the controller's pod addresses as `trusted_proxies`. Single host: the daemon's own certificate, or a local proxy |
| Egress (Kubernetes) | DNS, then ports 443 and 22 to addresses outside private ranges. A NetworkPolicy matches addresses, not names: pin it to your providers' ranges, or use a CNI that filters by name |
| Deploys | A person bumps the image tag. Nothing here updates itself |

### Backup and restore

Backups are crash-consistent. The order matters: first a SQLite online backup of the store
into the home (never a file copy of a live database), then a snapshot of the volume holding
the home and workspace, so the snapshot holds a clean store copy. `deploy/kubernetes/backup.yaml`
is a CronJob that does both: an init container runs `sqlite3 .backup`, checks the copy and
keeps the newest seven under `backups/` in the home, and only then a VolumeSnapshot is
created. On other hosts, run the same `sqlite3 "$SHEPHERD_HOME/shepherd.db" ".backup <file>"`
before your volume or filesystem snapshot.

A worktree caught mid-write by the snapshot may be torn. After a restore, with the daemon
stopped, put the backup in place and check every worktree before starting it:

```sh
rm -f shepherd.db shepherd.db-wal shepherd.db-shm
cp backups/shepherd-<time>.db shepherd.db
for d in /data/workspace/*-worktrees/*/; do
  git -C "$d" fsck --no-progress >/dev/null 2>&1 || echo "FAILED: $d"
done
```

A lane listed as failed needs its work recovered from the repository's objects or lost;
repo clones can simply be cloned again. Unpushed lanes and the store cannot.
