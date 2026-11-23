# A controller, workers and one conversation: design

Status: revised on 2026-11-22 for review. Not approved yet. The 2026-11-05 design was approved
by the maintainer; this revision changes its topology (see
[What changed](#what-changed-from-the-2026-11-05-decisions)). Phase 0a is built; phase 0b is
built except the items under its status note; phase 0c is in progress. Nothing from phase 1
on is built, and no code is to start until this revision is reviewed.

The detail is in two pages beside this one:

- [cluster/phase-0.md](cluster/phase-0.md): the hosted daemon, login, deployment examples
  and phase 0's done-when lines (unchanged except for names).
- [cluster/offline.md](cluster/offline.md): the journal, replay, what the main desk sees,
  the summaries-only setting and handing a repo to another machine.

## Goal

The person works on repos that live on different machines and wants one conversation across
all of them. The real case: a work laptop holds every work repo, a home laptop holds every
personal repo, and a Shepherd daemon deployed in Kubernetes keeps both sets going. At work
the person talks to that daemon about the work repos, using the laptop's agents. Away from
the laptop (a long commute, the end of the day), they still talk to the same daemon and get
a straight answer about the work repos, even though their agents sleep with the laptop. Back
at the laptop, whatever happened there while it was disconnected is in the thread.

## The model

Eight rules. Everything below follows from them.

1. **Every repo has exactly one home**: a work laptop, a home laptop, or the controller
   itself for repos that need no local hardware. The home owns that repo's lanes, scope
   leases, worktrees, runs and decisions, and its store is authoritative for them.
2. **The controller owns what spans machines**: the one conversation, Next up, topics, one
   Waiting on you, the spend view and keep-going across machines. It never writes a repo's
   lane state itself; it asks the home.
3. **Workers link outbound** to the controller, so NAT, VPNs and work networks are fine.
   The controller never needs a route to a laptop.
4. **A disconnected worker is still a full Shepherd** with a local desk for its own repos.
   Its turns, answers and events are journaled and replay into the controller's thread as
   one folded block ("Work laptop, offline 2h: 3 turns, 14 events"). There are no
   conflicts, because only the home could touch its repos.
5. **While a home is offline the controller shows its last-known state as stale.** Actions
   on its repos are refused, not queued. Planning Next-up items for them is still allowed.
6. **A closed laptop's runs pause.** Only repos homed on the controller run around the
   clock.
7. **Moving a repo's home is an explicit handover**: push branches, close lanes, switch.
8. **A per-home privacy setting**: a worker can send the controller summaries only and keep
   diffs and logs local (work code, a personal controller).

### Words

| Word | Means | Replaces |
|---|---|---|
| Controller | The daemon that holds the one conversation: the same `shepherd` binary with `cluster.role: controller`, reachable over HTTPS (phase 0). Usually the pod | Hub |
| Worker | A daemon on a laptop or other machine that links out to the controller | Member |
| Home | The one daemon, a worker or the controller, that owns a repo. Not the Shepherd home directory (`~/.shepherd`), which this doc calls "the Shepherd home" | The "repo home" of phase 6, now the core rule |
| Folded block | The single thread event a replayed offline period becomes | New |

**On the names.** "Controller" is kept because the person already says it, and it says what
the daemon does: it decides nothing about a repo, it coordinates. "Worker" is the person's
word and is used here, but it already means something in the code and the docs: the *worker
tokens* and *worker tools* (`shepherd mcp --worker`, `SHEPHERD_RUN`) are for the agents
Shepherd starts. The two never meet in one API, but a reader can confuse them. This doc says
"agent worker token" where the old sense is meant and "worker" alone for the machine. If the
overlap proves costly, the open question below lists the alternative.

### Non-goals

- Moving a run from one machine to another, or scheduling work onto whichever machine is
  free. A repo's work happens where its home is.
- A shared or replicated store. Each daemon's store is the only truth for its repos.
- Two machines both working one repo at once. One home per repo; another clone is read-only.
- Changing the default. A daemon listens on loopback unless it is configured otherwise, and
  beyond loopback only with TLS and a login.
- Several people sharing one controller. One person (one account), their machines.
- A desk on every machine reacting to every event. One desk holds the conversation at a time.
- Keeping a laptop awake, or queueing actions for later.
- Requiring any platform. Kubernetes, Docker and systemd appear only as examples.
- Shepherd deploying itself. The doc describes deployments; a person applies them.

### Use cases

| Case | Machines | What the person wants |
|---|---|---|
| A reachable daemon | One daemon on an always-on machine | Open `https://shepherd.example.com` from any browser or phone, log in, talk to the desk and answer decisions |
| Work laptop and home laptop | Each holds its own repos; a controller in Kubernetes | One thread and one Waiting on you across both; at work, run agents on the work repos; on the road, ask the controller what the work laptop was doing and what it needs |
| Repos with no local hardware needs | The controller itself | A docs or service repo that works overnight because the pod never sleeps |
| A work code policy | A work laptop, a personal controller | The controller sees "run 42 passed" and a cost, never the diff |

## What exists today

| Piece | Where | What it gives |
|---|---|---|
| Loopback-only daemon | `daemon.listen`, `allowedHost` in `internal/daemon/web.go` | No listener to secure; also no way to reach a daemon except through ssh |
| Browser sessions | `internal/daemon/web.go`, `web_sessions` | The web role: the desk's powers, the conversation and the person's answers, not setup |
| Tokens and roles | `internal/daemon/auth.go` | Operator token in the runtime file; agent worker and desk tokens minted in memory. Crook sees a desk token as the person's only in a turn the person started |
| Remote CLI and SSH transport | `--host`, `~/.shepherd/hosts.yaml`, `internal/remote` | Any command can be aimed at another machine's daemon over ssh; `Transport` is the seam for another |
| Remote URL matching | `workspace.NormalizeRemote` | Reduces ssh, scp-like and https forms of a remote to `host/path`; the scan already uses it |
| The desk | `internal/desk`, `internal/daemon/desk.go` | One Claude Code session per workspace, woken by feed items |
| The feed and its hub | `feed` table, `internal/daemon/feedhub.go` | A monotonic id per item, polled to wake the desk and streams |
| Thread, Waiting on you | `desk_events`, `internal/waiting` | Stored per workspace; Waiting on you is derived on each read |
| Topics, questions, Next up, keep-going, spend | their tables, `daemon.budget` | Per workspace or per daemon, keyed by local integer ids |

## Phase 0: a reachable daemon with a login

Kept as designed on 2026-11-05: hosted mode, the owner account with password and TOTP,
device tokens, deployment examples and the three merge requests 0a, 0b and 0c. The page is
[cluster/phase-0.md](cluster/phase-0.md). 0c is in progress in its own lane, which owns
`docs/hosted.md`.

**Deployment note.** The Kubernetes example puts everything in one namespace, `shepherd`:
the controller StatefulSet, its Service, Ingress, NetworkPolicy and Secret templates. There
are no per-environment namespaces; a second environment is a second cluster or none. A
controller reached by workers needs one more thing from the deployment: its public URL must
pass a long-lived WebSocket (an ingress timeout of an hour or more, or WebSocket-aware
defaults), because every worker holds one open.

## Topology

| Option | How it works | For | Against |
|---|---|---|---|
| **Controller, workers, one home per repo** | One daemon (the controller) holds the conversation and the person's clients. Each repo has one home whose store is authoritative; workers link out; a worker works alone when the controller is away | Reuses the HTTP API and phase 0's login; no write conflicts by construction; the controller can be unreachable without any machine disagreeing about a lane; the person's real case works | A repo can only be worked where its home is; moving it takes a handover |
| Hub and members, homes derived from clones (the 2026-11-05 design) | The same, but a repo's home was whichever daemon registered it, sorted out in phase 6 | Close to the above | Left the hardest rule to the last phase; assumed the hub could reach or follow every member; did not design a worker's own desk, its offline work or privacy |
| Peer federation | Every daemon follows every other and runs its own desk | No special daemon | N desks react to each event and spend N times; conflicting actions; agreeing on one thread is a consensus problem |
| Shared store | All daemons write one database | One truth | SQLite over a network filesystem is unsafe; breaks "the daemon owns the store"; a partition stops every machine |

**Recommendation: the first row.** A partition degrades to "the controller cannot see a
worker yet", never to two machines disagreeing about a lane, because the home is the only
writer.

### The controller is a role, not a program

The controller is the same `shepherd` daemon with `cluster.role: controller`; a worker is a
daemon whose config names a controller to link to (`cluster.controller: <url>`). Everything
the controller does is already in the daemon: the store, the desk, the feed hub, the login,
the web UI, the API every client speaks. A separate program would copy those or proxy every
call, and the person would run, upgrade and secure two things. It also lets the controller be
the home of its own repos (rule 1) with no new mechanism: those lanes run on the pod as they
run on any daemon. A split would make sense only if the controller stopped being one
person's; the HTTP API between controller and worker is the boundary to cut along.

## Repo homes

The controller keeps a table of homes: `(origin, home, gen, also)`. The origin is
`workspace.NormalizeRemote` of the repo's remote, so `git@host:group/repo.git` and
`https://host/group/repo` are one repo.

| Rule | Detail |
|---|---|
| Claim | A daemon registering a repo whose origin has no home claims it, generation 1. The controller's own repos are claimed by the controller |
| Second claim | A second daemon registering the same origin is refused for lane work and shown as a read-only clone. The person resolves it by a handover, or marks an intended second checkout with `--also` |
| Routing | Anything that acts on a repo (open a lane, start or stop a run, answer a decision) goes to the home. `lane_open` without a `host` argument opens at the repo's home |
| Reads | A read of a repo's lanes, runs and decisions is served from the home when it is online, and from its last-known state, marked stale, when not |
| Handover | Explicit, and fenced by the generation; see [cluster/offline.md](cluster/offline.md#moving-a-repos-home) |
| Fence | A daemon whose generation for a repo is below the controller's treats that repo as read-only |

Leases and tag reservations stay per daemon, per repo, so they are correct with no
cross-machine locking: the home is the only place that ever takes them.

## Identity and naming

Each daemon gets a **daemon id** (a random UUID written to its Shepherd home on first start,
shown in `GET /v1/status`), and the controller knows each worker by a **worker name**,
short and readable (`work`, `home`), given at enrolment. The controller refuses a worker
whose id has changed (a reinstall, or a name now used by another machine) until the person
confirms it.

A repo's home-owned things are addressed as `<worker>:<thing>`:

| Thing | Local form (unchanged) | From the controller |
|---|---|---|
| Lane | `fix-crash`, `repo/fix-crash` | `work:fix-crash`, `work:repo/fix-crash` |
| Run | `run 42` | `work:run 42` |
| Decision, request, question | `decision 7` | `work:decision 7` |
| Feed item | id 900 | (`work`, 900), shown as an event from `work` |

Things the controller owns (its thread, Next-up items, topics) keep their bare form. A
Next-up item for a worker's repo is the controller's record and has a bare id; a Next-up
item added at a worker while offline is imported under `(worker, local id)`. A topic's link
to a worker's item stores the daemon id, the kind and the worker's id.

Desk tools that take a lane, run or decision accept the prefixed form, and gain an optional
`host` argument for tools that take no id (`lane_list`, `run_list`, `waiting`). No `host`
means the controller's own repos and, for reads, every worker, each row marked with its
worker.

## Transport and auth

Workers link outbound; the controller never dials a worker. The 2026-11-05 design also kept
an SSH transport for a controller that could reach a worker; that is dropped from the plan
(see [What changed](#what-changed-from-the-2026-11-05-decisions)), and `--host` over ssh
remains as a way to use one daemon directly.

### Enrolment

The person runs `shepherd worker add <name>` on the controller, or uses Settings there. It
shows a one-time enrolment code: eight words from the EFF short wordlist (1296 words,
embedded in the binary with its attribution), lowercase, joined by `-`, about 82 bits;
single use, valid for five minutes. On the worker, `shepherd cluster join
https://shepherd.example.com <code>` trades it for a **worker credential**: kept as a hash in
the controller's `worker_credentials` table, named, revocable from Settings, stored on the
worker in its Shepherd home and never in config. Its role is `link`: it can open the link
and push that worker's own journal and status, and nothing else. Every acting route
(decision answer, lane open, run start and stop, settings) answers it with 403.

### The link

| Part | Design |
|---|---|
| Opening | The worker opens one long-lived connection to `GET /v1/cluster/link` with its credential and upgrades it to a WebSocket (the carrier, below), carrying a framed, bidirectional stream |
| Handshake | Both sides send their daemon id, version, `api_level` and capabilities (`read_lanes`, `push_journal`, `act`, and later ones). The controller also sends its cursor for the worker (see offline.md), the worker its last journal `seq`, the home generations it believes it holds, and its `cluster.share` setting. The controller uses only what a worker reports and says what is off for it |
| Frames | Length-prefixed JSON frames. Journal batches and status go up; the controller's calls go down as RPCs with a request id, each answered by the worker through its own local API. Many RPCs share the connection at once, matched by id, each with its own deadline |
| Keepalive | A ping every 20 seconds each way, under the idle timeouts of common proxies and load balancers; two missed pongs close the link |
| Reconnect | Full-jitter exponential backoff: a random wait between zero and the current step, doubling from one second, capped at 60 seconds. A `Retry-After` from the controller is honoured as a floor |
| Resume | The worker resends from the controller's cursor, which the handshake returns |

**The carrier: WebSocket** (`github.com/coder/websocket`, no further dependencies), behind a
small carrier interface so the framing does not depend on it. It crosses the proxies a work
network is likely to have, which the alternatives (a custom `Upgrade` header, HTTP/2
full-duplex, a long `GET` with short `POST`s) do not reliably do. The handshake is a `GET`.
Should the dependency ever need to go, a custom upgrade fits behind the same interface.
Outbound-only matters most here: a work laptop on a corporate network usually allows
outbound HTTPS to a public name and nothing else.

### Trust

**Actions.** A call from the controller that acts (answer, start, stop) carries the turn's
facts: desk or person, whether a person started the turn, the turn id. The worker mints a
local desk token with those facts for that one call, so Crook on the worker records the
right actor and the worker's own role checks still apply. The worker trusts the controller's
claim that a person started the turn, which is why the controller's login is the person's. A
stolen worker credential cannot make that claim: it opens a link to the controller, it is
not the controller.

**What the controller can ask.** Only what the worker's API offers its desk role, and under
`cluster.share: summaries` less: the worker filters before it answers (see
[the privacy setting](cluster/offline.md#the-privacy-setting)). The controller holds no
worker's operator token, so a controller that is compromised can act as the person's desk on
a worker but cannot reconfigure it.

## The one conversation

| Surface | How it merges |
|---|---|
| Thread | On the controller: one desk session, one `desk_events` history. A worker's events enter it as `[Shepherd]` events naming the worker ("on work: run 42 failed its gate") |
| `[Shepherd]` events | Carried by the worker's journal. The controller keeps a cursor per worker and applies entries in order |
| Waiting on you and the Inbox | `GET /v1/waiting` on the controller returns its own items plus each worker's, each with a `worker` field. Counts are per worker and in total; zero stays visible and faint. An offline worker's items are stale and without actions |
| Topics, Next up, keep-going | The controller's |
| A worker's own desk | Still there for the person at that machine. While the link is up the controller's desk holds the wake lease and the local desk does not wake; the lease lapses two minutes after the link is confirmed down |

**Answering.** A decision answered in the controller's thread or Inbox is sent to its home
with the person's words and the person-started turn facts. The home delivers it to the
asking run as today (`Runner.Answer`). The controller never answers one on its own. A
decision answered at the worker while offline reaches the controller as a journal entry and
closes the item in Waiting on you.

**Offline, replay and catching up** are in [cluster/offline.md](cluster/offline.md): the
journal's order, idempotency and cursor; the folded block; what is stale and refused.

## Spend, keep-going and merge rules

| Concern | Per daemon or cluster | Rule |
|---|---|---|
| Spend caps | Per daemon, plus a cluster view | Each daemon's `daemon.budget` stays the hard cap it enforces itself. The controller adds a cluster total and, later, a soft cap. An offline worker's total is its last report, marked stale |
| Keep-going | The controller's | The mode is the controller's desk's. Its cap counts the cost of every run it started, on any home, tracked by (worker, run id). It skips an offline or stale home and says so. Each home still applies its own limits and `Allow` gate |
| Merge rules, autonomy | Per repo | Unchanged: evaluated by the repo's home. The controller shows "Merged by rule <name>" as the home reports it |
| `max_runs`, quota holds | Per daemon | They describe a machine's capacity and an agent CLI's account on that machine |

**Cluster spend.** The controller follows each home's spend entries in its journal and adds
a pending-cost estimate for each run still going (its cost so far, plus the agent's recent
average for a run of that kind), so several homes starting runs at once do not overshoot a
soft cap between reports. A soft cap only warns; each daemon's own hard cap is what holds.
Under `summaries`, costs are still sent: a figure is not code.

## Failure modes

| Failure | What happens |
|---|---|
| Partition between controller and worker | The worker carries on as a full Shepherd with its local desk; runs on an awake machine finish. The controller marks it offline, shows its state stale, refuses actions, and on return applies the journal from its cursor as one folded block |
| Laptop closed or asleep | The link drops and its runs pause with the machine; the person sees the worker as offline with its last-known state. Nothing is lost: on wake, the link reopens and the journal replays |
| Worker restarts mid-run | As today on that worker: agents left by a crashed daemon are stopped and their runs recorded interrupted; the entries reach the thread on the next replay |
| The controller restarts or its pod moves | The drain lets its own runs finish or records them interrupted. Cursors, sessions, device tokens and worker credentials are in the store, so the person stays logged in and links resume where they stopped |
| Controller's public name or certificate fails | Workers cannot link and the person cannot log in from outside; every daemon still works locally, with its own desk |
| Worker credential stolen | It can open a link as that worker and send journal entries under its name; every acting route gives 403. The person revokes it and enrols again |
| Worker reinstalled or renamed | The daemon id changes; the controller stops following it and asks the person to confirm |
| Worker restored from a backup | Its journal `seq` is behind the controller's cursor; the controller applies nothing, says so, and resets the epoch on confirmation (see offline.md) |
| Journal pruned before the controller caught up | The controller sees the gap, says so once in the thread, and resumes from the oldest entry |
| An old laptop returns after its repo moved | Its generation is behind; it is told at the handshake and treats the repo as read-only |
| Version skew | The handshake's `api_level` and capabilities decide what the controller uses, per worker; an old worker never fails a whole read |
| Two controllers configured by mistake | A worker links to one controller; the wake lease names its holder, and a second is told another holds it and the person is warned |
| Restore from backup (controller) | Crash-consistent: the store copy is clean, a worktree may not be; `git fsck` lists the lanes to look at. Workers' cursors are in the controller's store, so a restore that rolls them back is the same case as a restored worker, from the other side: the worker resends from the older cursor, which is safe because application is in order and gated on it |

## Phased plan

Each phase is one merge request, useful alone, and builds only on the ones above it. Phase 0
is in [cluster/phase-0.md](cluster/phase-0.md): 0a and 0b are built, 0c is in progress in
lane `feat/cluster-0c-image`.

### 1. A laptop shows up in the controller's status

The smallest thing that puts a laptop in the controller's view. No repos, no events, no
actions.

Scope: `internal/config` (`cluster.role` on the controller, `cluster.controller` on a
worker), `internal/daemon` (`cluster.go` for both ends of the link; daemon id in the
Shepherd home and in status; enrolment; the `link` role), `internal/store/sqlite` (an
appended migration for workers and `worker_credentials`), `internal/api` (`GET
/v1/cluster`, `/v1/cluster/link`, enrolment), `internal/client`, `internal/cli` (`shepherd
worker add`, `shepherd cluster join`, `shepherd cluster status`, the `cluster_status` MCP
tool), the embedded EFF wordlist, `web/src` (workers in Settings), `go.mod`
(`github.com/coder/websocket`), `docs/cluster.md`, `docs/config.md`.

- Done when an enrolment code is eight lowercase words joined by `-`, enrols a worker once,
  and is refused after use or after five minutes.
- A worker holds a link to a test controller over TLS; the handshake records `api_level`
  and capabilities; pings flow every 20 seconds; a dropped link redials with jittered
  backoff capped at 60 seconds and honours `Retry-After`; the link starts from behind an
  outbound-only network (the test dials out only).
- `GET /v1/cluster` and `shepherd cluster status` list each worker with online or not (and
  since when), version, `daemon_id`, and runs going and waiting counts; the desk answers
  "is my work laptop connected" with the MCP tool.
- A link credential gets 403 on every acting route; a device token is never accepted as a
  link credential or the reverse.
- A revoked credential drops the link; a worker whose id changed is refused until confirmed.

### 2. Repo homes

Scope: `internal/store/sqlite` (an appended migration for the homes table),
`internal/daemon/cluster.go` (claims at the handshake, the generation fence),
`internal/api` and `internal/cli` (`shepherd cluster homes`, `shepherd cluster set-home`
for the first claim and `--also`), `web/src` (homes in Settings),
`docs/cluster.md`.

- Done when the ssh and https forms of one origin are one repo; a repo registered on a
  worker or on the controller claims its home at generation 1; a second registration is
  shown read-only and takes no lane; `--also` keeps an intended second clone readable; and
  `cluster status` lists each repo with its home.
- A repo homed on the controller needs nothing from a worker.
- `lane_open` with no `host` opens at the repo's home, and refuses with a plain message if
  that home is a worker that is offline.

### 3. Reads through the link, with the privacy setting

Scope: `internal/config` (`cluster.share`), `internal/cli/mcp.go` (a `host` argument and
prefixed ids on `lane_list`, `run_list`, `run_status`, `decision_list`, `question_list`,
`next_list`, `lane_diff`), `internal/daemon` (reads sent down the link as RPCs; the
worker-side filter), `internal/desk/claude.go` (one brief line on worker names),
`docs/cluster.md`.

- Done when the desk lists and reads lanes, runs and decisions on a named worker, several
  reads share the link at once, a read with no `host` covers every worker with each row
  marked, and an offline worker gives its last-known state marked stale with one plain line,
  not an error for the whole read.
- With `cluster.share: summaries`, a read for a diff, log or artifact returns "kept on the
  work laptop", asserted by a test that plants a known string in a diff and finds it in no
  frame the worker sends.

### 4. The journal and the thread, while linked

Scope: `internal/store/sqlite` (an appended migration for `journal` on workers and the
cursor on the controller), `internal/daemon` (the journal writer, batching, acks,
`feedhub.go` taking workers' entries, the wake lease), `internal/api` (worker and worker
time on feed items), `internal/desk` (event text names the worker), `web/src/model/feed.ts`,
the terminal chat's event rendering, `docs/cluster.md`.

- Done when a worker's run outcome appears once in the controller's thread with its worker
  name, sorted by controller receive time with the worker's time shown second.
- The controller's desk wakes for it and the worker's does not; the worker's desk wakes
  again after the lease lapses.
- Killing the link mid-batch, restarting the controller, and racing two links lose nothing
  and repeat nothing, tested against the cursor.
- A worker clock more than a minute off gives one non-blocking warning.

### 5. Offline replay

Scope: `internal/daemon` (the folded block and its structured summary; the offline
threshold; the rollback reset and the gap marker; `shepherd chat` attaching to the
controller and falling back to the local desk), `internal/desk` (the block's standing
instruction), `web/src` and the terminal chat (the expandable block, and the stale marker),
`docs/cluster.md`.

- Done when a worker disconnected for over five minutes, with local desk turns, an answered
  decision and finished runs, reconnects and the controller's thread gets one block ("Work
  laptop, offline 2h: 3 turns, 14 events") that expands in `seq` order, with the desk woken
  once.
- The decision answered at the worker closes in Waiting on you; the desk does not answer it
  again.
- A restored worker (lower `seq`) applies nothing until confirmed; a pruned journal posts
  one gap line.
- A blip of under five minutes with fewer than 11 entries replays as ordinary events.
- Under `summaries`, turn text is absent from the block and from every frame sent.

### 6. One Waiting on you, with stale and refused

Scope: `internal/waiting`, `internal/api/waiting.go`, `internal/daemon`,
`web/src/pages/Inbox.tsx`, `web/src/model/backlog.ts`, the terminal dock,
`docs/waiting-on-you.md`, `docs/cluster.md`.

- Done when the Inbox and the dock list every worker's items with a worker chip, counts per
  worker stay visible at zero, an offline worker's items show stale with their time and no
  actions, and the footer names the worker. Both themes checked.

### 7. Acting on a home

Scope: `internal/daemon/auth.go` (per-call desk tokens from turn facts), `internal/daemon`
(actions sent down the link, behind the `act` capability; acknowledgement and idempotent
apply), `internal/cli/mcp.go` (`host` on `lane_open`, `lane_run`, `run_continue`,
`run_stop`, `decision_answer`, `question_answer`, `request_close`), `web/src/pages/Inbox.tsx`
(answer in place), `docs/cluster.md`.

- Done when a decision answered in the controller's thread reaches the asking run on its
  worker; Crook on the worker records the person only for a person-started turn; a worker
  without `act` is told so; a worker credential alone cannot make an action.
- An action on an offline worker is refused, not queued, and a request whose ack was lost
  does no harm when repeated.
- Next-up items for an offline worker's repos can be added at the controller, and are
  offered, not started, on reconnect.

### 8. Handover

Scope: `internal/daemon` (quiesce, branch verification, the generation switch, the fence),
`internal/cli` (`shepherd cluster move-home`, with `--force`), `web/src` (Settings),
`docs/cluster.md`.

- Done when a repo with an open lane moves from one worker to another: the lane's branch is
  pushed and verified on the origin, the lane closed and its lease released, the generation
  goes up, and lanes open at the new home.
- A move is refused while a run is going or a branch is unpushed, naming which.
- A worker asleep during the move learns at its next handshake and treats the repo as
  read-only; `--force` moves a repo from a home that is gone, with the warning.

### 9. Cluster spend and keep-going

Scope: `internal/daemon` (spend entries and the pending-cost estimate),
`internal/daemon/keepgoing.go` (counting homes' runs; skipping stale ones),
`internal/config` (the soft cluster cap, off by default), `internal/api` (cluster spend),
the web spend view, `docs/keep-going.md`, `docs/cluster.md`.

- Done when the cluster's daily total, including the pending estimate and with a stale
  worker marked, is shown view-only. The soft-cap warning ships off and the person turns it
  on after a week of comparing the view with each daemon's own totals.
- Keep-going stops at its cap counting runs it started on any home, and says it skipped an
  offline one.

## What changed from the 2026-11-05 decisions

| # | 2026-11-05 decision | Now |
|---|---|---|
| 1 | Password and TOTP built in | Kept |
| 2 | A hosted daemon runs agents itself | Kept; it is also now the home of the repos assigned to it |
| 3 | Device token lifetime | Kept |
| 4 | The hub is a role of the same daemon | Kept, renamed: the controller is `cluster.role: controller` |
| 5 | Both transports: HTTPS link first, SSH later (phase 7) | **Changed**: the link only. SSH is dropped from the plan because a controller dialling a laptop contradicts "workers link outbound" and a work network |
| 6 | A member's own desk keeps working, behind the wake lease | **Changed**: kept and made central. The worker's desk is the offline desk; its turns are journaled and replay as one block; the lease lapses two minutes after the link is confirmed down |
| 7 | Soft cluster cap after a view-only week | Kept |
| 8 | The hub's items are bare, members' prefixed | Kept, restated: the controller's own things are bare, a home's are prefixed with the worker's name |
| 9 | Image registries | Kept |
| 10 | The carrier is WebSocket | Kept; its main reason is now outbound-only networks |
| 11 | A StatefulSet with one replica | Kept; in a single `shepherd` namespace |
| 12 | The TOTP key's source | Kept |
| 13 | Repo homes derived by origin, with `set-home` and `--also`, in phase 6 | **Changed**: the core rule (one home per repo, always), recorded with a generation, from phase 2, and moved only by an explicit handover (phase 8) |
| 14 | The soft cap view-only for a week | Kept |

Other changes, not in the table:

- **New:** the offline journal, ordering, cursor and replay; the folded block; the stale and
  refused rules; the privacy setting; the handover and its fence.
- **Changed:** catching up. Before, over 10 missed items posted one summary event; now a
  disconnect of over five minutes or more than 10 entries posts one block of everything,
  including the worker's own desk turns and answers, not only feed items.
- **Changed:** the feed cursor is now a journal cursor with an epoch, so a restore from
  backup is detectable.
- **Changed:** the plan is re-cut so a laptop shows up first (phase 1) and homes come second,
  not last.
- **Renamed:** hub to controller, member to worker, `shepherd member add` to `shepherd worker
  add`, `member_credentials` to `worker_credentials`.

## Open questions

Each has a recommendation; the person chooses.

**1. The name for a machine: worker, or something else?**

| Option | For | Against |
|---|---|---|
| **Worker** | The person's word; reads naturally ("workers link outbound") | Overlaps with the agent worker tokens and `shepherd mcp --worker` |
| Home | Pairs with "every repo has one home" | Overlaps with the Shepherd home directory, and the controller is a home too |
| Keep "member" | No rename | The person has moved on from it; "member of what" is vague |

Recommendation: **worker**, and call the existing credentials "agent worker tokens" in docs.
Revisit only if the two senses cause a real mistake.

**2. What does `summaries` let through for a decision?**

| Option | For | Against |
|---|---|---|
| **The prompt and its options, as the agent wrote them** | The person can answer from the controller, which is the use | An agent's question can quote code |
| A one-line title only, with "answer at the work laptop" | Nothing a diff could leak | The commuting person cannot answer, which defeats the main case |
| Per-worker choice of the two | Fits both | A second setting to explain |

Recommendation: the first, stated plainly on the setting's page, with the second available
later as the per-worker choice if someone needs it.

**3. Does the local desk's turn text replay in full mode?**

| Option | For | Against |
|---|---|---|
| **Yes, in the block, collapsed** | The main desk and the person know what was said | A lot of text for a long offline period |
| A summary only | Small | Loses the person's own words |

Recommendation: full text in `full` mode, collapsed by default, with the summary as the
block's headline.

**4. Should `shepherd chat` on a linked worker attach to the controller's thread?**

| Option | For | Against |
|---|---|---|
| **Yes, fall back to the local desk when the link is down** | One conversation, always; the person never wonders which desk they are in | A worker on a slow link feels slower |
| Always the local desk | Fast, simple | Two conversations exist while linked, the thing this design removes |

Recommendation: attach, with the fallback stated on screen.

**5. Journal retention.** Options: 7 days, **30 days and 50 000 entries (recommended)**, or
unbounded. A laptop off for a month is rare, and an honest gap marker beats a growing
database; a person who is away longer can raise the cap in config.

**6. How should `--force` handover treat a lost laptop's unpushed work?**

| Option | For | Against |
|---|---|---|
| **Allow it, with a warning and the fence** | A lost laptop must not trap the repo | The unpushed work is lost to the cluster |
| Refuse until the old home returns | Nothing lost | A broken laptop blocks the repo forever |

Recommendation: allow, and tell the person what is lost.

**7. Keep an SSH transport at all?** Options: **drop it for now (recommended)**,
keep it as a later phase, or keep it for a controller that can reach a server-class worker.
Nothing in the person's case needs it, and `--host` over ssh still works for one daemon.
Add it back only for a worker that cannot make outbound connections.

**8. The offline threshold for a folded block.** Options: **over five minutes or more than 10
entries (recommended)**, over one minute, or always. A flaky link should not make blocks all
day, and a real absence should read as one.
