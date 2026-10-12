# Topics: naming what the thread is about, and clearing what is settled

Status: **proposed**, approved in outline by the maintainer (decision 85, 2026-10-11) with
the defaults in the Decisions table. Nothing is built. Implementation is four merge
requests, each its own lane (section 7).

## The problem

The front desk's thread is one conversation per workspace, and that is right: one place
to look, one session, one history ([desk.md](desk.md), [web-chat.md](web-chat.md)). But
the thread has no sense of what it is *about*. Each row is tied to a run, a lane or a
decision, never to the matter being discussed. In one evening the thread carried a
release and its tag, an announcement post, keep-going, soft approvals, a configuration
change, where to publish teaching material, and a manual. Each was spread across many
turns, runs and decisions, interleaved with the others, with nothing tying them together.

Three things follow:

- **No way to see one matter whole.** "Where are we on the release?" means scrolling and
  recognising rows by eye.
- **No way to put a settled matter away.** The thread only grows; the backlog rail
  ([web-chat.md](web-chat.md)) shows live work, not conversations, so a finished
  discussion keeps its place among live ones.
- **No indication of what waits in which matter.** A decision, an approval and a failed
  pipeline in three unrelated matters look the same.

The person asked for a tab on the right that tracks the different things being discussed,
so some can be cleared from the thread, while keeping one thread. This design adds
**topics**: a name for a matter, a link from everything that belongs to it, a rail tab to
see them, and a person-only "clear" that folds a topic out of the main thread without
touching the work.

A topic is a filter and a fold over the one thread. It is never a second thread, a second
session or a second desk.

## Decisions

Approved with the recommendations below.

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | What a topic is | tags on events only / a store table with links / a desk session per topic | A table plus links, over the one thread and one session. A session per topic breaks the one-thread principle and multiplies cost |
| 2 | Who assigns | the desk only / Shepherd only / desk, Shepherd and the person | All three. The desk creates and tags; Shepherd tags by existing links; only the person renames, merges, moves, clears |
| 3 | Where it shows | a page / a rail tab / both | A third rail toggle beside Now and Next up, filtering the thread; the terminal gets a dock section and slash commands |
| 4 | What "clear" does | delete / archive out of the thread / mute | Fold out of the main thread and keep in history and search. Anything that needs the person is never folded |
| 5 | Who may clear | anyone / the person only | The person only, the rule `decision_answer` follows |
| 6 | Existing history | leave untagged / backfill automatically / backfill proposed by the desk | A one-batch proposal from the desk that the person confirms; never automatic |

## 1. What a topic is and where it lives

Two tables in the store, an appended migration:

| Table | Holds |
|---|---|
| `topics` | `id`, `workspace_id`, `title`, `status` (`open` or `cleared`), `summary` (a line, optional), `source` (`desk`, `person`, `backfill`), `created`, `updated`, `cleared_at`, `merged_into` (0 unless merged) |
| `topic_links` | `topic_id`, `kind`, `ref`, `by` (`desk`, `shepherd`, `person`), `created`; unique on (`kind`, `ref`) |

