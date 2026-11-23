# Planning rooms: a conversation on one topic that is all signal

Status: **proposed, not built.** For the maintainer's review before anything is built. The
shape in "The idea" was proposed by the front desk and accepted by the maintainer as the
starting point; everything else here is a recommendation. The questions that are still open
are at the end.

## The problem

Planning takes focus. A design conversation needs the person to hold a few facts in their
head, answer one question, see the answer change the picture, and answer the next. Today the
front desk has one thread for everything ([desk.md](../desk.md), [web-chat.md](../web-chat.md)),
and the thread is built for coordination, which is the opposite of focus:

- `[Shepherd]` events land in the middle of a design discussion: a run ended, a pipeline
  failed, a decision was asked in another lane.
- The desk's coordinator habits come with it: the "Needs you / Status" closing format, the
  repeated `[Waiting on you (N)]` line ([waiting-on-you.md](../waiting-on-you.md)), and a
  brief written for dispatching work, not for thinking about it.
- Questions pile up. The desk may ask three things in one reply, and the answers scroll
  away under the next event.
- What was decided is scattered through the turns. Nothing says "so far we have settled
  these four things, rejected these two, and still need to answer that one".

Topics ([topics.md](../topics.md)) name a matter and filter the thread to it. That helps
find a matter, but it is the same thread, the same session and the same brief, so the
noise that came from the desk and from the other matters' events still arrives. A **room** is
the next step: a separate conversation about one topic, with its own desk session, its own
brief, and a plan the conversation keeps up to date.

## The idea

| Part | What it is |
|---|---|
| Room | A separate conversation on one topic. It has its own desk session and a planning brief, not `DeskBrief`. It reads the topic's design docs once, at the start |
| Events | `[Shepherd]` events stay in the main thread. The room shows only events about its own topic's lanes, and one quiet "N updates in main" line for the rest |
| Format | No Needs-you or Status header. One question at a time |
| Plan panel | Beside the conversation, three lists: **decided**, **open questions**, and **rejected options with the reason**. It updates with each answer |
| Closing | Writes the plan as an artifact, or commits it to the design doc through a lane. Proposes Next-up items for the person to approve. Posts one summary line back to the main thread |
| Focus | While the person is in a room, only that room's questions interrupt. The rest wait in the Inbox |

A room is opt-in. Nothing about the main thread changes for a person who never opens one.

## How a room differs from the topic switcher

Next-up item 333 (the chat topic switcher, in flight) filters the main thread by topic. The
two are different tools for different needs and a room is built on the switcher, not beside it.

| | Topic switcher (item 333) | Room |
|---|---|---|
| What it is | A view over the one thread | A second conversation |
| Session | The one desk session | Its own desk session |
| Brief | `DeskBrief` | A planning brief |
| Who writes into it | The desk, from every matter, and Shepherd's wake-ups | The person and the room's desk; events about the topic's lanes appear as rows |
| Output | Nothing new: the filtered rows | A plan (decided, open, rejected), then an artifact, Next-up proposals and a summary line |
| Use | Find a matter, see it whole, clear it | Think a matter through |
| Cost | None | A session while it is open, below |

What a room reuses, so nothing is built twice:

- **The topic.** A room is created from a topic, or creates one. Its `topic_id` is how it
  finds its lanes, runs, decisions, requests, questions and Next-up items: the `topic_links`
  table of [topics.md](../topics.md) section 1, with no second link table.
- **The switcher's selection.** The set of events that belong to a topic is one function. The
  switcher uses it to filter the main thread; a room uses it to choose the event rows it
  shows. A room adds no rule of its own about which events belong.
- **The switcher's route pattern.** The switcher's filter is a hash parameter
  (`#/chat?topic=7`), linkable and kept across a reload. A room is a sibling route
  (`#/room/3`) with the same properties. A topic row in the switcher gains an **Open as room**
  action; a room's header links back to **Show in main thread** (the switcher's filter on the
  same topic).
- **The topic's lifecycle.** Clearing a topic ([topics.md](../topics.md) section 4) does not
  close its room, and closing a room does not clear its topic. The person does each.

If item 333 has not landed when the room's web phase starts, that phase waits for it. The
earlier phases touch only the store, the daemon and the CLI.

## Data model

