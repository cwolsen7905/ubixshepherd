# Offline, the journal and replay, privacy and handover

Part of the [cluster design](../cluster.md). This page is the detail behind rules 4, 5, 7
and 8 of the core model: what a worker records while it cannot reach the controller, how that
reaches the one conversation when it can, what a worker with a privacy setting sends, and how a
repo changes home. Names: the **controller** is the daemon deployed in Kubernetes (or
anywhere always on); a **worker** is a daemon on a laptop or other machine that links out to
it; a repo's **home** is the one daemon, a worker or the controller itself, that owns it.

## What a worker is while offline

A worker whose link is down is a full Shepherd. It runs lanes, leases, runs, the forge
watcher and gates for its own repos exactly as a daemon with no controller does, and it has
its own desk: `shepherd chat` on that machine talks to the worker's local desk, woken by the
worker's own feed. Nothing in the worker waits on the controller. That is what makes the
journal safe: the worker is the only writer of its repos' state, so what it replays is
history, never a competing edit.

While the link is up, the controller's desk holds the **wake lease** and the worker's local
desk does not wake for events (two desks never react to one event). The lease lapses two
minutes after the link is confirmed down (two missed pongs), so the local desk takes over
only once the controller has clearly gone. `shepherd chat` on a linked worker attaches to the
controller's thread as a client; when the link is down it says "controller unreachable, using
the local desk" and opens the local one.

## The journal

The journal is a table on the worker (`journal`, an appended migration), written in the same
transaction as the thing it records, so an entry exists if and only if the change does.

