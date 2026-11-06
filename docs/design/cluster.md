# A cluster of daemons behind one conversation: design

Status: proposed, for the maintainer's approval. Nothing here is built, and no code changes
with this document.

## Goal

The person runs a Shepherd daemon on more than one machine and talks to all of them through
one front desk: one thread, one Waiting on you, one feed of `[Shepherd]` events. Each
daemon keeps doing what it does today for the repos on its machine (lanes, runs, leases,
forge polling, gates); the conversation is what is shared.

The first step is smaller: **one daemon hosted in the person's own Kubernetes cluster,
reached over HTTPS at a name they own (written `shepherd.example.com` below), with a real
login** for the web UI, the desk and the CLI (phase 0). Connecting other daemons to it comes
after.

## Non-goals

- Moving a run from one machine to another, or scheduling work onto whichever machine is
  free. A repo's work happens where the repo is.
- A shared or replicated store. Each daemon's store stays the only truth for its work.
- Opening every daemon to the network. Only a daemon the person deliberately hosts, behind
  TLS and a login, listens beyond loopback; a laptop daemon stays loopback-only.
- Several people sharing one daemon or cluster. One person (one account), their machines.
- A desk on every machine reacting to every event. There is one desk for the cluster.
- Shepherd deploying itself. The doc describes a deployment; a person applies it.

## Use cases

| Case | Machines | What the person wants |
|---|---|---|
| A hosted daemon | One daemon in the person's Kubernetes cluster, always on | Open `https://shepherd.example.com` from any browser or phone, log in, talk to the desk and answer decisions; `shepherd --host https://shepherd.example.com status` from a laptop |
| Laptop and build box | A laptop daemon for everyday repos; a build box with UbixOS, whose builds need its toolchain and disk | Answer the build box's decisions in one thread, start a run there without opening an ssh session |
| A daemon per machine, different repos | A work machine for client repos, a home server for uBixCore and its packs | One Waiting on you across both; one place to see spend |

## What exists today

| Piece | Where | What it gives |
|---|---|---|
| Loopback-only daemon | `daemon.listen` must be a loopback address (`internal/config`); every request's Host must be loopback (`allowedHost`, `internal/daemon/web.go`) | No listener to secure; also no way to host a daemon |
| Browser sessions | `internal/daemon/web.go`, `web_sessions` table | The CLI trades the operator token for a one-minute, single-use sign-in code; the code for an HttpOnly, SameSite=Strict cookie kept only as a hash, 12 hours, revocable; a CSRF header on writes; Origin and Sec-Fetch-Site checks; framing refused. The session has the web role: the desk's powers, the conversation and the person's answers, not setup or sign-in minting |
| Tokens and roles | `internal/daemon/auth.go` | Operator token in the runtime file; worker and desk tokens minted in memory, revoked by a restart. Crook sees a desk token as the person's only in a turn the person started |
| Remote CLI | `--host`, `SHEPHERD_HOST`, `~/.shepherd/hosts.yaml` (`internal/cli/host.go`) | Any command can be aimed at another machine's daemon |
| SSH transport | `internal/remote` (`Transport`, `SSH`, `Endpoint`) | Reads the remote runtime file over ssh, opens a `-L` tunnel, redials when the daemon moves. `Transport` is the seam for a second transport |
| The desk | `internal/desk`, `internal/daemon/desk.go` | One Claude Code session per workspace, woken by feed items, with operator tools from `shepherd mcp` |
| The feed and its hub | `feed` table, `internal/daemon/feedhub.go`, `/v1/feed`, `/v1/feed/stream` | A monotonic id per item, polled every 500 ms to wake the desk and the streams |
| Desk thread, Waiting on you | `desk_events`, `/v1/desk/history`; `GET /v1/waiting` (`internal/waiting`), the web Inbox | The thread is stored per workspace; Waiting on you is derived on each read |
| Topics, questions, next-up, keep-going, spend | `topics`, `desk_questions`, `next_items`, `keep_going`, `spend` tables; `daemon.budget` | Per workspace or per daemon, keyed by local integer ids |

The browser session machinery is most of a login already: what is missing is a way to get a
session without the operator token, and a listener that is safe on the internet.

## Phase 0: a hosted daemon with a login

### Login

