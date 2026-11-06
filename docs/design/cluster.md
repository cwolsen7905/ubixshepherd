# A cluster of daemons behind one conversation: design

Status: proposed, for the maintainer's approval. Nothing here is built, and no code changes
with this document.

## Goal

The person runs a Shepherd daemon on more than one machine and talks to all of them through
one front desk: one thread, one Waiting on you, one feed of `[Shepherd]` events. Each
daemon keeps doing what it does today for the repos on its machine (lanes, runs, leases,
forge polling, gates); the conversation is what is shared.

## Non-goals

- Moving a run from one machine to another, or scheduling work onto whichever machine is
  free. A repo's work happens where the repo is.
- A shared or replicated store. Each daemon's store stays the only truth for its work.
- Opening a daemon to the network. Every daemon stays loopback-only, as today.
- Several people sharing one cluster. One person, their machines.
- A desk on every machine reacting to every event. There is one desk for the cluster.

## Use cases

| Case | Machines | What the person wants |
|---|---|---|
| Laptop and build box | A laptop daemon for everyday repos; a build box with UbixOS, whose builds need its toolchain and disk | Ask the laptop desk "how is the kernel lane doing", answer the build box's decisions in the laptop thread, start a run there without opening an ssh session |
| A daemon per machine, different repos | A work machine for client repos, a home server for uBixCore and its packs | One Waiting on you across both; one place to see spend |
| A long-running box | A small always-on server that keeps lanes moving overnight; the laptop sleeps | When the laptop wakes, the thread shows what the server did while it slept, in order |

## What exists today

| Piece | Where | What it gives a cluster |
|---|---|---|
| Loopback-only daemon | `daemon.listen` must be a loopback address (`internal/config`) | No listener to secure; a cluster must reach members another way |
| Remote CLI | `--host`, `SHEPHERD_HOST`, `~/.shepherd/hosts.yaml` (`internal/cli/host.go`) | Any command can be aimed at another machine's daemon |
| SSH transport | `internal/remote` (`Transport`, `SSH`, `Endpoint`) | Reads the remote runtime file over ssh, opens a `-L` tunnel, redials when the daemon moves. `Transport` is the seam for a second transport |
| Tokens and roles | `internal/daemon/auth.go` | Operator token in the runtime file; worker and desk tokens minted in memory, revoked by a restart. Crook sees a desk token as the person's only in a turn the person started |
| The desk | `internal/desk`, `internal/daemon/desk.go` | One Claude Code session per workspace, woken by feed items, rotated by size or cost, with operator tools from `shepherd mcp` |
| The feed and its hub | `feed` table, `internal/daemon/feedhub.go`, `GET /v1/feed`, `/v1/feed/stream` | A monotonic id per item, polled every 500 ms to wake the desk and the streams |
| Desk thread | `desk_events`, `/v1/desk/history`, `/v1/desk/stream` | The thread is stored per workspace by the daemon that runs the desk |
| Waiting on you | `GET /v1/waiting` (`internal/waiting`), the web Inbox page | Derived on each read, nothing stored |
| Topics, questions, next-up, keep-going | `topics`, `desk_questions`, `next_items`, `keep_going` tables | All per workspace, keyed by local integer ids |
| Spend | `spend`, `daemon.budget`, `budget_cap` | A daily cap per daemon |

So one person can already reach every daemon from a terminal, one command at a time. What is
missing is a daemon that does this on the person's behalf, continuously, and a thread that
holds what it learns.

## Topology