### A room versus a topic

| Option | For | Against |
|---|---|---|
| A flag on `topics` (`is_room`) | One table; no join | A topic with no session would carry unused columns; the session, plan and cost are not about the topic itself; a topic can have a room opened, closed and opened again |
| **A `rooms` table that points at a topic** | A topic stays what it is (a name and links). A room is a session on a topic. Several rooms over time can share one topic, at most one open | One join |

**Recommendation: a `rooms` table.** Topics stay a filter and a fold over the one thread;
rooms are the only thing that adds a session, and they say so in their own table. This is a
deliberate, narrow exception to the first decision in [topics.md](../topics.md) ("a table
plus links, over the one thread and one session"), and to its out-of-scope line on a session
per topic: rooms are opt-in, explicit, bounded in number, and never created by the desk
alone. The build must amend that page.

### Tables

Three appended migrations, never edits.

| Table | Holds |
|---|---|
| `rooms` | `id`, `workspace_id`, `topic_id`, `status` (`open` or `closed`), `session` (the desk session's id), `model`, `docs` (the doc paths read at the start, as a list), `opened`, `closed_at`, `closed_by`, `summary` (the line posted to main), `plan_artifact` (an artifact id, 0 until close), `turns`. At most one open room per topic, enforced by a partial unique index on (`topic_id`) where `status = 'open'` |
| `room_plan` | `id`, `room_id`, `list` (`decided`, `open`, `rejected`), `text`, `reason` (for `rejected`, and optionally for `decided`), `by` (`desk` or `person`), `turn` (the turn that earned it), `question_id` (0 or the question whose answer produced it), `state` (`live` or `superseded`), `superseded_by`, `rank`, `created`, `updated` |
| `desk_events.room_id`, `desk_questions.room_id` | Two nullable columns, default 0 for the main thread |

A plan row is never deleted. Changing an entry supersedes it with a new row, so the panel can
show "changed from ..." and the closing artifact can say what moved.

### Where the room's conversation is kept

| Option | For | Against |
|---|---|---|
| A separate `room_events` table | No query on the main thread can leak a room's rows | Duplicates history, search, attachments, streaming, the turn file and redaction that `desk_events` already carries |
| **A `room_id` column on `desk_events`** | History, search, streaming, attachments and the turn-file all work as they do; one place to redact | Every read of the main thread must say `room_id = 0` |

**Recommendation: the column.** The risk is a read that forgets the filter, so the store
exposes the thread through one method that takes a `room_id` (0 for main), the existing
callers move to it in the same migration's MR, and a test lists every caller of the old
form. A room's rows are never returned from a main-thread read and the reverse.

### Session lifecycle

`desk.Manager` holds one `Desk` per workspace today, in a map keyed by workspace id. A room
needs its own queue and session, so the key becomes (workspace, room), with room 0 the main
desk. A room's `Desk` is the same type with a different brief, tool set and wake policy, so
rotation, interruption, the stream and turn recording are reused.

| Stage | What happens |
|---|---|
| Open | `shepherd room new <topic>` (or **Open as room**, or `/room new`). The daemon records the row with status `open`. No session starts yet and nothing is spent |
| First turn | The person's first message starts the session with the planning brief. Shepherd lists the doc paths (the topic's linked artifacts, the plan docs of its Next-up items, and any `--doc` given at open) in the brief, and the room's first action is to read them once. Reading is bounded by `room.docs_chars` (default 60000) so a long doc cannot blow the first turn |
| Turns | The person's messages, one turn each, resumed headless as the main desk is. The room never wakes by itself: no swarm event starts a turn. Events about the topic's lanes are shown as rows and handed to the session as one quiet digest at the start of its next turn |
| Leave | Closing the page or the terminal does not close the room. The session rests on disk; nothing runs and nothing is spent |
| Rotate | At a turn's end, past `desk.rotate_tokens` or `desk.rotate_cost`, as the main desk does. The seed is the plan (all three lists, with reasons), the open question if any, the doc paths, and the last turns verbatim. The docs are read again only if the plan names a point that needs them |
| Close | Ends the session, once, after the closing steps below |
| Reopen | A closed room can be opened again: a new session, seeded from the plan, with the closing artifact in its doc list |

A daemon that stopped mid-turn closes the room's open turn the way it does the main desk's
(`recover`).

### Cost

A room's turns cost like the main desk's, and the bill is per open room, so the design bounds
it:

- Turn costs are written as spend rows with source `room` and the room id, so they count in
  the day's budget (`daemon.budget`) with everything else. The room header shows its total
  and today's share.
- `desk.rooms_max` (default 3) is how many rooms may be open at once. A fourth is refused with
  the names of the others.
- A room's model is `desk.model` unless set at open. The first turn is the costly one (the
  docs); the planning brief and doc list are stable, so prompt caching applies to the rest.