| Option | For | Against |
|---|---|---|
| **Built-in owner account: password and TOTP** | No identity provider to run; the CLI can log in the same way; TOTP is a few lines of standard library (HMAC); one small, well-known dependency for password hashing (`golang.org/x/crypto`, argon2id) | Shepherd owns a password store and its rate limiting |
| Built-in passkeys (WebAuthn) | Phishing-resistant, no password | Needs a WebAuthn library (CBOR, COSE, attestation), more than "dependencies few" allows for a first step; the CLI still needs another way in |
| OIDC behind the ingress (oauth2-proxy) | Shepherd writes no login code; reuses an existing identity provider | Needs an identity provider; the daemon must trust a header set by the proxy, which is one misrouted request from a bypass; the CLI and members need a non-browser path anyway |

**Recommendation: a built-in owner account with a password and TOTP**, with passkeys added
later as a second factor or replacement. A proxy in front (oauth2-proxy, an IP allow list)
is fine as defence in depth, but the daemon never relies on it.

How it works:

- **Setup.** `shepherd account set` (operator token, so only from inside the pod or the
  machine) sets the password and prints a TOTP enrolment URI once. The store keeps the
  argon2id hash and the TOTP secret; neither is ever returned by the API.
- **Logging in from a browser.** `POST /v1/web/login` with password and TOTP code, then the
  existing session path: a random cookie kept as a hash, now with `Secure` and the
  `__Host-` prefix, the CSRF header on every write, the Origin check against the public
  URL. Failed attempts are rate limited per address and in total, and repeated failure locks
  the account for a while and puts an item in the feed. A used TOTP code cannot be reused.
- **Logging in from the CLI.** `shepherd login https://shepherd.example.com` asks for the
  same password and code and gets a **device token**: named, kept only as a hash, with an
  expiry (default 30 days), listed and revoked from the web Settings page and from
  `shepherd account devices`. It is stored in the client's home, never in `hosts.yaml`.
- **Revocation.** Logging out ends one session; "sign out everywhere" ends every session
  and device token; changing the password or TOTP secret does the same. All of them are rows
  in the store, so revocation survives a restart.
- **Roles.** A browser session and a device token both have the web role: the person's
  powers over the conversation, decisions, approvals and (with confirmation, as today)
  settings, but not the operator's setup routes or token minting. Worker and desk tokens are
  unchanged: minted in memory for agents and the desk, never sent outside the pod.
- **Crook's person's turn.** A desk turn is the person's when it comes from a logged-in
  session or device token, exactly as a web turn is today. A `[Shepherd]` wake-up is not,
  so the desk still cannot answer a decision in it.
- **The operator token.** In hosted mode the operator routes answer only on loopback
  (reached by `kubectl exec` or `kubectl port-forward`), and the runtime file stays on the
  volume. The internet listener refuses the operator token outright.

Never exposed over the public listener, in logs, in the feed or in the web UI: the operator
token, worker and desk tokens, the password hash and TOTP secret, agent CLI credentials,
forge tokens, git push keys. Run logs and artifacts are already redacted
(`internal/redact`); hosting raises the stakes, so phase 0 adds a test that a known secret
written by a fake agent never reaches a public route.

**Hosted mode in config.** A new `daemon.public` section turns it on: the public URL, the
address to listen on inside the pod (`0.0.0.0:8080`), and the proxies whose
`X-Forwarded-For` and `X-Forwarded-Proto` are trusted. Without it, `daemon.listen` stays
loopback-only, as now. With it, the Host check accepts the public name, a request not
marked HTTPS by a trusted proxy is refused, and responses carry HSTS. Sign-in links from
the operator token keep working on loopback.

### Running in Kubernetes