| Field | Meaning |
|---|---|
| `epoch` | A random id minted when the table is created. It changes only through a rollback reset (below) |
| `seq` | A gapless, monotonic integer per worker, starting at 1. The total order of the worker's history |
| `at` | The worker's wall time. Shown, never used for ordering |
| `kind` | A closed set: `feed` (a feed item, by reference to its id), `turn` (a local desk turn: who spoke, the text or its summary), `answer` (a decision or question answered at the worker), `next_add` (a Next-up item added at the worker), `state` (a snapshot of the worker's lanes, runs and counts, written when the link drops and at intervals while it is down) |
| `body` | The payload, passed through `internal/redact` before it is stored, and filtered by the home's privacy setting before it is sent |

The journal is written whether or not a controller is configured or reachable, and costs a
row per event. It is bounded: entries older than 30 days that the controller has acknowledged
are pruned, and a worker that has never linked prunes at 30 days regardless. An
unacknowledged entry is kept until the cap (50 000 entries); beyond it the oldest go and a
`gap` marker records how many, so the controller says "events were dropped on the work laptop
while offline" instead of pretending the record is whole.

### Ordering

`seq` is the order within one worker, and replay preserves it exactly: the controller applies
entries in `seq` order and never out of it. Across workers there is no shared order and none
is invented. The thread sorts by the time the controller **received** each block, as in the
earlier design, so replay never reorders what the person has already read. A folded block
(below) sits at the time of reconnection; the worker's own times are shown inside it. A
worker whose clock is more than a minute off the controller's is warned about once, and
nothing is held or refused because of it.

### Idempotency and cursors

The controller keeps one cursor per worker: `(daemon id, epoch, acked_seq)`, in the same
transaction as the thread events the entries produced.

1. On connect, the handshake returns the controller's cursor for that worker. The worker
   sends from `acked_seq + 1`.
2. Entries travel in batches of at most 200 as `(epoch, seq_from, seq_to, entries)`.
3. The controller applies a batch only if `seq_from == acked_seq + 1` and the epoch matches.
   It commits the thread events and the new cursor together, then acknowledges the high-water
   `seq`. A batch that starts at or before the cursor is acknowledged without being applied
   (a resend after a lost ack); one that starts after it asks for a resend from the cursor.
4. The worker prunes only what is acknowledged, and resends everything after the cursor on
   every reconnect, so a link that dies mid-batch loses nothing and repeats nothing.

Because application is strictly in order and gated on the cursor, two links racing during a
redial cannot add an entry twice: the second batch finds the cursor already past it. The
dedup key (daemon id, epoch, seq) is also stored on each thread event, as a backstop.

**Rollback reset.** If a worker is restored from a backup, its `seq` can be lower than the
controller's cursor, and its entries reuse numbers the controller has already applied. The
handshake reports the worker's last `seq`; when it is below the cursor, the controller does
not apply anything. It says so once in the thread ("the work laptop's record is older than
what I have: restored from a backup?"), and on the person's confirmation sets a new epoch
(sent to the worker, which stamps it on later entries) and a cursor at the worker's last
`seq`. The controller's thread keeps what it had. The entries between were lost with the
backup and nothing claims otherwise.

**Journal pruned before the controller caught up.** The same as a gap marker: the controller
notes the gap once and resumes from the oldest entry the worker holds.

### What the main desk sees

When a worker was offline long enough to matter (the link was down for more than five
minutes, or more than 10 entries are waiting; a shorter blip replays as ordinary events), the
controller does not stream the replay into the thread entry by entry. It posts **one folded
block** at the time of reconnection:

> Work laptop, offline 2h: 3 turns, 14 events

The block expands to its entries, in `seq` order, each with the worker's own time. It carries
a short structured summary the controller's desk reads in one wake-up: counts by kind, runs
that ended and how, decisions opened and **answered at the worker** (so the Waiting on you
list closes them), lanes opened or closed, spend during the gap, and anything still waiting
on the person. The desk is woken once for the block, not per entry, with the standing
instruction that the block is history: it reports it to the person and acts only on what is
still open. The desk does not re-answer a decision the block shows as answered, and cannot
anyway: it never answers a decision, and the home refuses a second answer to one already
answered.

Local desk turns are in the block as the person's words and the local desk's replies, so the
main desk knows what was said to the home's desk while the link was down, in the same
account the person would give. Under the summaries-only setting a turn is its one-line
summary and no text.

### What the controller shows while a worker is offline

| Surface | Offline behaviour |
|---|---|
| Status, thread | One event when the worker goes away and one when it returns (not one per retry); the web footer names it |
| Waiting on you, lanes, runs | The last-known state from the worker's last `state` entry and feed items, each marked **stale** with the time it was read; their actions are off |
| Actions on its repos | Refused with a plain message ("the work laptop is offline; it last reported at 17:42"), never queued: a queued "stop run 42" that fires an hour later would surprise the person |
| Next up for its repos | Still plannable. Next-up items are the controller's records; adding or editing one for an offline worker's repo is allowed. Starting one is an action, so it waits for the worker. On reconnect they are offered to the worker as proposed items, never started by themselves |
| Keep-going | Skips a stale home and says so; it never sends work to an offline worker |
| Spend | The worker's total as last reported, marked stale |

A Next-up item added on the worker itself while offline is a `next_add` journal entry, and the
controller imports it under (worker, local id) so ids never collide.

### Races at the edge of a disconnect

The only place a controller-side action and a worker-side event can meet is a request in
flight when the link drops. The rules:

- An action is a request with an id, acknowledged by the home after it commits. No
  acknowledgement means "not delivered": the controller shows that, does not retry on
  reconnect, and the person asks again if they still want it.
- The home applies an action to a decision or run idempotently by that object's id and
  state: an answer to an answered decision, or a stop of a stopped run, is refused with the
  current state, never applied twice. So a request that did arrive but whose ack was lost
  does no harm when the person repeats it.

There are no write conflicts because there is one writer: the home. The controller never
edits a repo's lane state; it asks.

## Runs while a laptop is closed

A closed laptop's runs pause with the machine. Nothing in the design keeps a laptop awake,
and it is not Shepherd's place to. When the machine wakes, runs the operating system left
alone continue, and any whose process was killed are recorded interrupted by the same path a
crashed daemon uses today. Only repos homed on the controller run around the clock. A person
who wants a repo to work overnight gives it to the controller (a handover) and accepts that
its agent then needs to run on the pod, with the pod's egress and credentials.

## The privacy setting

A per-worker setting, `cluster.share`, with two values:

| Value | The controller receives | Stays on the worker |
|---|---|---|
| `full` (default) | Everything the link carries: events, decisions with their context, diffs and log excerpts on request, turn text | Nothing is withheld by the setting |
| `summaries` | Counts, lane and run names and states, one-line outcomes, costs, decision prompts and their options (the person must be able to answer from the controller), and turn summaries | Diffs, file paths beyond the repo name, run logs and artifacts, and the text of local desk turns |

The use is a work laptop whose code must not reach a personal controller or its model
provider. The rules that make it hold:

- **The worker filters, not the controller.** The filter runs on the worker before an entry
  is sent, so a controller that is compromised or misconfigured cannot ask for more. A read
  RPC for a diff, log or artifact is answered with "kept on the work laptop" and nothing
  else.
- **One-line summaries are written on the worker** from its own redacted data, by rule
  (kind, lane, outcome, cost, never free text from a diff), not by a model call that could
  carry code across.
- **Decision text is the exception that needs thought.** An agent's question can quote code.
  In `summaries` mode the prompt is sent as the agent wrote it, because answering from
  the controller is the point; the setting says so, and a person who wants it stricter
  answers at the laptop (the controller shows "answer at the work laptop"). This is an open
  question below.
- Changing the setting takes effect for entries not yet sent; what the controller already
  holds stays, and the setting's page says so. Switching to `summaries` on a worker that is
  mid-replay filters the unsent remainder.
- The setting is per worker, so a work laptop can be `summaries` and a home laptop `full`
  against the same controller.

## Moving a repo's home

A repo has exactly one home, recorded on the controller with a **generation**: the home's
claim is `(origin, home, gen)`, and `gen` increases on every change. A handover is explicit:

`shepherd cluster move-home <origin> <to-worker>` (also in Settings), which the controller
runs as these steps, each confirmed by the home that does it:

| Step | By | Check |
|---|---|---|
| 1. Ask the old home to quiesce | Old home | No run on the repo is going (the person stops them or waits), no lane has unpushed or unpublished commits |
| 2. Push branches and close lanes | Old home, through its own lane commands | Every open lane's branch is on the origin (verified with `git ls-remote`), each lane closed, its lease released. Pending decisions for the repo are answered or closed first |
| 3. Switch | Controller | In one transaction, `gen` goes up by one and the home becomes the new worker. From here the old home's claim is stale |
| 4. Take it up | New home | Fetch or clone the origin, register the repo, accept the claim. Lanes can open there. The old home keeps its history of the repo, read-only, and the controller keeps the folded events in the thread |

The generation is the fence. A home whose generation is lower than the controller's is told
so on its next handshake and treats that repo as read-only. This matters when a laptop was
off during the move and wakes up thinking it still owns the repo.

If the old home cannot be reached (a lost or broken laptop), `move-home --force` takes the
repo over after a warning that the old home's unpushed lanes are lost to this cluster. The
generation still goes up, so if the old laptop returns it finds out it no longer owns the
repo, and its unpushed work stays on its disk for the person to push by hand.

A repo that has no home yet takes the home of the first daemon that registers it. When a second
daemon registers the same origin (compared with `workspace.NormalizeRemote`), it is refused
for lane work and shown as a read-only clone until a handover or `--also` marks it as an
intended second clone.