- Rotation settings apply. An optional `desk.room_cost` warns once, in the room's header,
  when one room passes it. It never stops a turn; the daily budget is the hard stop, as for
  everything else.
- A room does not run agents, so it adds no run cost. It cannot open lanes (below).

## The planning brief

`RoomBrief` lives beside `DeskBrief` and `Brief` in `internal/desk`. In plain terms it tells
the room's desk:

- You are helping the person think one matter through, the topic named in the head. You are
  not coordinating work, and you cannot start it.
- Read the listed docs first, once. Then say in two lines what you understood and what the
  plan holds so far.
- Ask one question at a time. Give two or three options, name the trade-off of each, and
  recommend one. Open the question with `question_open`; wait for the answer before asking
  the next.
- Keep the plan with the plan tools. Put a point on **decided** only when the person has said
  it. Put an option on **rejected** with the person's reason, or the reason you gave and they
  accepted. Put what is still unanswered on **open questions**.
- Do not summarise events, status or the backlog. Do not write a Needs-you or Status block.
  If something outside the topic needs the person, say nothing: Shepherd shows one quiet line.
- When the open list is empty or the person says it is enough, propose closing and say what
  you would put on Next up. Do not close.

The daemon enforces the parts that must not rest on the brief:

| Rule | Enforced as |
|---|---|
| One question at a time | `question_open` in a room is refused while one of that room's questions is open, with the open question's text in the refusal |
| Decided and rejected come from the person | `plan_decide` and `plan_reject` are refused outside a turn the person started, and name the turn that earned the entry. The origin check is the one `decision_answer` and Next-up approval use |
| The room cannot start work | The room's operator tools (`shepherd mcp --scoped --room <id>`) leave out `lane_open`, `lane_run`, `run_continue`, `run_stop`, `request_route`, `decision_answer` and the `next_update` state changes. It has reads, the plan tools, `question_open` and `question_answer`, `artifact_list` and `artifact_read`, `topic_show`, and `next_add` for proposals at close |
| The room cannot edit | As the main desk: `--disallowedTools Edit Write Bash`, so the docs are read, not changed |
| Agents never approve their own work | Unchanged: an agent's token reaches no room tool |

### Plan tools

Each is a CLI command first, then its mapping in `internal/cli/mcp.go`, a client of the
daemon's routes, as every command is.