| Option | How it works | For | Against |
|---|---|---|---|
| **Hub and members** | One daemon (the hub) runs the desk and the person's clients. It follows each member's feed and calls each member's API through the existing transport. Members are ordinary daemons | Reuses `internal/remote` and the HTTP API unchanged; each store stays authoritative; a member works alone when the hub is away; one desk, one session, one spend line for it | The hub is a single place to talk from; if it is down, the person uses a member directly (as today) |
| Peer federation | Every daemon follows every other and runs its own desk over all of them | No special daemon | N desks react to each event and spend N times; conflicting actions; N-squared links; agreeing on one thread is a consensus problem |
| Shared store | All daemons write one database | One truth, no merging | SQLite over a network filesystem is unsafe, so it needs a database server; breaks "the daemon owns the store"; a partition stops every machine; worktrees and processes are still per machine, so the store would describe things it cannot touch |

**Recommendation: hub and members.** It is the smallest step from what exists (the hub does
what `--host` does, from inside the daemon), it keeps every write on the daemon that owns the
work, and partitions degrade to "the hub cannot see a member yet", never to two machines
disagreeing about a lane. Any daemon can be the hub; the person chooses one in config. A
member needs no setting at all for the read-only phases.

## Identity and naming

Each daemon gets a **daemon id** (a random UUID written to its home on first start, shown in
`GET /v1/status`) and the hub knows each member by a **member name**: its `hosts.yaml` key,
short and readable (`build`, `home`). The hub records the id the first time it reaches a
member and refuses a member whose id has changed (a reinstall, or a name now pointing at
another machine) until the person confirms it.

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

**Phase one uses what is there.** The hub daemon dials each member with `remote.SSH`, sharing
one connection per member (`control: true`), and keeps the tunnel up, redialling with backoff.
This needs non-interactive ssh from the hub's OS user to the member (keys or an agent; ssh
already runs with `BatchMode`). Nothing new listens anywhere.

**What it costs in trust.** Over ssh the hub reads the member's runtime file, which holds the
operator token, so the hub can do anything the person can do on that member. That is the
same trust `--host` already has, and it is acceptable for reads. For actions it is too much:
a desk turn the person did not start must not answer a decision on a member.

**What is needed for actions.** A member mints a scoped token for the hub on request: the hub
calls a new operator-only route with the turn's facts (desk role, whether a person started
the turn, the turn id), and the member returns a desk token with those facts, held only in
its memory like today's desk tokens. Crook on the member then sees the right actor, and a
member's restart revokes the token as it does every minted token. The operator token is used
only to mint, never to act.

**Later, optional: direct TLS.** A second `Transport` for machines with no ssh between them:
mutual TLS with a certificate pinned per member, the private keys kept outside config files
and outside any repo. It is listed as an open question, not a phase.

## The one conversation

| Surface | How it merges |
|---|---|
| Thread | Stays on the hub: one desk session, one `desk_events` history. Member events enter it as `[Shepherd]` events that name their member ("on build: run 42 failed its gate") |
| `[Shepherd]` events | The hub follows each member's feed from a stored cursor (last feed id per member, in a new hub table) and feeds them to its feed hub as if local, tagged with the member. The desk wakes on them by the same rules as local items |
| Waiting on you and the Inbox | `GET /v1/waiting` on the hub returns its own items plus each member's, each with a `member` field. Counts are per member and in total; zero stays visible and faint |
| Topics, keep-going | The hub's, since they belong to the conversation |
| A member's own desk | Still there for someone at that machine, but it does not wake for swarm events while the hub is following its feed (a lease the hub renews by reading; lapsed after a few minutes, the member's desk wakes again). This stops two desks reacting to one event |

**Ordering.** Within one member, items keep their feed order (ids are monotonic). Across
members there is no total order: the thread shows events in the order the hub received them,
with each member's own timestamp, and the hub warns once if a member's clock is more than a
minute off. Claiming a global order would need clocks the design should not depend on.

**Dedup.** A merged item is keyed by (daemon id, feed id). Rereading after a reconnect, or
two tunnels racing during a redial, cannot add an event twice. A redial resumes from the
cursor, so nothing between the last read and the outage is lost.