| Concern | Design |
|---|---|
| Image | One image from the release binary: `shepherd daemon run` as PID 1 under a small init, with git (at least 2.31), `glab`, `gh`, `ssh` and the agent CLIs the person uses. Runs as a non-root user. Built from a `Dockerfile` in the repo by the release pipeline, tagged with the release version |
| State | One persistent volume (ReadWriteOnce) mounted as `SHEPHERD_HOME` and the workspace: `config.yaml`, the SQLite store, `runs/` logs, artifacts, the workspace's repo clones and every lane's worktree. Unpushed lane work lives only here |
| Replicas | Exactly one: a StatefulSet (or Deployment with the `Recreate` strategy) with `replicas: 1`. The daemon's home lock refuses a second process on the same volume; two pods on two volumes would be two daemons, which is the cluster's job, not scaling |
| Credentials | Kubernetes Secrets, mounted as files (not environment variables, which agents' processes would inherit and could print): the Claude, OpenCode and Copilot credentials, the `glab` and `gh` tokens, the git push key and its known hosts. The secrets' source of truth is uBixVault, synced in by whatever the cluster already uses; none is written into config or the repo. An agent CLI that logs in interactively is logged in once with `kubectl exec`, its state kept on the volume |
| Network | An Ingress with TLS (cert-manager or the cluster's own certificates) to the Service on 8080. A NetworkPolicy limits egress to the forges, the model providers and DNS. The admin port is never in the Service |
| Probes | Liveness and readiness on an unauthenticated `GET /v1/health` that says only "up" |
| Shutdown | `terminationGracePeriodSeconds` long enough for the existing drain (`Draining`), so a rollout lets runs finish or records them interrupted |
| Backups | A nightly consistent copy of the store (SQLite's online backup, not a file copy of a live database) and volume snapshots for the worktrees. Repo clones can be cloned again; unpushed lanes and the store cannot. Restore is documented and tested once by hand |
| Deploys | Human: the person applies the manifests and bumps the image tag. The repo ships an example manifest and a page describing it, nothing that deploys |

### Done when

Phase 0 is too large for one merge request; it splits in three, each reviewable alone.

**0a. Login and hosted mode.**
Scope: `internal/config` (`daemon.public`), `internal/daemon/web.go`, `internal/daemon/auth.go`,
a new `internal/daemon/account.go`, `internal/store` and `internal/store/sqlite` (an appended
migration for the account and device tokens), `internal/api/web.go`, `internal/cli`
(`shepherd account`, `shepherd login`), `web/src` (a login page and the device list in
Settings), `go.mod` (`golang.org/x/crypto`), `docs/hosted.md`, `docs/config.md`.

- Done when, with `daemon.public` set, a browser logs in with password and TOTP and uses the
  web UI and the desk; a wrong code, a reused code and a burst of attempts are refused and
  logged; the operator token is refused on the public listener; logout and "sign out
  everywhere" end sessions and device tokens across a restart; a known secret from a fake
  agent never appears on a public route; without `daemon.public` nothing changes. Both
  themes checked.

**0b. The CLI over HTTPS.**
Scope: `internal/remote` (an HTTPS `Transport` beside `SSH`), `internal/cli/host.go`
(`--host https://...`, a `url` field in `hosts.yaml`), `internal/client`, `docs/cli.md`,
`docs/hosted.md`.

- Done when `shepherd login` then `shepherd --host https://... status`, `chat` and
  `decision answer` work against a test daemon behind a TLS test server, a revoked or
  expired device token gives a plain "log in again", and the token never appears in
  output, logs or `hosts.yaml`.

**0c. The image and the example deployment.**
Scope: `Dockerfile`, `deploy/kubernetes/` (example StatefulSet, Service, Ingress,
NetworkPolicy, Secret templates with no values), `.github/workflows/release.yml` (build and
sign the image), `docs/hosted.md`.

- Done when the image builds in CI for the release, starts with a fresh volume and answers
  its health check, runs a fake-agent lane end to end in a local cluster (kind), survives a
  pod restart with its store and lanes intact, and the backup and restore steps in the doc
  have been run once by hand.

## Topology

| Option | How it works | For | Against |
|---|---|---|---|
| **Hub and members** | One daemon (the hub) runs the desk and the person's clients, follows each member's feed and calls each member's API. Members are ordinary daemons | Reuses the HTTP API and phase 0's login; each store stays authoritative; a member works alone when the hub is away; one desk, one session, one spend line for it | The hub is a single place to talk from; if it is down, the person uses a member directly (as today) |
| Peer federation | Every daemon follows every other and runs its own desk over all of them | No special daemon | N desks react to each event and spend N times; conflicting actions; N-squared links; agreeing on one thread is a consensus problem |
| Shared store | All daemons write one database | One truth, no merging | SQLite over a network filesystem is unsafe, so it needs a database server; breaks "the daemon owns the store"; a partition stops every machine; worktrees and processes are still per machine |

**Recommendation: hub and members**, and the hosted daemon from phase 0 is the natural hub:
always on, reachable from everywhere, already behind a login. It keeps every write on the
daemon that owns the work, and a partition degrades to "the hub cannot see a member yet",
never to two machines disagreeing about a lane.

### The hub is a role, not a program

The hub is the same `shepherd` daemon with a `cluster:` section in its config. Any daemon
can be one; a member is a daemon whose config names a hub to link to.

**Recommendation: keep it in the one binary.** Everything the hub does is already in the
daemon: the store for cursors, the desk, the feed hub that wakes it, the login, the web UI,
the API every client speaks. A separate hub program would either copy those or proxy every
call through to a daemon beside it, and the person would run, upgrade and secure two things
instead of one. It also keeps the hub useful on its own: the hosted daemon is a hub that also
runs its own lanes.

A split would make sense only if the hub stopped being one person's: many people or teams
sharing it, or so many members that following their feeds needed its own scaling. Both are
outside this plan; if they come, the HTTP API between hub and members is already the
boundary to cut along.

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
| **HTTPS link** (primary) | The member dials out to the hub's public URL and holds the connection open | The hub is hosted (phase 0) and members sit behind NAT on laptops and home machines, where the hub cannot reach them | A member credential from enrolment, below |
| SSH tunnel | The hub dials the member with `remote.SSH`, as `--host` does | A hub that can ssh to the member, with no public URL | The member's runtime file, read over ssh |

**The HTTPS link reuses phase 0.** The person enrols a member from the hub's web UI or with
`shepherd member add <name>` there, which shows a one-time, short-lived enrolment code. On the
member, `shepherd cluster join https://shepherd.example.com <code>` trades it for a **member
credential**: like a device token (hashed on the hub, named, revocable from the same
Settings list) but with a member role that can only open the link and push its own feed,
never act as the person. The member then holds one outbound HTTPS connection to the hub:
it sends its feed items and status up, and receives the hub's calls (reads, and in phase 5
actions) down the same connection, answering each one through its own local API. The
member never listens beyond loopback; nothing new is opened on the member's machine. The
member's credential is stored in its home, never in config.

**What the SSH transport costs in trust.** Over ssh the hub reads the member's runtime file,
which holds the operator token, so the hub can do anything the person can on that member.
That is the trust `--host` already has; acceptable for reads, too much for actions.

**Actions, either way.** A call from the hub that acts (answer, start, stop) carries the
turn's facts: desk or person, whether a person started the turn, the turn id. The member
mints a local desk token with those facts for that one call, so Crook on the member records
the right actor, and the member's own role checks still apply. The member trusts the hub's
claim that a person started the turn; that is why the hub's login is the person's and why a
member credential, if stolen, cannot make such a claim (it opens a link to the hub, it is
not the hub). Over ssh, the operator token is used only to mint that desk token, never to act.

**Later, optional: mutual TLS between daemons**, with certificates pinned per member, for a
member that may not hold an outbound connection. Listed as an open question, not a phase.

## The one conversation

| Surface | How it merges |
|---|---|
| Thread | Stays on the hub: one desk session, one `desk_events` history. Member events enter it as `[Shepherd]` events that name their member ("on build: run 42 failed its gate") |
| `[Shepherd]` events | The hub keeps a cursor per member (last feed id, in a new hub table). Over the link the member sends from that cursor; over ssh the hub reads from it. Either way the items reach the hub's feed hub tagged with the member, and the desk wakes on them by the same rules as local items |
| Waiting on you and the Inbox | `GET /v1/waiting` on the hub returns its own items plus each member's, each with a `member` field. Counts are per member and in total; zero stays visible and faint |
| Topics, keep-going | The hub's, since they belong to the conversation |
| A member's own desk | Still there for someone at that machine, but it does not wake for swarm events while a hub holds its wake lease: held while the link is up (or the hub keeps reading over ssh), lapsed a few minutes after. This stops two desks reacting to one event |

**Ordering.** Within one member, items keep their feed order (ids are monotonic). Across
members there is no total order: the thread shows events in the order the hub received them,
with each member's own timestamp, and the hub warns once if a member's clock is more than a
minute off.

**Dedup.** A merged item is keyed by (daemon id, feed id). A resent batch after a reconnect,
or two links racing during a redial, cannot add an event twice. A reconnect resumes from the
cursor, so nothing between the last read and the outage is lost.

**A member offline.** The hub shows it in status, the thread gets one event when it goes away
and one when it is back (not one per retry), and the web footer names it. Its last Waiting on
you items stay listed, marked stale with the time they were read, and their actions are off.
Actions aimed at it are refused with a plain message, never queued: a queued "stop run 42"
that fires an hour later would surprise the person. On return, the hub catches up from the
cursor and the desk gets one summary event rather than a burst, when more than a handful
arrived.

**Answering.** A decision answered in the hub's thread or Inbox is sent to its member with
the person's words and the person-started turn facts. The member delivers it to the asking
run exactly as today (`Runner.Answer`). The hub never answers one on its own.

## Leases, spend, keep-going and merge rules

| Concern | Per daemon or cluster | Rule |
|---|---|---|
| Scope leases, tag reservations | Per daemon | A repo has one **home** daemon in the cluster: the hub refuses to follow two daemons that register the same origin remote, until the person picks one. Opening a lane goes to the repo's home. Leases and reservations then stay correct with no cross-machine locking |
| Spend caps | Per daemon, plus a cluster view | Each daemon's `daemon.budget` stays the hard cap it enforces itself. The hub shows the cluster's total and may carry a cluster cap that is soft (warns) at first |
| Keep-going | The hub's | The mode is the hub's desk's. Its cap counts the cost of every run it started, on any member, tracked by (member, run id). Each member still applies its own limits and `Allow` gate to what the hub asks of it |
| Merge rules, autonomy | Per repo | Unchanged: read from the repo's base branch and evaluated by its home daemon. The hub shows "Merged by rule <name>" as the member reports it |
| `max_runs`, quota holds | Per daemon | They describe a machine's capacity and an agent CLI's account on that machine |

## Failure modes

| Failure | What happens |
|---|---|
| Partition between hub and member | The member carries on: runs finish, the watcher polls, decisions wait on it. Its desk takes the wake-ups once the hub's lease lapses. The hub marks it offline, keeps the stale view, refuses actions, and catches up from its cursor on return. Over the link the member redials with backoff |
| Member restarts mid-run | As today on that member: agents left by a crashed daemon are stopped and their runs recorded interrupted. It reopens the link (or the hub redials over ssh) and resends from the cursor; the interrupted runs reach the thread as ordinary events |
| The hosted hub's pod restarts | The drain lets its own runs finish within the grace period, or records them interrupted. Cursors, sessions and device tokens are in the store, so the person stays logged in and members' links resume where they stopped |
| Hub's public name or certificate fails | Members cannot link and the person cannot log in from outside; every daemon still works locally, and `kubectl port-forward` reaches the hub's loopback |
| Member credential stolen | It can only open a link as that member and send feed items under its name; the person revokes it from Settings and enrols the member again |
| Member reinstalled or renamed | The daemon id changes; the hub stops following it and asks the person to confirm, so items from two different stores never share a name |
| Feed pruned on a member before the hub caught up | The hub sees a gap between its cursor and the oldest item, says so once in the thread, and resumes from the oldest |
| Version skew | Each daemon reports its version and an API level with a list of capabilities when the link opens. The hub uses only what a member reports, says which features are off for it ("build is on 0.9: answering decisions from here needs 0.10"), and never fails a whole read because one member is old |
| Two hubs configured by mistake | A member links to one hub; the wake lease names its holder, and a second hub over ssh is told another hub holds it, and the person is warned |

## Phased plan

Each phase is one merge request, and each is useful alone. Phase 0 (0a, 0b, 0c) is above.

### 1. A member linked to the hub, status only

Scope: `internal/config` (`cluster:` on the hub, `cluster.hub` on a member), `internal/daemon`
(a `cluster.go` for the link on both sides; daemon id in the home and in status; enrolment),
`internal/store/sqlite` (an appended migration for members and their credentials),
`internal/api` (`GET /v1/cluster`, the link and enrolment routes), `internal/client`,
`internal/cli` (`shepherd member add`, `shepherd cluster join`, `shepherd cluster status`, the
`cluster_status` MCP tool), `web/src` (members in Settings), `docs/cluster.md`, `docs/config.md`.

- Done when a member enrolled with a one-time code holds a link to a test hub over TLS; the
  hub's `GET /v1/cluster` lists it with online or not, version, daemon id, workspaces, runs
  going and waiting counts; the desk answers "what is running on build" with that tool; a
  revoked credential drops the link; a member whose id changed is refused until confirmed.

### 2. Read-only desk tools across members

Scope: `internal/cli/mcp.go` (a `host` argument and prefixed ids on `lane_list`, `run_list`,
`run_status`, `decision_list`, `question_list`, `next_list`, `lane_diff`), `internal/daemon`
(reads sent down the link), `internal/desk/claude.go` (one brief line on member names),
`docs/cluster.md`.

- Done when the desk can list and read lanes, runs and decisions on a named member, a read
  with no `host` covers every member with each row marked, and an offline member gives one
  plain line in the answer, not an error for the whole read.

### 3. Members' events in the thread

Scope: `internal/store/sqlite` (an appended migration for member cursors), `internal/daemon`
(`feedhub.go` takes members' items; the wake lease), `internal/api` (member on feed items),
`internal/desk` (event text names the member), `web/src/model/feed.ts`, the terminal chat's
event rendering, `docs/cluster.md`.

- Done when a member's run outcome appears once in the hub's thread with its member name,
  the hub's desk wakes for it and the member's desk does not, a hub restart or a dropped link
  neither loses nor repeats an item, and the member's desk wakes again after the lease lapses.

### 4. One Waiting on you

Scope: `internal/waiting`, `internal/api/waiting.go`, `internal/daemon`, `web/src/pages/Inbox.tsx`,
`web/src/model/backlog.ts`, the terminal dock, `docs/waiting-on-you.md`, `docs/cluster.md`.

- Done when the Inbox and the dock list every member's items with a member chip, counts per
  member stay visible at zero, an offline member's items show stale with their time and no
  actions, and the footer names the member. Both themes checked.

### 5. Acting on a member

Scope: `internal/daemon/auth.go` (per-call desk tokens from turn facts), `internal/daemon`
(actions sent down the link), `internal/cli/mcp.go` (`host` on `lane_open`, `lane_run`,
`run_continue`, `run_stop`, `decision_answer`, `question_answer`, `next_add`, `request_close`),
`web/src/pages/Inbox.tsx` (answer in place), `docs/cluster.md`.

- Done when a decision answered in the hub's thread reaches the asking run on its member;
  Crook on the member records the person as the actor only for a person-started turn and the
  desk otherwise; a member credential alone cannot make an action; an action on an offline
  member is refused, not queued.

### 6. Repo homes and cluster spend

Scope: `internal/daemon` (repo homes by origin; routing `lane_open` to the home),
`internal/daemon/keepgoing.go` (counting members' runs), `internal/api` (cluster spend), the
web spend view, `docs/keep-going.md`, `docs/cluster.md`.

- Done when the hub refuses to follow a repo registered on two daemons until the person picks
  one, `lane_open` without `host` opens on the repo's home, the cluster's daily total is shown
  with a soft cap that warns, and keep-going stops at its cap counting runs it started on
  members.

### 7. Members over SSH

Scope: `internal/daemon/cluster.go` (the hub dialling with `remote.SSH` behind the same member
interface), `internal/config` (a member given as a `hosts.yaml` name), `docs/cluster.md`.

- Done when a member reached over ssh behaves in phases 1 to 6 exactly as a linked one, tested
  with the fake ssh `Runner`, and the operator token read over ssh is used only to mint
  per-call desk tokens.

## Open questions

| # | Question | Options | Recommendation |
|---|---|---|---|
| 1 | How the person logs in | Password and TOTP / passkeys / OIDC behind the ingress | Password and TOTP built in, passkeys later; a proxy in front only as defence in depth |
| 2 | Whether the hosted daemon runs agents itself | Hub only / hub and its own lanes | Both: it is a full daemon, and lanes for repos that need no special machine can run there. Its egress policy and secrets decide what those agents can reach |
| 3 | How long a device token lives | 30 days / until revoked / per login | 30 days, renewed by use, revocable from Settings |
| 4 | Is the hub a separate program | Same binary, a config role / a separate service | Same binary; split only if it ever serves many people |
| 5 | Members' transport | HTTPS link only / SSH only / both | Both, the link first (phase 1) because the hosted hub cannot reach laptops; SSH as phase 7 |
| 6 | Does a member's own desk keep working | Off while in a cluster / on, with the wake lease | On, with the lease: someone at the build box can still talk to it, and two desks never both react |
| 7 | A cluster-wide hard spend cap | Soft only / hard, enforced by the hub telling members to hold | Soft first (phase 6); hard later if needed, since a hard cap needs members to obey the hub |
| 8 | Hub's items unprefixed | Bare for the hub's own / prefix everything | Bare for the hub's own: nothing changes for a single daemon |
| 9 | Where the image is published | GitHub's container registry / the internal forge's registry / both | Both, signed like the release archives, so a stranger can run it and the person's cluster pulls from its own registry |
