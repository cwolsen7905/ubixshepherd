# A cluster of daemons behind one conversation: design

Status: proposed, for the maintainer's approval. Nothing here is built, and no code changes
with this document. The maintainer answered the first round of open questions on 2026-11-05
(see [Decided](#decided-on-2026-11-05)).

## Goal

The person runs a Shepherd daemon on more than one machine and talks to all of them through
one front desk: one thread, one Waiting on you, one feed of `[Shepherd]` events. Each
daemon keeps doing what it does today for the repos on its machine (lanes, runs, leases,
forge polling, gates); the conversation is what is shared.

The first step is smaller: **one daemon reachable over HTTPS with a real login** for the
web UI, the desk and the CLI (phase 0). Where it runs is the person's choice: a laptop, a
home server, a VPS, a container host or a Kubernetes cluster. The examples below use the
name `shepherd.example.com`. Connecting other daemons to it comes after.

## Non-goals

- Moving a run from one machine to another, or scheduling work onto whichever machine is
  free. A repo's work happens where the repo is.
- A shared or replicated store. Each daemon's store stays the only truth for its work.
- Changing the default. A daemon listens on loopback unless it is configured otherwise, and
  it listens beyond loopback only with TLS and a login.
- Several people sharing one daemon or cluster. One person (one account), their machines.
- A desk on every machine reacting to every event. There is one desk for the cluster.
- Requiring any platform. Kubernetes, Docker and systemd appear only as examples.
- Shepherd deploying itself. The doc describes deployments; a person applies them.

## Use cases

| Case | Machines | What the person wants |
|---|---|---|
| A reachable daemon | One daemon on an always-on machine (a home server, a VPS, a Kubernetes cluster) | Open `https://shepherd.example.com` from any browser or phone, log in, talk to the desk and answer decisions; `shepherd --host https://shepherd.example.com status` from a laptop |
| Laptop and build box | A laptop daemon for everyday repos; a build box with UbixOS, whose builds need its toolchain and disk | Answer the build box's decisions in one thread, start a run there without opening an ssh session |
| A daemon per machine, different repos | A work machine for client repos, a home server for uBixCore and its packs | One Waiting on you across both; one place to see spend |

## What exists today

| Piece | Where | What it gives |
|---|---|---|
| Loopback-only daemon | `daemon.listen` must be a loopback address (`internal/config`); every request's Host must be loopback (`allowedHost`, `internal/daemon/web.go`) | No listener to secure; also no way to reach a daemon except through ssh |
| Browser sessions | `internal/daemon/web.go`, `web_sessions` table | The CLI trades the operator token for a one-minute, single-use sign-in code; the code for an HttpOnly, SameSite=Strict cookie kept only as a hash, 12 hours, revocable; a CSRF header on writes; Origin and Sec-Fetch-Site checks; framing refused. The session has the web role: the desk's powers, the conversation and the person's answers, not setup or sign-in minting |
| Tokens and roles | `internal/daemon/auth.go` | Operator token in the runtime file; worker and desk tokens minted in memory, revoked by a restart. Crook sees a desk token as the person's only in a turn the person started |
| Remote CLI | `--host`, `SHEPHERD_HOST`, `~/.shepherd/hosts.yaml` (`internal/cli/host.go`) | Any command can be aimed at another machine's daemon |
| SSH transport | `internal/remote` (`Transport`, `SSH`, `Endpoint`) | Reads the remote runtime file over ssh, opens a `-L` tunnel, redials when the daemon moves. `Transport` is the seam for a second transport |
| Remote URL matching | `workspace.NormalizeRemote` (`internal/workspace/scan.go`) | Reduces ssh, scp-like and https forms of a remote to `host/path`, lower case, without port, user or `.git`; the scan already uses it to spot two clones of one repo |
| The desk | `internal/desk`, `internal/daemon/desk.go` | One Claude Code session per workspace, woken by feed items, with operator tools from `shepherd mcp` |
| The feed and its hub | `feed` table, `internal/daemon/feedhub.go`, `/v1/feed`, `/v1/feed/stream` | A monotonic id per item, polled every 500 ms to wake the desk and the streams |
| Desk thread, Waiting on you | `desk_events`, `/v1/desk/history`; `GET /v1/waiting` (`internal/waiting`), the web Inbox | The thread is stored per workspace; Waiting on you is derived on each read |
| Topics, questions, next-up, keep-going, spend | `topics`, `desk_questions`, `next_items`, `keep_going`, `spend` tables; `daemon.budget` | Per workspace or per daemon, keyed by local integer ids |

The browser session machinery is most of a login already: what is missing is a way to get a
session without the operator token, and a listener that is safe beyond loopback.

## Phase 0: a reachable daemon with a login

### Hosted mode

Hosted mode is not a kind of daemon. It is three settings, all opt-in, on any daemon:

| Setting | What it does |
|---|---|
| A non-loopback listener (`daemon.public.listen`, with `daemon.public.url` naming the public address) | The daemon accepts connections beyond loopback and accepts the public name in the Host check |
| TLS: a certificate and key (`daemon.public.tls`), or a TLS-terminating proxy (`daemon.public.trusted_proxies`) | Without a proxy the daemon serves HTTPS itself. With one, it accepts plain HTTP only from the listed proxy addresses, refuses a request the proxy did not mark `X-Forwarded-Proto: https`, and reads the client address from `X-Forwarded-For` only on those requests |
| A login (an owner account, set with `shepherd account set`) | Browser sessions and device tokens come from the login, not the operator token |

The daemon refuses to start with a non-loopback listener unless the other two are present.
Without `daemon.public`, nothing changes: loopback only, as now. The original loopback
listener stays up beside the public one, for the CLI on the same machine and for the
operator token.

**A hub on a laptop.** Nothing in the plan needs the hub on a server. Its one need is that
members can reach it: a public address, a VPN (Tailscale or similar) whose address the
members can dial, or a tunnel. A laptop that sleeps is a hub that is away; members carry on
alone and catch up when it wakes (see [Failure modes](#failure-modes)).

### Login

| Option | For | Against |
|---|---|---|
| **Built-in owner account: password and TOTP** | No identity provider to run; the CLI logs in the same way; TOTP is HMAC from the standard library; one small, well-known dependency for password hashing (`golang.org/x/crypto`, argon2id) | Shepherd owns a password store and its rate limiting |
| Built-in passkeys (WebAuthn) | Phishing-resistant, no password | Needs a WebAuthn library (CBOR, COSE, attestation); the CLI still needs another way in |
| OIDC behind a proxy (oauth2-proxy) | Shepherd writes no login code; reuses an existing identity provider | Needs an identity provider; the daemon would trust a header set by the proxy, one misrouted request from a bypass; the CLI and members need a non-browser path anyway |

**Decided: a built-in owner account with a password and TOTP.** The second factor is a
`SecondFactor` interface in the daemon (enrol, verify, list, remove), with TOTP its first
implementation, so WebAuthn and passkeys can be added later without changing the login
flow. A proxy in front (oauth2-proxy, an address allow list) is fine as defence in depth,
but the daemon never relies on it.

- **Setup.** `shepherd account set` (operator token, so only on the daemon's machine) sets
  the password and prints, once, the TOTP enrolment URI and ten recovery codes. The store
  keeps the argon2id hash of the password and of each recovery code; a recovery code works
  once in place of a TOTP code. The TOTP secret is encrypted at rest with a key kept in a
  separate file (0600, outside the store, sourced like other credentials). That protects a
  leaked or backed-up store on its own; it does not protect a machine whose whole home is
  read, which is said in the docs rather than implied away.
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
  days on a sliding window (each use pushes the expiry out). It is stored in the client's
  home, never in `hosts.yaml`. A revoked or expired token gets one message, "log in again:
  shepherd login https://shepherd.example.com", which says nothing of the token's age.
- **Revocation.** Logging out ends one session; "sign out everywhere" ends every session
  and device token; changing the password or the second factor does the same. All are rows
  in the store, so revocation survives a restart. Settings lists device tokens and members
  and revokes each.
- **Roles and their tables.** Browser sessions stay in `web_sessions`; device tokens get
  their own `device_tokens` table and member credentials (phase 1) a separate
  `member_credentials` table, so no lookup can confuse one for another. Sessions and device
  tokens have the `web` role: the person's powers over the conversation, decisions,
  approvals and (with confirmation, as today) settings, never the operator's setup routes or
  token minting. Member credentials have a new `link` role (phase 1). Worker and desk tokens
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

### Deployment examples

Each example runs the same binary with the same three settings. The repo ships them under
`deploy/` with a page describing them; a person applies them.

| Example | Shape |
|---|---|
| A single host (systemd or launchd) | The release binary as a service, which `internal/service` already registers; TLS from the daemon's own certificate files, or a local reverse proxy (Caddy, nginx) as the trusted proxy. The home and workspace are ordinary directories |
| Docker Compose | One container from the image, two volumes (home, workspace), a reverse-proxy container in front holding the certificate, credentials mounted read-only as files |
| Kubernetes | A StatefulSet with one replica and a ReadWriteOnce volume claim, a Service, an Ingress with TLS, a NetworkPolicy limiting egress to the forges, model providers and DNS, and Secret templates with no values that point at an external-secrets operator or Vault |

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

### Done when

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
- A browser logs in with password and TOTP, and with a recovery code once; a wrong code, a
  reused code and a burst of attempts per account and per address are refused; an
  `X-Forwarded-For` from an untrusted address is ignored.
- The operator token gets 404 on the public listener and works on loopback; the login route
  sends `nosniff` and `no-store`.
- Logout and "sign out everywhere" end sessions and device tokens across a restart.
- A fake agent writes a known secret into its output, and the test asserts it appears on no
  public route, in no feed item and in no daemon log line.
- The TOTP secret is not readable in the store without its key file. Both themes checked.

**0b. The CLI over HTTPS.**
Scope: `internal/remote` (an HTTPS `Transport` beside `SSH`), `internal/cli/host.go`
(`--host https://...`, a `url` field in `hosts.yaml`), `internal/cli` (`shepherd login`,
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

## Topology

| Option | How it works | For | Against |
|---|---|---|---|
| **Hub and members** | One daemon (the hub) runs the desk and the person's clients, follows each member's feed and calls each member's API. Members are ordinary daemons | Reuses the HTTP API and phase 0's login; each store stays authoritative; a member works alone when the hub is away; one desk, one session, one spend line for it | The hub is a single place to talk from; if it is away, the person uses a member directly (as today) |
| Peer federation | Every daemon follows every other and runs its own desk over all of them | No special daemon | N desks react to each event and spend N times; conflicting actions; N-squared links; agreeing on one thread is a consensus problem |
| Shared store | All daemons write one database | One truth, no merging | SQLite over a network filesystem is unsafe, so it needs a database server; breaks "the daemon owns the store"; a partition stops every machine; worktrees and processes are still per machine |

**Recommendation: hub and members**, with the phase 0 daemon as the natural hub: reachable,
already behind a login. It keeps every write on the daemon that owns the work, and a
partition degrades to "the hub cannot see a member yet", never to two machines disagreeing
about a lane.

### The hub is a role, not a program

**Decided: the hub is the same `shepherd` daemon with a `cluster:` section in its config.**
Any daemon can be one; a member is a daemon whose config names a hub to link to.
Everything the hub does is already in the daemon: the store for cursors, the desk, the feed
hub that wakes it, the login, the web UI, the API every client speaks. A separate program
would copy those or proxy every call to a daemon beside it, and the person would run,
upgrade and secure two things instead of one. It also lets the hub run its own lanes.

A split would make sense only if the hub stopped being one person's: many people sharing
it, or so many members that following their feeds needed its own scaling. Both are outside
this plan; the HTTP API between hub and members is the boundary to cut along if they come.

## Identity and naming

Each daemon gets a **daemon id** (a random UUID written to its home on first start, shown in
`GET /v1/status`) and the hub knows each member by a **member name**, short and readable
(`build`, `laptop`), given when the member is enrolled. The hub refuses a member whose id has
changed (a reinstall, or a name now used by another machine) until the person confirms it.

Everything a member owns is addressed as `<member>:<thing>`:

| Thing | Local form (unchanged) | From the hub |
|---|---|---|
| Lane | `fix-crash`, `repo/fix-crash` | `build:fix-crash`, `build:kernel/fix-crash` |
| Run | `run 42` | `build:run 42` |
| Decision, request, question | `decision 7` | `build:decision 7` |
| Next-up item | `next 12` | `build:next 12` |
| Feed item | id 900 | (`build`, 900), shown as an event from `build` |

The hub's own items keep their bare form, so a person with one daemon sees no change. Topics
belong to the thread, so they live on the hub only; a topic link to a member's item stores
the daemon id, the kind and the member's id, and shows the member name.

Desk tools that take a lane, run or decision accept the prefixed form, and gain an optional
`host` argument for tools that take no id (`lane_list`, `run_list`, `waiting`), mirroring
the CLI's `--host`. No `host` means the hub's own daemon, or, for reads, every member, each
row marked with its member.

## Transport and auth

There are two ways between a hub and a member. Both carry the same HTTP API.

| Transport | Who dials | When | Auth |
|---|---|---|---|
| **HTTPS link** (primary, phase 1) | The member dials out to the hub's public URL and holds the connection open | A reachable hub (phase 0) with members behind NAT, on laptops and home machines, where the hub cannot reach them | A member credential from enrolment |
| SSH tunnel (phase 7) | The hub dials the member with `remote.SSH`, as `--host` does | A hub that can ssh to the member | The member's runtime file, read over ssh |

### Enrolment

The person runs `shepherd member add <name>` on the hub, or uses Settings there. It shows a
one-time enrolment code of eight words from a wordlist embedded in the binary, single use,
valid for five minutes. On the member, `shepherd cluster join https://shepherd.example.com
<code>` trades it for a **member credential**: kept as a hash in the hub's
`member_credentials` table, named, revocable from Settings, stored on the member in its
home and never in config. Its role is `link`: it can open the link and push that member's
own feed and status, and nothing else. Every acting route (decision answer, lane open, run
start and stop, next-up, settings) answers it with 403.

### The link

| Part | Design |
|---|---|
| Opening | The member opens one long-lived connection to `/v1/cluster/link` with its credential, and the connection is upgraded to a framed, bidirectional stream (the carrier is an open question below) |
| Handshake | Both sides send their daemon id, version, `api_level` and capabilities (`read_lanes`, `push_feed`, `act`, and later ones). The hub uses only what a member reports and says what is off for it ("build is on 0.9: answering decisions from here needs `act`") |
| Frames | Length-prefixed JSON frames. Feed batches and status go up; the hub's calls go down as RPCs with a request id, each answered by the member through its own local API. Many RPCs share the connection at once, matched by id, each with its own deadline |
| Keepalive | A ping every 20 seconds each way, under the idle timeouts of common proxies and load balancers; two missed pongs close the link |
| Reconnect | Full-jitter exponential backoff: a random wait between zero and the current step, doubling from one second, capped at 60 seconds. A `Retry-After` from the hub (while it drains or rate limits) is honoured as a floor |
| Resume | The member resends from the hub's cursor, which the handshake returns |

**The carrier.** Go's standard library has no WebSocket server, and the repo keeps
dependencies few.

| Carrier | Dependency | Through proxies | Notes |
|---|---|---|---|
| WebSocket | One module (`github.com/coder/websocket`, no further dependencies) | Passed by proxies, ingresses and CDNs that support WebSocket, which is the form they most often support | The handshake is `GET` with `Upgrade: websocket`; ping and pong frames are built in |
| A custom HTTP upgrade (`Upgrade: shepherd-link/1`) | None: `http.Hijacker` on the hub, and the client's 101 response body is a read-write stream | Passed by a proxy that forwards any `Upgrade` header; refused by one that forwards only WebSocket | Can be on `POST`; our own framing and pings |
| HTTP/2 full-duplex streaming | None | Needs HTTP/2 end to end; a proxy that speaks HTTP/1.1 to the backend, or buffers bodies, breaks it | No upgrade at all |
| Two streams (a long `GET` down, short `POST`s up) | None | Works through any proxy that streams responses | More requests; not one connection to watch |

**Recommendation: WebSocket**, behind a small carrier interface so the framing above does
not depend on it. The link must cross proxies the person may not control, and WebSocket is
the upgrade they are most likely to pass; one dependency with none of its own is a fair
price. If the person prefers no dependency, the custom upgrade is the fallback, and a member
behind a proxy that refuses it uses the SSH transport.

### Trust

**What the SSH transport costs.** Over ssh the hub reads the member's runtime file, which
holds the operator token, so the hub can do anything the person can on that member. That is
the trust `--host` already has; acceptable for reads, too much for actions.

**Actions, either way.** A call from the hub that acts (answer, start, stop) carries the
turn's facts: desk or person, whether a person started the turn, the turn id. The member
mints a local desk token with those facts for that one call, so Crook on the member records
the right actor, and the member's own role checks still apply. The member trusts the hub's
claim that a person started the turn, which is why the hub's login is the person's. A stolen
member credential cannot make that claim: it opens a link to the hub, it is not the hub.
Over ssh, the operator token is used only to mint that desk token, never to act.

## The one conversation

| Surface | How it merges |
|---|---|
| Thread | Stays on the hub: one desk session, one `desk_events` history. Member events enter it as `[Shepherd]` events that name their member ("on build: run 42 failed its gate") |
| `[Shepherd]` events | The hub keeps a cursor per member (last feed id, in a new hub table). The member sends from that cursor over the link; over ssh the hub reads from it. The items reach the hub's feed hub tagged with the member, and the desk wakes on them by the same rules as local items |
| Waiting on you and the Inbox | `GET /v1/waiting` on the hub returns its own items plus each member's, each with a `member` field. Counts are per member and in total; zero stays visible and faint |
| Topics, keep-going | The hub's, since they belong to the conversation |
| A member's own desk | Still there for someone at that machine, but it does not wake for swarm events while a hub holds its wake lease: held while the link is up (or the hub keeps reading over ssh), lapsed a few minutes after. Two desks never react to one event |

**Ordering.** The thread sorts by the time the hub received each item, so it never reorders
what the person has already seen. Within one member, that is also feed order (ids are
monotonic). Each member event shows the member's own time second, after the hub's. When a
member's clock is more than a minute off the hub's, the hub says so once, in the thread and
in the member's status; nothing is held or refused because of it.

**Dedup.** A merged item is keyed by (daemon id, feed id). A resent batch after a reconnect,
or two links racing during a redial, cannot add an event twice.

**A member offline.** The hub shows it in status, the thread gets one event when it goes away
and one when it is back (not one per retry), and the web footer names it. Its last Waiting on
you items stay listed, marked stale with the time they were read, and their actions are off.
Actions aimed at it are refused with a plain message, never queued: a queued "stop run 42"
that fires an hour later would surprise the person.

**Catching up.** On a reconnect with more than 10 missed items, the thread gets one summary
event ("build: 23 events while away: 3 runs ended, 1 decision") that expands to the items,
and the desk is woken once with the summary rather than per item. Ten or fewer arrive as
ordinary events.

**Answering.** A decision answered in the hub's thread or Inbox is sent to its member with
the person's words and the person-started turn facts. The member delivers it to the asking
run exactly as today (`Runner.Answer`). The hub never answers one on its own.

## Leases, spend, keep-going and merge rules

| Concern | Per daemon or cluster | Rule |
|---|---|---|
| Scope leases, tag reservations | Per daemon | A repo has one **home** daemon in the cluster, below. Opening a lane goes to the repo's home, so leases and reservations stay correct with no cross-machine locking |
| Spend caps | Per daemon, plus a cluster view | Each daemon's `daemon.budget` stays the hard cap it enforces itself. The hub adds a cluster total and, later, a soft cluster cap, below |
| Keep-going | The hub's | The mode is the hub's desk's. Its cap counts the cost of every run it started, on any member, tracked by (member, run id). Each member still applies its own limits and `Allow` gate to what the hub asks of it |
| Merge rules, autonomy | Per repo | Unchanged: read from the repo's base branch and evaluated by its home daemon. The hub shows "Merged by rule <name>" as the member reports it |
| `max_runs`, quota holds | Per daemon | They describe a machine's capacity and an agent CLI's account on that machine |

**Repo homes.** The hub keys repos by origin, compared with `workspace.NormalizeRemote`, the
function the workspace scan already uses, so `git@host:group/repo.git` and
`https://host/group/repo` are one repo. When two daemons register the same origin, the hub
follows neither copy for lane work until the person picks one:
`shepherd cluster set-home <origin> <member>` (also in Settings). A clone the person keeps
on purpose (a mirror for reading, a second checkout on the build box) is marked with
`shepherd cluster set-home <origin> <member> --also <other>`: the other copy stays visible
for reads and never takes lanes.

**Cluster spend.** The hub follows members' spend items in their feeds, and adds a
pending-cost estimate for each run still going (its cost so far, plus the agent's recent
average for a run of that kind), so several members starting runs at once do not overshoot
a soft cap between reports. A soft cap only warns; each daemon's own hard cap is what holds.

## Failure modes

| Failure | What happens |
|---|---|
| Partition between hub and member | The member carries on: runs finish, the watcher polls, decisions wait on it. Its desk takes the wake-ups once the hub's lease lapses. The hub marks it offline, keeps the stale view, refuses actions, and catches up from its cursor on return. The member redials with the backoff above |
| Member restarts mid-run | As today on that member: agents left by a crashed daemon are stopped and their runs recorded interrupted. It reopens the link (or the hub redials over ssh) and resends from the cursor; the interrupted runs reach the thread as ordinary events |
| The hub restarts or sleeps | The drain lets its own runs finish, or records them interrupted. Cursors, sessions, device tokens and member credentials are in the store, so the person stays logged in and members' links resume where they stopped |
| Hub's public name or certificate fails | Members cannot link and the person cannot log in from outside; every daemon still works locally, and the hub's loopback listener is still reachable on its machine |
| Member credential stolen | It can only open a link as that member and send feed items under its name; every acting route gives 403. The person revokes it from Settings and enrols the member again |
| Member reinstalled or renamed | The daemon id changes; the hub stops following it and asks the person to confirm, so items from two stores never share a name |
| Feed pruned on a member before the hub caught up | The hub sees a gap between its cursor and the oldest item, says so once in the thread, and resumes from the oldest |
| Version skew | The handshake's `api_level` and capabilities decide what the hub uses, per member; an old member never fails a whole read |
| Two hubs configured by mistake | A member links to one hub; the wake lease names its holder, a second hub over ssh is told another hub holds it, and the person is warned |
| Restore from backup | Crash-consistent: the store copy is clean, a worktree may not be; `git fsck` lists the lanes to look at |

## Phased plan

Each phase is one merge request, and each is useful alone. Phase 0 (0a, 0b, 0c) is above.

### 1. A member linked to the hub, status only

Scope: `internal/config` (`cluster:` on the hub, `cluster.hub` on a member), `internal/daemon`
(a `cluster.go` for both ends of the link; daemon id in the home and in status; enrolment;
the `link` role), `internal/store/sqlite` (an appended migration for members and
`member_credentials`), `internal/api` (`GET /v1/cluster`, `/v1/cluster/link`, enrolment),
`internal/client`, `internal/cli` (`shepherd member add`, `shepherd cluster join`,
`shepherd cluster status`, the `cluster_status` MCP tool), an embedded wordlist,
`web/src` (members in Settings), `go.mod` (if WebSocket is chosen), `docs/cluster.md`,
`docs/config.md`.

- Done when an eight-word code enrols a member once and is refused after use or after five
  minutes.
- The member holds a link to a test hub over TLS; the handshake records its `api_level` and
  capabilities; pings flow every 20 seconds; a dropped link redials with jittered backoff
  capped at 60 seconds and honours `Retry-After`.
- `GET /v1/cluster` lists each member with online or not, version, `daemon_id`, workspaces,
  runs going and waiting counts; the desk answers "what is running on build" with that tool.
- A link credential gets 403 on decision answer, lane open and every other acting route, and
  a device token is never accepted as a link credential or the reverse.
- A revoked credential drops the link; a member whose id changed is refused until confirmed.

### 2. Read-only desk tools across members

Scope: `internal/cli/mcp.go` (a `host` argument and prefixed ids on `lane_list`, `run_list`,
`run_status`, `decision_list`, `question_list`, `next_list`, `lane_diff`), `internal/daemon`
(reads sent down the link as RPCs), `internal/desk/claude.go` (one brief line on member
names), `docs/cluster.md`.

- Done when the desk can list and read lanes, runs and decisions on a named member, several
  reads share the link at once, a read with no `host` covers every member with each row
  marked, and an offline member gives one plain line in the answer, not an error for the
  whole read.

### 3. Members' events in the thread

Scope: `internal/store/sqlite` (an appended migration for member cursors), `internal/daemon`
(`feedhub.go` takes members' items; the wake lease; the catch-up summary), `internal/api`
(member and member time on feed items), `internal/desk` (event text names the member),
`web/src/model/feed.ts`, the terminal chat's event rendering, `docs/cluster.md`.

- Done when a member's run outcome appears once in the hub's thread with its member name,
  sorted by hub receive time with the member's time shown second.
- The hub's desk wakes for it and the member's desk does not; the member's desk wakes again
  after the lease lapses.
- A hub restart or a dropped link neither loses nor repeats an item; a reconnect with more
  than 10 missed items posts one expandable summary event and wakes the desk once.
- A member clock more than a minute off gives one non-blocking warning.

### 4. One Waiting on you

Scope: `internal/waiting`, `internal/api/waiting.go`, `internal/daemon`, `web/src/pages/Inbox.tsx`,
`web/src/model/backlog.ts`, the terminal dock, `docs/waiting-on-you.md`, `docs/cluster.md`.

- Done when the Inbox and the dock list every member's items with a member chip, counts per
  member stay visible at zero, an offline member's items show stale with their time and no
  actions, and the footer names the member. Both themes checked.

### 5. Acting on a member

Scope: `internal/daemon/auth.go` (per-call desk tokens from turn facts), `internal/daemon`
(actions sent down the link, behind the `act` capability), `internal/cli/mcp.go` (`host` on
`lane_open`, `lane_run`, `run_continue`, `run_stop`, `decision_answer`, `question_answer`,
`next_add`, `request_close`), `web/src/pages/Inbox.tsx` (answer in place), `docs/cluster.md`.

- Done when a decision answered in the hub's thread reaches the asking run on its member;
  Crook on the member records the person as the actor only for a person-started turn and the
  desk otherwise; a member without `act` is told so; a member credential alone cannot make
  an action; an action on an offline member is refused, not queued.

### 6. Repo homes and cluster spend

Scope: `internal/daemon` (repo homes by `workspace.NormalizeRemote`; routing `lane_open` to
the home; following spend items and the pending-cost estimate), `internal/cli`
(`shepherd cluster set-home`), `internal/daemon/keepgoing.go` (counting members' runs),
`internal/config` (the soft cluster cap, off by default), `internal/api` (cluster spend), the
web spend view, `docs/keep-going.md`, `docs/cluster.md`.

- Done when the ssh and https forms of one origin count as one repo, the hub holds lane work
  for a repo registered on two daemons until `set-home` picks one, `--also` keeps an
  intended second clone readable and lane-free, and `lane_open` without `host` opens on the
  repo's home.
- The cluster's daily total, including the pending estimate for running runs, is shown
  view-only. The soft-cap warning ships off, and the person turns it on after a week of
  comparing the view with members' own totals.
- Keep-going stops at its cap counting runs it started on members.

### 7. Members over SSH

Scope: `internal/daemon/cluster.go` (the hub dialling with `remote.SSH` behind the same member
interface), `internal/config` (a member given as a `hosts.yaml` name), `docs/cluster.md`.

- Done when a member reached over ssh behaves in phases 1 to 6 exactly as a linked one, tested
  with the fake ssh `Runner`, and the operator token read over ssh is used only to mint
  per-call desk tokens.

## Decided on 2026-11-05

The maintainer agreed with every recommendation in the first round.

| # | Question | Decision |
|---|---|---|
| 1 | How the person logs in | Password and TOTP built in, behind a `SecondFactor` interface for passkeys later; a proxy in front only as defence in depth |
| 2 | Whether a hosted daemon runs agents itself | Yes: it is a full daemon; its egress and credentials decide what its agents can reach |
| 3 | How long a device token lives | 30 days on a sliding window, renewed by use, revocable from Settings |
| 4 | Is the hub a separate program | No: a config role of the same daemon |
| 5 | Members' transport | Both: the HTTPS link first (phase 1), SSH later (phase 7) |
| 6 | Does a member's own desk keep working | Yes, behind the wake lease |
| 7 | A cluster-wide hard spend cap | Soft first, after a view-only week; hard later only if needed |
| 8 | Hub's items unprefixed | Yes: bare for the hub's own, prefixed for members |
| 9 | Where the image is published | GitHub's container registry and the internal forge's registry, signed like the release archives |

## Open questions

| # | Question | Options | Recommendation |
|---|---|---|---|
| 1 | The link's carrier | WebSocket (`github.com/coder/websocket`) / a custom HTTP upgrade (standard library) / HTTP/2 streaming / two streams | WebSocket behind a carrier interface: it is the upgrade proxies most often pass, for one dependency with none of its own. The custom upgrade if no new dependency is wanted |
| 2 | The Kubernetes example's controller | A StatefulSet / a Deployment with the `Recreate` strategy | A StatefulSet: with one replica it already stops the old pod before starting the new one. `Recreate` is a Deployment strategy and has no StatefulSet equivalent |
| 3 | Where the TOTP key comes from | A key file generated in the home on `account set` / a file synced from uBixVault | A file from uBixVault on a hosted machine, so a copy of the home alone does not hold it; generated in the home when no vault is set up, with the docs saying what that protects |