**A member offline.** The hub shows it in status, the thread gets one event when it goes away
and one when it is back (not one per retry), and the web footer names it with the next try.
Its last Waiting on you items stay listed, marked stale with the time they were read, and
their actions are off. Actions aimed at it are refused with a plain message, never queued: a
queued "stop run 42" that fires an hour later would surprise the person. On return, the
hub catches up from the cursor and the desk gets one summary event rather than a burst,
when more than a handful arrived.

**Answering.** A decision answered in the hub's thread or Inbox is sent to its member with
the person's words and a person-started desk token. The member delivers it to the asking run
exactly as today (`Runner.Answer`). The hub never answers one on its own.

## Leases, spend, keep-going and merge rules

| Concern | Per daemon or cluster | Rule |
|---|---|---|
| Scope leases, tag reservations | Per daemon | A repo has one **home** daemon in the cluster: the hub refuses to follow two members that register the same origin remote, until the person picks one. Opening a lane goes to the repo's home. Leases and reservations then stay correct with no cross-machine locking |
| Spend caps | Per daemon, plus a cluster view | Each daemon's `daemon.budget` stays the hard cap it enforces itself. The hub shows the cluster's total and may carry a cluster cap that is soft (warns) in the first phases |
| Keep-going | The hub's | The mode is the hub's desk's. Its cap counts the cost of every run it started, on any member, which the hub tracks by (member, run id). Each member still applies its own limits and `Allow` gate to what the hub asks of it |
| Merge rules, autonomy | Per repo | Unchanged: read from the repo's base branch and evaluated by its home daemon. The hub shows "Merged by rule <name>" as the member reports it |
| `max_runs`, quota holds | Per daemon | They describe a machine's capacity and an agent CLI's account on that machine |

## Failure modes

| Failure | What happens |
|---|---|
| Partition between hub and member | The member carries on: runs finish, the watcher polls, decisions wait on it. Its desk takes the wake-ups once the hub's lease lapses. The hub marks it offline, keeps the stale view, refuses actions, and catches up from its cursor on return |
| Member restarts mid-run | As today on that member: agents left by a crashed daemon are stopped and their runs recorded as interrupted. The runtime file has a new address and token, so the hub's `Dial` redials and mints new scoped tokens. The interrupted runs reach the thread as ordinary events |
| Hub restarts | Cursors are stored, so following resumes where it stopped; the desk session resumes as it does now. Scoped tokens it held on members are dropped and minted again |
| Member reinstalled or renamed | The daemon id changes; the hub stops following it and asks the person to confirm, so items from two different stores never share a name |
| Feed pruned on a member before the hub caught up | The hub sees a gap between its cursor and the oldest item, says so once in the thread, and resumes from the oldest |
| Version skew | Each daemon reports its version and an API level with a list of capabilities. The hub uses only what a member reports, says which features are off for it ("build is on 0.9: answering decisions from here needs 0.10"), and never fails a whole read because one member is old |
| Two hubs configured by mistake | The member's wake lease names its holder; a second hub can read but is told another hub holds the lease, and the person is warned |

## Phased plan

Each phase is one merge request, and each is useful alone.

### 1. Read a member's status from the desk

Scope: `internal/config` (a `cluster:` section listing member names from `hosts.yaml`),
`internal/daemon` (a `cluster.go` that keeps a tunnel per member; daemon id in the home and
in status), `internal/api` (`GET /v1/cluster`), `internal/client`, `internal/cli`
(`shepherd cluster status`, the `cluster_status` MCP tool), `docs/cluster.md`,
`docs/config.md`.

- Done when the hub's `GET /v1/cluster` lists each member with reachable or not, version,
  daemon id, workspaces, runs going and waiting counts; the desk answers "what is running on
  build" with that tool; tests drive two daemons through the fake ssh `Runner`, including a
  member that is down and one whose id changed.

### 2. Read-only desk tools across members

Scope: `internal/cli/mcp.go` (a `host` argument and prefixed ids on `lane_list`,
`run_list`, `run_status`, `decision_list`, `question_list`, `next_list`, `lane_diff`),
`internal/daemon` (proxying those reads to a member), `internal/desk/claude.go` (one brief
line on member names), `docs/cluster.md`.