`kind` is one of `turn` (a desk event's `turn` id), `feed` (a feed row), `next_item`,
`lane`, `run`, `decision` and `request`. The unique key means one thing belongs to one
topic at a time; moving it is an update, not a second link. Anything without a link is in
**General**, which is not a stored topic but the absence of one: it is never cleared and
never blocks anything.

A topic's state is derived, never stored, as with Next-up's `started`: its last activity
is the newest of its links' own times, and what waits on the person is counted from
the linked decisions, requests and approvals at read time, so it cannot go stale.

Merging sets `merged_into` and repoints the links; the old id keeps resolving to the new
topic so a link already pasted still works.

## 2. How topics are created and assigned

| By | How | May |
|---|---|---|
| The desk | `topic_set` on a turn: an existing `id`, or a new `title`. It names the topic of the turn being answered and of the lanes, items, runs and decisions that turn touches | create, tag, and describe (`summary`). Not rename, merge, move or clear |
| Shepherd | When an event arrives for a lane, run, decision, request or Next-up item that is already linked, the event's feed row gets the same topic. A turn's tool calls link the lane or item they open or touch to the turn's topic | tag by an existing link only; it never invents a topic and never reassigns |
| The person | CLI, web and terminal: create, rename, merge two, move a message or an item, split, clear and reopen | everything |

Rules the daemon enforces, not the brief:

- `topic_set` outside a turn the person started is allowed to *tag into an existing
  topic* and to create one (an event turn can name the matter it concerns), and is
  refused for rename, merge, move, clear and reopen. The origin check is the one
  `decision_answer` and Next-up approval use ([backlog-next.md](backlog-next.md)).
- An agent's token reaches no topic tool. Agents do not know topics exist; their lane
  and run are linked for them.
- A link set by the person is never overwritten by the desk or Shepherd (`by: person`
  wins), so a move sticks.
- One new topic per turn at most from the desk, and a title is capped at 80 characters,
  so a confused model cannot flood the rail.

The desk's brief gets one paragraph: name the matter of each person turn with `topic_set`,
prefer an existing topic over a new one, and use the topic list (`topic_list`) it is
given in the head of each turn. The list in the head is the open topics only, newest
first, capped at twelve, each a line: id, title, what waits.

If the desk does not call `topic_set`, nothing breaks: the turn is in General or inherits
from the links of what it touched.

## 3. How it shows

### Web (the Topics tab)

The chat rail's header gains a third toggle, **Now / Next up / Topics**, stored as
`shepherd.chat.rail` (which gains the value `topics`) ([web-chat.md](web-chat.md)).

| Part | What |
|---|---|
| Row | title, last activity (relative), a "waits on you" count in the attention colour (open decisions, approvals, requests needing routing), and badges for the linked lanes and items with their derived state |
| Order | most attention first, then most recent activity |
| Expand | a row opens to its linked items (Next up, with state), lanes (with run and MR badge), and its open decisions, each linking to where they live today |
| Click | filters the thread to the topic. The filter is a URL hash parameter (`#/chat?topic=7`), so it is linkable and survives a reload. A second click, or the chip in the thread's header, clears the filter |
| Cleared | a collapsed "Cleared (N)" group at the bottom, each with **Reopen** |
| Actions | rename, merge into, clear, reopen, new topic. The desk's tools are not the only way: each is a button |

The filter is a view. It does not change what the desk sees or says; the desk still
carries the whole thread. A filtered thread says so in its header ("Showing: release v0.3.0,
12 rows. Show all") so nobody mistakes a filter for lost history.

The thread's rows gain a small topic chip, shown only on hover and while a filter is
active, and a "Move to topic" action beside the existing row actions.

### Terminal (`shepherd chat`)

The dock ([desk.md](desk.md), `internal/chat/dock.go`) gains a **topics** section sorted
by attention, one line each: title, waiting count, last activity. Slash commands call the
daemon's endpoint directly, as `/model` does:

| Command | Effect |
|---|---|
| `/topics` | list open topics, and cleared with `--all` |
| `/topic <id\|title>` | filter the thread to a topic; `/topic` alone clears the filter |
| `/clear <id\|title>` | clear a topic |
| `/reopen <id>` | reopen one |

Everything else (rename, merge, move) is said to the desk, which maps it onto the person's
turn, or run as `shepherd topic ...` in a shell.

### CLI and desk tools

Per CLAUDE.md each tool is a CLI command first, then its mapping in `internal/cli/mcp.go`.
The CLI is a client of `GET/POST /v1/topics` and `PATCH /v1/topics/{id}`; no command opens
the store.

| CLI | Desk tool | Notes |
|---|---|---|
| `shepherd topic list [--all] [--json]` | `topic_list` | read |
| `shepherd topic show <id>` | `topic_show` | links and what waits, read |
| `shepherd topic new <title>` | `topic_set` with `title` | creates |
| `shepherd topic set <id> --turn T \| --lane L \| --item N \| ...` | `topic_set` with `id` | tags |
| `shepherd topic rename`, `merge`, `move`, `clear`, `reopen` | the same names | person's turn only |

### Feed

One new closed kind, `topic`, for a topic created, renamed, merged, cleared or reopened,
so the thread and history carry the audit and the desk is told. The fold of a cleared
topic's rows is a view over the thread, not a feed row.

## 4. Clear

"Clear" is the person's way to say "this matter is settled, take it out of my thread".

| Does | Does not |
|---|---|
| Sets the topic `cleared`, with `cleared_at` | Close a lane |
| Folds the topic's turns and feed rows out of the main thread into one collapsed "N cleared: <title>" line per run of rows | Cancel or stop a run |
| Keeps every row in history and in search, with the topic's title as a match | Drop, defer or move a Next-up item |
| Keeps linked decisions, requests and approvals open and answerable in the rail | Answer or dismiss a decision or request |
| Is undone by **Reopen**, which restores the rows exactly | Delete anything |

**Anything needing the person is not folded.** A cleared topic is a promise that nothing
there needs them. When something later does (a new decision or request on a linked lane,
a failed run or pipeline, a quota hold on a linked run, an item approved but unstarted
in a topic the person has since cleared), Shepherd shows that one row in the thread as
usual, and the topic shows **reopen?** in the rail. It is never silently hidden, and it is
never reopened for the person: the next step is theirs.

Only the person clears: the CLI, the web button, `/clear`, or the desk acting on their
words in a turn they started. A `[Shepherd]` event cannot, and keep-going
([keep-going.md](keep-going.md)) does not. There is no auto-clear after a quiet period; a
finished lane or merged item does not clear its topic on its own, though the rail marks
a topic **settled?** when every linked lane is closed, every item done or dropped, and
nothing waits, so the person can clear it in one click.

Clearing a topic does not reduce the desk's context; that is rotation's job
([desk.md](desk.md)). The rotation summary gains the open topics' titles so a new session
keeps the names.

## 5. How it fits the rest

| Piece | Fit |
|---|---|
| One thread | Kept. Topics are a filter and a fold over the one thread and one session |
| Item 29, the desk registers work discussed in chat on Next up | The new item is linked to the turn's topic when added, so "put X on the list" lands under the matter that raised it, and the topic's row lists the item |
| Item 28, alerts for items waiting on approval | The count of items awaiting the person's approval is per topic and shown in the topic's "waits on you" number, so the alert says which matter it is about |
| Next up ([backlog-next.md](backlog-next.md)) | Items carry no new field: the link is in `topic_links`. State, rank and approval are unchanged |
| Keep-going ([keep-going.md](keep-going.md)) | Unchanged. Its audit rows take the topic of the item or lane they act on |
| Decisions and requests | Linked like lanes. Answering one is the same action in the rail, the thread or the topic |
| Rotation ([desk.md](desk.md)) | The summary lists open topics; the new session keeps the vocabulary |
| Backlog rail ([web-chat.md](web-chat.md)) | Now is unchanged; a lane row shows its topic chip |
| Terminal dock | A topics section and four slash commands (section 3) |

## 6. Existing history

Existing turns and rows are in General. `shepherd topic backfill` asks the desk, once, to
propose topics for the last N days (default 7) as a batch of (title, the turns and links
it takes). The proposal is shown as a list the person confirms, edits or drops; nothing
is written until they do, and confirmed links are marked `by: person`. It costs one desk
turn and is never run on its own. A person who skips it loses nothing: topics start
from the day the feature lands.

## 7. Sequencing

Four merge requests, each green alone:

1. **Core**: the migration (`topics`, `topic_links`), store methods, the daemon's topic
   type with the origin rules of section 2, `GET/POST /v1/topics` and
   `PATCH /v1/topics/{id}`, the client, `shepherd topic`, and tests (create, tag, move,
   merge keeps links resolving, person-only rules with fake origins, clear and reopen,
   the needs-the-person re-surface rule). Scope: `internal/store`, `internal/daemon`,
   `internal/api`, `internal/client`, `internal/cli`.
2. **Desk**: `topic_set`, `topic_list` and `topic_show` and their mappings, the topic list
   in each turn's head, automatic tagging of events by existing links, the `DeskBrief`
   paragraph, rotation summary, and the terminal dock section and slash commands.
   Depends on 1. Scope: `internal/desk`, `internal/chat`, `internal/cli`.
3. **Web**: the Topics tab, the thread filter and chip, the clear fold and reopen,
   "Move to topic", and `docs/web-chat.md`. Depends on 1.
4. **Feed and backfill**: the `topic` feed kind in thread and history, search by topic
   title, and `shepherd topic backfill` with its confirm step. Depends on 1; may land
   with 2.

## Out of scope

- A desk session, model or permission set per topic.
- Per-topic permissions or visibility.
- Auto-clearing, or clearing driven by an agent or a `[Shepherd]` event.
- Topics across workspaces.
- Nested topics or a tree; merge is how two become one.
- Deleting a topic or its rows.

## Risks

- **Mis-tagging.** The desk is a model and will tag some turns wrong. Mitigations: it
  prefers an existing topic, Shepherd's link-based tags are deterministic, a person's move
  always wins, and untagged is harmless.
- **Topic sprawl.** Too many near-duplicate topics make the tab noise. The one-new-topic
  cap per turn, the open-list in the head and merge are the answers; a title that repeats
  an open one is refused with the existing id.
- **Hidden work.** A cleared topic could hide something that matters. The needs-the-person
  rule is the guard, and Cleared (N) stays in the rail with what changed since.
- **A second thing to maintain.** Topics ask the person for a click they did not have.
  Everything works untouched: General is the default, and the tab is optional.
- **Filter mistaken for loss.** The filtered header says what is shown and offers "Show
  all"; the desk and history are never filtered.