| CLI | Tool | Effect |
|---|---|---|
| `shepherd room plan [<room>] [--json]` | `plan_show` | Read the three lists |
| `shepherd room open-question <text>` | `plan_open` | Add to **open questions** (desk or person) |
| `shepherd room decide <text> [--question N] [--reason R]` | `plan_decide` | Add to **decided**, moving the linked question off **open** (person's turn) |
| `shepherd room reject <option> --reason R` | `plan_reject` | Add to **rejected** with the reason (person's turn) |
| `shepherd room revise <plan-id> <text>` | `plan_revise` | Supersede an entry; moving a decided entry off the list is the person's |

## How a room maps onto what exists

| In the room | Becomes | Notes |
|---|---|---|
| The room's question | A desk question ([desk-questions.md](../desk-questions.md)) with `room_id` set | Waits in Waiting on you as kind `question`, flagged "in room: title". Answering it, from the room's input, from the Inbox or in the terminal, is the person's reply to it, as today |
| An answer | A **decided** entry, and the question leaves **open questions** | One step, so the panel and the question never disagree. The entry quotes the answer's words |
| An option the person turns down | A **rejected** entry with the reason | The reason is the useful part: it stops the same option coming back |
| An agent's decision or request on one of the topic's lanes | Not a plan entry. It appears as a row in the room and in the panel's "Waiting" strip, answerable there, and counts as the room's for the focus rule | The desk still never answers one |
| A proposed piece of work | A Next-up item (`next_add`, state `planned`, shown as proposed) linked to the topic, created only when the person confirms the close | Approval stays the person's, as for every item ([desk.md](../desk.md)) |
| The finished plan | An artifact, or a committed doc, at close | See below |

### Closing

Closing is a step the person confirms in a modal, not a button that acts at once. It shows
what will happen, with a checkbox on each part, and then does it in this order:

1. **Render the plan.** Shepherd renders the markdown from the plan rows, not a model: a
   title, **Decided**, **Rejected (with reasons)**, **Open questions**, and a link per
   question to the turn that settled it. Deterministic, like the rotation summary, and
   passed through `internal/redact`.
2. **Write it down.** The default is an artifact owned by the room and shown wherever
   artifacts are listed in the web UI, linked to the topic. The other
   choice is to **commit it to a design doc through a lane**: the person names the repo and
   path, and Shepherd opens a lane with that one file as its scope and starts an agent with
   the rendered plan as its brief, so the change goes through the usual gate, merge request
   and review. The room's desk does not do this itself.
3. **Propose Next-up items.** The desk's proposals appear in the modal as an editable list
   (title, repo, note). Each ticked one is added as `planned` and linked to the topic. None
   is approved.
4. **Post one line to the main thread**: "Room closed: <title>. 4 decided, 2 rejected, 1
   open. Plan: <link>. 3 items proposed." It is one `room` feed item (a new closed kind), so
   the main desk and history carry it and it is the only trace of the room in the main thread.
5. **End the session.** The room's status becomes `closed`.

A room with open questions can be closed; they are carried into the artifact under **Open
questions** and the matching desk questions stay open in Waiting on you, now without a room.
Nothing the person was asked is lost by closing.

## Events and focus

### What the room shows

The room's event rows are the feed items about the topic's lanes, runs, decisions, requests
and Next-up items, chosen by the switcher's function (above). They are compact rows
(a glyph, a line), not turns, and they do not start a turn. Everything else is counted into
one quiet line at the foot of the conversation: "6 updates in main". It is faint and not a
button until the person hovers it; it opens the main thread. It is never lantern.

### What the main desk sees while a room is open

| Thing | Behaviour |
|---|---|
| Events | Unchanged. Every event still reaches the main thread and wakes the main desk by today's rules, so work keeps moving. A lane in the room's topic is still coordinated by the main desk |
| Head of each main turn | One line: "Room open on #7 (cluster topology): the person is planning it there; leave its questions to the room." Shepherd adds it from the `rooms` table |
| Questions about the topic | The main desk's `question_open` for a topic with an open room is refused with the room's name, so the person is not asked the same thing twice in two places |
| The room's plan | Readable by the main desk with `plan_show`, so "where are we on the cluster rework" is answerable. Not writable |
| Close | The summary line above, once |

The room's desk has the mirror rule: it never sees the main thread, only the topic's rows and
the quiet count.

### Focus: only the room's questions interrupt

"In a room" means a client is attached to that room's stream (`GET /v1/desk/stream?room=3`);
the daemon already tracks attached clients for `desk.wake`. The rule is about what clients
show, and nothing is hidden:

| Surface | In a room |
|---|---|
| The header badge | Shows the room's waiting count in the usual colour, and "N in Inbox" beside it, faint |
| ▲ and ▼ counter, `]` | Step through the room's waiting items only |
| Inbox items from elsewhere | Still counted and listed in the Inbox, with their ages. They are not pushed at the person |
| A new question elsewhere | Raises the faint Inbox count by one. No sound, no banner, no focus change |
| The main desk's replies | Go to the main thread only |
| A failure, a quota hold, an offline daemon | Still shown: red is for failures and these are never quiet. The footer and the offline banner behave as in every view |

Counts never hide: a person who leaves the room sees every item with its true age, and
leaving is one key (Esc, or "Leave room").

## The web UI

The layout is the usual one, sidebar, main and side panel, with the rules of CLAUDE.md. A new
component takes its class in `components.css` beside its siblings, a `data-component`
attribute, and shared parts from `web/src/ui/`, with only `--sh-*` tokens.

### Sidebar

A **Rooms** group under the chat entry. Each row is a room: the topic's title, a state
glyph and words ("● working" while a turn runs, "? waiting on you" when its question is open,
"○ idle"), and last activity in relative time. Open rooms first, then closed ones in a
collapsed "Closed (N)" group with **Reopen**. `j`, `k` and Enter work as in every list.
Selecting a row opens the room and never leaves the list. A **New room** action opens a
dialog (topic, optional docs, optional model), and the topic switcher's rows offer **Open as
room**.

### Main: the conversation

- A header with the title, the topic chip, the room's cost today, and **Show in main thread**
  and **Close room**.
- The thread is the room's rows only: the person's messages, the desk's replies in plain
  markdown, and the event rows (`▸` for runs, with their words). There is no Needs-you
  block, no Status block and no repeated waiting line.
- The open question is a card at the foot, above the composer, in lantern (it is waiting on
  the person) with its options. `1` to `9` pick an option, Cmd+Enter sends and moves on, and
  the composer takes free text for anything else. There is never more than one.
- The quiet line, "N updates in main", under the last row.
- Offline: the footer and a banner say so with the next try, Send is off, drafts stay.

### Side panel: the plan

The plan is a side panel, resizable, its width remembered per view (`room`), with **Open as
page** like every side panel. Main keeps at least 40 characters.

| Section | Glyph and words | Holds |
|---|---|---|
| Decided | `✓` decided | Entries, newest first, each with the quote or turn that earned it |
| Open questions | `?` open | What is still unanswered. The one being asked now carries the lantern mark; the rest are plain, since nothing waits on the person for them yet |
| Rejected | rejected (glyph: open question below) | The option and the reason on a sub-line |

Counts show at zero, faint. A row that just changed flashes once. `j`, `k` and Enter move
through the rows; Enter on an entry shows the turn that earned it in the conversation, and
the panel's actions are **Edit** and **Move to rejected** on a person's own entry (a change by
the person is recorded as theirs and told to the desk at its next turn). The panel never
saves by itself in a way that surprises: the person's edit is explicit, and the desk's
changes are shown as they land, each undoable for a short time.

The rules about a panel that answers ("the side panel is for answering, the main body for
reading") are bent here: the panel is a living reference beside the thread, as the backlog
rail already is, and the answer goes in main's composer. This is open question 7.

### Close dialog

A modal, since it confirms. It lists the five steps, each editable, and uses the person's
lantern commit button for **Close room**. Nothing in it is destructive, so the broken button
is not used.

### Both themes

Checked before the phase is called done: light and dark, with the card, the lists, the
quiet line and the dialog.

## The terminal chat

`shepherd chat` keeps one input and an inline thread printed to scrollback
([desk.md](../desk.md), `internal/chat`). A room is a mode of that model:

| Command | Effect |
|---|---|
| `shepherd chat --room <id\|title>` | Starts in a room |
| `/rooms` | Lists rooms, open first, with state |
| `/room new <topic>` | Opens a room on a topic and enters it |
| `/room <id\|title>` | Enters one; prints a divider, `-- room: cluster topology --` |
| `/room leave` | Returns to the main thread; prints a divider |
| `/plan` | Prints the three lists |
| `/room close` | Walks the closing steps as a confirmation list |

In a room the model reads the room's stream, replays the room's history on entry, and prints
only the room's rows. Scrollback is append-only, so what the main thread printed earlier stays
above the divider: the dividers say where each part belongs. The dock shows the plan's
counts (decided, open, rejected) and the open question as the attention item, with one faint
"N waiting in Inbox" line instead of the attention-sorted list. The searchable Ctrl-O
transcript is per room. The bell and any later desktop notification follow the focus rule.

## Hosted and clusters

[cluster.md](cluster.md) puts one desk, one thread and one set of topics on the hub, with members
as ordinary daemons linked to it. Rooms follow topics: **they live on the hub only.**

| Concern | Design |
|---|---|
| Where a room is | On the daemon that holds the desk. A member daemon can have rooms for a person at that machine, as it can have a desk, and the hub knows nothing of them |
| Ids | A hub room is `room 3`, bare, as the hub's own items are. Plan entries and links to a member's lane, run or decision are stored with the daemon id, the kind and the member's id, and shown as `build:fix-crash`, the form the cluster design defines |
| Transport | The room's routes are ordinary `/v1/...` calls and a stream, used by the web UI and the CLI as every client does. A browser logged in to a hosted daemon, and the CLI with `--host`, reach rooms with no new transport. The terminal chat over HTTPS waits on the cluster design's follow-up for `chat` over https, as the rest of the terminal chat does |
| Events from members | They reach the hub's feed tagged with the member, and the topic's lanes may be on a member: the room shows their rows with the member name. Cursors, dedup and the catch-up summary are the hub's, so a room never sees a duplicate; a catch-up summary event shows once, filtered to the topic |
| A member offline | The room goes on: the plan is on the hub. The quiet line names the member, and the member's rows and waiting items are stale with their time and have their actions off, as in the cluster design. Nothing is queued |
| Reading docs | The room reads the hub's own clones. A repo homed on a member is read through the read calls of the cluster design's phase 2 once they exist; until then such a doc is named in the plan and not read |
| Committing the plan at close | `lane_open` goes to the repo's home as the cluster design routes it, with the artifact as the lane's brief. If the home is offline the choice is offered again later as "Commit this plan" on the closed room; the artifact is already written |
| Spend | The room's spend is the hub's desk's spend and counts in the cluster view like any desk turn |
| Wake lease | A room never wakes a desk, so it does not touch the lease |

Nothing in this design stores a filesystem path in an API, a plan row or a feed item, so a
room on a hub is the same data as a room on a laptop.

## Worked example: the cluster topology rethink

The first real use is a rethink of the topology in [cluster.md](cluster.md). The sketch below is
what the room might hold after its first hour. It is an illustration of the mechanics, not a
decision: the person decides, in the room.

**Opening.** The person opens a topic, "Cluster topology", linked to the cluster design and
the hosted-mode page, and opens a room on it with
`shepherd room new "Cluster topology" --doc docs/design/cluster.md --doc docs/hosted.md`.
The first message is the framing: *every repo has one home, the controller is the
conversation, homes work offline and replay.* The room reads the two docs once and says, in
two lines, what it understood: the existing design has a hub that runs the desk and
members that own their repos, a home chosen per repo, and refused (not queued) actions on a
member that is away; the framing narrows the hub to the conversation.

**One question at a time.** The card shows:

> Today an action aimed at an offline home is refused, never queued. "Replay" could mean two
> different things. Which do you mean?
> 1. Replay events: the home keeps working alone and the controller catches up from a cursor
> (what the design already does). Recommended: nothing to build.
> 2. Replay intents: the controller records what the person asked while the home was away and
> sends it on return. Costs a rule for what is safe to send late.
> 3. Both, with intents limited to what is safe to send late.

The person picks 1 and adds "and a stop is never an intent". Two things happen at once:
**Decided** gains "Replay is events only: homes catch the controller up from a cursor. Actions
are refused while a home is away" (quoting the answer), and the next open question is asked.

**The plan after an hour:**

| List | Entries |
|---|---|
| Decided | Every repo has one home, chosen by its origin (kept from the cluster design). Replay is events only; actions to an away home are refused. The controller is the conversation: it holds the desk, the thread, topics, rooms and Waiting on you |
| Open questions | Does the controller hold a repo's leases and tag reservations, or does the home? (asked now) Does a repo with no reachable home refuse a new lane or ask the person to pick another? What must a home send on return that the cursor cannot carry? |
| Rejected | A shared store, because SQLite over a network filesystem is unsafe and a partition would stop every machine. Peer federation, because every daemon would run a desk and react to the same event, and agreeing on one thread is a consensus problem. Both from the existing design, carried over with their reasons so they stay rejected |

**While it runs.** A lane on the build machine fails its gate. If that lane is linked to the
topic, the room shows one row for it; if not, the quiet line goes from "2 updates in main" to
"3". The person's other questions wait in the Inbox, and the header says "2 in Inbox".

**Closing.** The person says the open list is down to two follow-ups they will do later. The
dialog shows the rendered plan, defaulting to an artifact, and offers a commit into
`docs/design/cluster.md`. They choose the commit: a lane opens with that file as its scope,
an agent revises the doc from the plan, and the change goes through review like any other.
Three Next-up items are proposed (revise the phased plan, a home-away test plan, a section on
what a home sends on return), none approved. The main thread gets one line:
"Room closed: Cluster topology. 3 decided, 2 rejected, 2 open. 3 items proposed."

## Answerable questions

A room asks one question at a time, and a question is only useful if the person can answer it
where it is asked. [chat-replies.md](chat-replies.md) describes asks as typed cards with
options, an "Other..." answer and one live card per ask. A room uses them unchanged: its
current question is one live card, and the plan panel's open list links to the cards. Rooms
add no answer path of their own.

## Phased plan

Each phase is one merge request, green alone, and each updates the user docs for what it
ships (`docs/rooms.md`, linked from the README's "Read next" table, and the amendment to
[topics.md](../topics.md) in phase 1). Per the repo's rule, nothing in `docs/` describes
more than has shipped.

### 1. Store, routes and CLI

Scope: `internal/store`, `internal/store/sqlite` (appended migrations), `internal/daemon`,
`internal/api`, `internal/client`, `internal/cli` (`shepherd room`), `docs/topics.md`,
`docs/rooms.md`.

- Done when `shepherd room new|list|show|close` works against the daemon, one open room per
  topic is refused as a second, and `desk.rooms_max` refuses a fourth with the others' names.
- The store's thread method takes a room id, every old caller uses it, and a test lists
  them: a room's rows never appear in a main-thread read nor the reverse.
- Plan rows can be added, revised and read through the routes, and the person-only rule for
  decided and rejected is tested with fake origins.
- A migration upgrade test for the two new columns.

### 2. The room's session

Scope: `internal/desk` (keyed desks, `RoomBrief`, room tools), `internal/daemon/desk.go`,
`internal/cli` (`mcp --room`, plan tools), `internal/config` (`desk.rooms_max`,
`room.docs_chars`, `desk.room_cost`).

- Done when a turn in a room runs with its own session and brief, reads the listed docs on its
  first turn within `room.docs_chars`, and records spend with source `room`.
- `question_open` is refused while a room question is open; `plan_decide` is refused in a turn
  Shepherd started; the room's tool list has no lane, run or decision-answer tool. Tested with
  the fake agent.
- No swarm event starts a room turn; the topic's events reach the session as one digest on
  the next person turn.
- Rotation seeds a new session from the plan and the open question. The main desk's head
  names an open room and refuses `question_open` for its topic.
- A real run of the room is a manual check before merging, as for any change to the desk.

### 3. The web room

Scope: `web/src/pages/Room.tsx` and its test, `web/src/model`, `web/src/shell/nav.ts`,
`web/src/router.ts`, `web/src/styles/components.css`, `web/src/api`, `docs/web-chat.md`.
Depends on item 333.

- Done when the Rooms group, the room page, the plan panel, the question card and the quiet
  line work, with `1` to `9`, `j`, `k`, Enter, `]`, Cmd+Enter and Esc as the UI rules say.
- The focus rule: in a room the header and `]` show only the room's items, "N in Inbox" is
  faint, and nothing is hidden when the person leaves.
- The style lint passes with no new allowlist entries; both themes checked; zero counts show
  faint; offline turns Send off and keeps drafts.

### 4. Closing

Scope: `internal/daemon` (render, close), `internal/store` (room-owned artifact), `internal/fold`
through the existing `lane_open` route for the commit choice, `internal/api`, `internal/cli`
(`shepherd room close`), `web/src` (the dialog), `docs/rooms.md`.

- Done when closing renders the plan deterministically, writes the artifact or opens the
  commit lane, adds only the ticked Next-up proposals (`planned`, unapproved), posts one
  `room` feed item to the main thread, and ends the session.
- Closing with open questions keeps the desk questions open, now without a room. Reopen starts
  a new session seeded from the plan.
- The rendered plan passes through `internal/redact`; a test plants a secret in a plan row.

### 5. The terminal room

Scope: `internal/chat`, `internal/cli`, `docs/chat.md`, `docs/rooms.md`.

- Done when `--room`, `/rooms`, `/room`, `/plan` and `/room close` work as above, history
  replays on entry, the dock shows the plan counts and the one question, and a fake stream
  test (as `chat_test.go` builds) covers entering, leaving and the dividers.

### 6. Hosted and members

Scope: `internal/daemon`, `internal/store`, `docs/rooms.md`, `docs/design/cluster.md`.
Depends on the cluster design's members' events (phase 3).

- Done when a room on a hub shows a member's lane events with the member name, an offline
  member's rows are stale with actions off, the plan stores a member's lane as daemon id,
  kind and id, and the commit-at-close choice opens its lane on the repo's home.

## Out of scope

- Agents in a room. A room plans; it opens no lane and starts no run.
- More than one person in a room.
- A room across workspaces, or across hub and members as one.
- Rooms started by the desk or Shepherd on their own. Only the person opens one.
- A room that closes itself, or clears its topic.
- A model-written summary of the room at close: the closing text is rendered from the plan.
- Editing the repo's docs from the room. That is a lane's work, started at close.

## Risks

- **A second thing to maintain.** A room asks the person to open and close it. Mitigation:
  the main thread works unchanged without one, and `desk.rooms_max` stops sprawl.
- **The plan drifts from the conversation.** The desk is a model and may record something
  the person did not say. Mitigation: decided and rejected need a turn the person started,
  each entry links to its turn, and the person can edit any of it.
- **A leaked room row in the main thread.** Mitigation: one thread method with a room id and
  a test of its callers.
- **Quiet is mistaken for lost.** Mitigation: the Inbox count is always shown, red alerts are
  never muted, and leaving is one key.
- **Cost.** An open room is a second long session. Mitigation: bounded count, docs cap,
  rotation, spend rows and a header total.

## Open questions

Each has options and a recommendation. None blocks phase 1 except the first.

1. **Is a session per topic acceptable here?** [topics.md](../topics.md) rules it out.
   Options: no rooms; rooms as an explicit, opt-in exception; rooms that replace topics.
   Recommendation: the explicit exception, with that page amended in phase 1.
2. **A room as its own table or a topic flag?** Options: `rooms` table; a flag on `topics`.
   Recommendation: the table, so a topic can have rooms over time and stays a plain filter.
3. **Where a room's turns are kept.** Options: a `room_id` column on `desk_events`; a
   separate table. Recommendation: the column, with the one-method guard against leaks.
4. **What closing writes by default.** Options: an artifact; a committed doc through a lane;
   ask each time. Recommendation: artifact by default, commit offered in the dialog, because
   committing changes a repo and starts an agent.
5. **Artifacts without a lane.** Today an artifact belongs to a lane and a run. Options: a
   `room_id` on artifacts with the content kept in the store's versions; a nullable lane; a
   file in a Shepherd-owned directory. Recommendation: a room-owned artifact, subject to
   checking how artifact versions are read; this decides the size of phase 4.
6. **How many rooms open at once.** Options: one; three; unlimited. Recommendation: three,
   settable with `desk.rooms_max`, since planning two matters in parallel is real but a long
   list of live sessions is not.
7. **The plan panel and the side-panel rule.** The UI rules say the side panel is for answering
   and main is for reading. The plan is a reference beside the thread. Options: the plan as
   the side panel with answers in main (this design); a plan tab in main with the side panel
   for the question. Recommendation: this design, as the backlog rail already sets the
   precedent; the maintainer owns the UI rules, so this is theirs to rule on.
8. **A glyph for rejected.** The set has no glyph for it, and red is for failures only.
   Options: a new glyph (for example `⊘`) added to the set; words only. Recommendation: words
   only in phase 3, with a glyph if the maintainer wants one.
9. **Who may edit the plan.** Options: the person only; the desk and the person, with the
   person's entries protected. Recommendation: both, as topics do (`by: person` wins), with
   the desk's decided and rejected entries needing a person's turn.
10. **Should the topic's events also reach the room's session.** Options: never (rows only);
    as a digest on the next person turn; wake the room. Recommendation: the digest, so the
    desk is not blind to a failed lane it is planning around, and never a wake, so the room
    stays quiet.
11. **The room's reach while the main desk coordinates the same lanes.** Options: the main
    desk keeps handling the topic's events and refuses to ask about it (this design); the
    room takes over the topic's lanes while open. Recommendation: this design, so no work
    stalls while the person thinks.