- Done when the desk can list and read lanes, runs and decisions on a named member, a read
  with no `host` covers every member with each row marked, and an offline member gives one
  plain line in the answer, not an error for the whole read.

### 3. Members' events in the thread

Scope: `internal/store/sqlite` (an appended migration for member cursors), `internal/daemon`
(`feedhub.go` reads members' feeds; the wake lease on members), `internal/api` (member on
feed items), `internal/desk` (event text names the member), `web/src/model/feed.ts`, the
terminal chat's event rendering, `docs/cluster.md`.

- Done when a member's run outcome appears once in the hub's thread with its member name,
  the hub's desk wakes for it and the member's desk does not, a hub restart or a dropped
  tunnel neither loses nor repeats an item, and the member's desk wakes again after the
  lease lapses.

### 4. One Waiting on you

Scope: `internal/waiting`, `internal/api/waiting.go`, `internal/daemon`, `web/src/pages/Inbox.tsx`,
`web/src/model/backlog.ts`, the terminal dock, `docs/waiting-on-you.md`, `docs/cluster.md`.

- Done when the Inbox and the dock list every member's items with a member chip, counts per
  member stay visible at zero, an offline member's items show stale with their time and no
  actions, and the footer names the member with its next try. Both themes checked.

### 5. Acting on a member

Scope: `internal/daemon/auth.go` (the hub token route on members), `internal/daemon` (proxied
actions), `internal/cli/mcp.go` (`host` on `lane_open`, `lane_run`, `run_continue`,
`run_stop`, `decision_answer`, `question_answer`, `next_add`, `request_close`),
`web/src/pages/Inbox.tsx` (answer in place), `docs/cluster.md`.

- Done when a decision answered in the hub's thread reaches the asking run on its member;
  Crook on the member records the person as the actor only for a person-started turn and the
  desk otherwise; an action on an offline member is refused, not queued; a member restart
  revokes the hub's tokens and the next action mints new ones.

### 6. Repo homes and cluster spend

Scope: `internal/daemon` (repo homes by origin; routing `lane_open` to the home), `internal/daemon/keepgoing.go`
(counting members' runs), `internal/api` (cluster spend), the web spend view,
`docs/keep-going.md`, `docs/cluster.md`.

- Done when the hub refuses to follow a repo registered on two members until the person
  picks one, `lane_open` without `host` opens on the repo's home, the cluster's daily total
  is shown with a soft cap that warns, and keep-going stops at its cap counting runs it
  started on members.

## Open questions

| # | Question | Options | Recommendation |
|---|---|---|---|
| 1 | Who is the hub | A config choice on one daemon / any daemon a client happens to open | A config choice (`cluster.members` on the hub). One hub keeps one thread and one desk spend line |
| 2 | Does a member's own desk keep working | Off while in a cluster / on, with the wake lease / always on | On, with the lease: someone at the build box can still talk to it, and two desks never both react |
| 3 | A cluster-wide hard spend cap | Soft only / hard, enforced by the hub telling members to hold | Soft first (phase 6); hard later if the soft one proves not enough, since a hard cap needs members to obey the hub |
| 4 | The hub's power on members | Operator token for everything / operator only to mint, scoped tokens to act | Mint, then scoped tokens (phase 5): the hub's desk must not act as the person in turns the person did not start |
| 5 | A transport without ssh | SSH only / add mutual TLS later | SSH only for this plan. Revisit if a machine the person wants cannot take ssh |
| 6 | Hub's items unprefixed | Bare for the hub's own, prefixed for members / prefix everything | Bare for the hub's own: nothing changes for a single daemon, and the hub is where the person sits |
| 7 | Where member names come from | `hosts.yaml` keys / a separate list with its own names | `hosts.yaml` keys: one name per machine for both `--host` and the cluster |
