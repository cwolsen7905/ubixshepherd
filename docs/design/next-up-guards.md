# Next-up guards: keeping the list current

Status: **proposed**; the maintainer approved all five guards ("go with all of it").
Built so far: guard 1 (watcher and lane close), expiry, the migration, `done_when` and
`depends_on` (with `blocked_on` and a cycle check) through the API, CLI and MCP, and the
suggestion actions in Waiting on you. Not yet: what *writes* a suggestion (the still-needed
brief's `ALREADY DONE:` report and the weekly re-check), duplicate warnings and the drift check.
This note is the design; the code lands in separate merge requests, one per guard where
practical. Read [backlog-next.md](../backlog-next.md) first: this extends the Next-up list
it built.

## The problem

Next-up items went stale without anyone noticing, for four reasons that repeat:

| Cause | What happened |
|---|---|
| Merge proof by git ancestry | A squash-merged lane showed "closed without a merge", so its item stayed approved. |
| Work landed under another lane | The item had no link to what finished it, and nothing asked. |
| Status kept in two places | A plan doc's status rows drifted from the item list. |
| No dependencies | An item was shown as "next" while it needed unfinished items. |

The rule that shapes every fix: **the system may notice and suggest; only the person
approves, defers, drops, reorders or marks done.** The one exception is the pair of changes
that are a pure function of a forge's word or the clock, and each is recorded on the feed:
done on merge (guard 1) and expiry of an unapproved proposal (guard 4). Nothing here asks an
LLM to judge; an agent only reports a fact (guard 2) that a person then confirms.

## Guard 1: done on merge

Today `store.Settle` marks an item done only when its lane is closed *and* the lane's stored
forge state is `merged`. That state is written only while the lane is open and polled, so a
lane closed by hand (`lane close`, which proves a merge by git ancestry and so refuses or
mislabels a squash) never records it.

- **Watcher.** Each pass, for every non-done, non-dropped item linked to a *closed* lane whose
  stored merge request is not `merged`, the watcher asks the forge for the lane's branch. If
  the request is merged it records `merged` (and the merge commit) in the lane's forge state
  and marks the item done with `done_at` = the lane's close time, the same feed line as the
  daemon's settle (`plan` kind, "lane X merged"). The forge's `merged` is the proof; ancestry is
  not consulted. The existing first-sight rule is kept for *open* lanes: a request already
  merged when Shepherd first looks proves nothing about the lane's current work.
- **Lane close.** `Fold.Close` consults the forge when git cannot see the branch merged: a
  merged request whose head the branch does not exceed counts as merged, so a squash merge
  needs no `--force` (this is `CloseMerged`'s check, reused). *Needs `internal/fold` and
  `internal/cli/lane.go`: a scope request.*
- Back-stop: the daemon's read-time settle stays, so a done item never depends on the
  watcher having run.

## Guard 2: "still needed?" before work starts

When a run starts in a lane that an item is linked to, the runner adds a short block to the
agent's brief (`dispatch.StillNeeded`), after the standards and before the task:

> This lane was opened for next-up item N: "<title>". Before you change anything, fetch and
> check `<base>` for whether this work has already landed (the files, the commits, the
> tests). If it has, make no commits and report done with a summary that starts with
> `ALREADY DONE:` and names the evidence (commit ids, paths). Otherwise carry on.

Only a new session gets it (a continued one already has the brief). When a run ends whose last
report is `done` with an `ALREADY DONE:` summary and no commits, the runner stores a
*suggestion* on the item (see store) and writes a `plan` feed line. The person sees
"mark done?" in Waiting on you; accepting is `shepherd next done N` (or the page's button),
declining clears the suggestion. Shepherd never marks it itself: an agent saying "already
done" is a claim, and a wrong one would hide real work.

## Guard 3: "done when", dependencies, duplicate warnings

- `done_when`: optional one line, the acceptance test ("`shepherd next` lists the item as
  done after a squash merge"). Shown with the item; used by the weekly re-check (guard 4) and
  in the brief of a lane opened for the item.
- `depends_on`: item ids. **Blocked is derived, never stored**: an item is blocked on every
  dependency whose effective state is not `done`. A blocked item is never `next_up`, the
  rail's Next up view lists it after the unblocked ones with "blocked on #3, #7", and
  `next_list` / `shepherd next` return `blocked_on` (ids and titles). Starting a lane for it
  is not refused (the person may know better), but the opener is told. Setting a dependency on
  itself or one that closes a cycle is refused; a dropped dependency is *not* done and keeps
  the item blocked, and the item is flagged so the person decides (drop or re-plan).
- **Duplicate warning on add.** Deterministic: the new title and every open item's title (same
  repo, or either repo empty) and every open lane's name in that repo are lower-cased,
  split on non-alphanumerics, stop words dropped; a pair is "likely duplicate" at
  Jaccard similarity >= 0.6 or when one title's tokens contain the other's. The add still
  succeeds; the response carries `warnings` naming the item or lane, and the CLI prints them.
  No LLM, no network.

## Guard 4: expiry and the weekly re-check

Both run in the watcher (it already ticks), are read-only against the forge, and are throttled
by timestamps stored on the item, so a restart does not repeat them.

- **Expiry.** A *proposed* item (source `desk` or `agent`: it waits on the person) still
  `planned` after `next.expire_days` (default 14, `0` turns it off) expires to `deferred`.
  The person's own and chat-registered items never expire: they are the person's notes, not a
  queue waiting on them. The change is recorded three ways: a `plan` feed line ("expired to
  deferred after 14 days unapproved"), the item's note gets the same sentence with the date,
  and the item's `updated` moves. The person can approve it from deferred as usual.
- **Weekly re-check** (every 7 days per item, by `checked`). For each *approved* item without
  a started lane or whose lane's request is not merged: if the lane's request is merged, or its
  lane closed with a merged request, the suggestion is "done?"; if `done_when` is set and the
  lane is closed unmerged, or the lane has been closed for more than a week, or a WORK.md row
  of the same lane disagrees with the item (guard 5), it is "done or drop?". The suggestion
  is a short line in the item's `suggestion` field. The check **never changes `state`**.
  It appears in Waiting on you as kind `next`, action `decide`; the person's options are
  done, drop, or "still needed" (clears the suggestion and resets the clock).

## Guard 5: one source of truth

Two options:

| Option | Cost | Determinism |
|---|---|---|
| A. Shepherd reads plan-doc prose and status columns and compares them to items | parsing free-form markdown per repo; every doc style needs its own rules | brittle |
| B. Plan docs link to item ids and keep no status column; Shepherd reports the drift that remains | a convention, plus a check over a table Shepherd already parses | exact |

**Option B.** The docs are the plan, the item list is the status. A plan doc names its items
(`next-up item 12`, or a `Next-up item` column holding the id) and shows no status of its own.
Shepherd already parses `docs/WORK.md` (the work page and the matrix read-through), whose rows
have a status and a lane. The drift check compares a row to the stored item that names the same
lane in the same repo: row done but item approved/planned/started, or row todo/blocked but
item done/dropped. It reports, never edits: a line in `shepherd next` ("drift: WORK.md row
`web-overhaul` says done, item 14 is approved") and, for approved items, an input to the
weekly re-check. Prose that is not a WORK.md row is not parsed; docs that drop their status
columns cannot drift, which is the point. Generating docs from items was rejected: it writes
into repos, makes conflicts, and still needs the doc's own format.

## Store changes

One appended migration to `next_items`:

| Column | Meaning |
|---|---|
| `done_when` | one line, default '' |
| `depends_on` | item ids, comma separated, default '' |
| `suggestion` | the one-line "done?" / "done or drop?" suggestion, '' when none |
| `suggested` | when the suggestion was made |
| `checked` | when the weekly re-check last ran for the item |

`NextItem` gains `DoneWhen`, `DependsOn []int64`, `Suggestion`, `Suggested`, `Checked`;
`NextPatch` gains pointer fields for `DoneWhen`, `DependsOn`, `Suggestion`, `Checked`.
Pure helpers live in `internal/store/next.go` so every client agrees: `Blocked(item, all)`,
`Similar(title, title)`. Config adds `next.expire_days` (default 14). No new table.

## API, CLI, MCP

- `POST /v1/next` takes `done_when`, `depends_on`; replies with `warnings`. `PATCH
  /v1/next/{id}` takes `done_when`, `depends_on`, and `suggestion` (empty clears it).
  `GET /v1/next` items carry `done_when`, `depends_on`, `blocked_on` (`[{id,title}]`),
  `suggestion`, and the list carries `drift` lines.
- CLI: `shepherd next add --done-when "..." --depends 3,7`, `shepherd next edit N --done-when
  --depends --dismiss` (clears a suggestion), `shepherd next` prints blocked-on and drift.
- MCP: `next_add` / `next_update` gain `done_when`, `depends_on`; `next_list` returns the new
  fields. Each maps onto the CLI flags above (CLI first, then the mapping).

## Events

All on the existing `plan` feed kind with `ref` = the item id, one redacted line each, so no
client changes: done on merge (already exists, now also from the watcher), "may already be
done" (guard 2), "expired to deferred after N days unapproved" (guard 4), "re-check: done or
drop?" (guard 4). A suggestion that is dismissed or acted on writes nothing extra beyond the
state change's own line.

## What stays the person's call

| Action | Who |
|---|---|
| approve, defer, drop, reorder, mark done, return to planned | the person (the desk only in a turn they started) |
| accept or dismiss a suggestion | the person |
| set `done_when` / `depends_on` | the person or the desk (text, not state) |
| done on merge | the system, on the forge's word; recorded |
| expiry of an unapproved *proposal* to deferred | the system, on the clock; recorded; reversible |
| everything else (re-check, already-done, drift, duplicates) | suggestion only |

## Suggestions in Waiting on you

An item that is not done or dropped and carries a `suggestion` shows in Waiting on you as
kind `next`, action `decide`, the suggestion line as the detail. A suggestion that starts
`done or drop?` offers Done, Drop and Still needed; any other (`done?`) offers Done and
Still needed. Done and Drop are the state change, and either clears the suggestion. Still
needed is `PATCH` with an empty `suggestion`: it clears the line and stamps `checked`, so
the weekly re-check starts over. Only the person may do any of these; the desk may in a
turn the person started. An item whose lane's merge was seen is left to settle as done, not
asked about.

## Delivery

1. Done on merge: watcher settles closed lanes (this lane, in scope). Lane close on the forge's
   word needs `internal/fold` and `internal/cli/lane.go`.
2. Still-needed brief: `internal/dispatch`. Its suggestion half needs the migration.
3. `done_when`, `depends_on`, duplicates: needs the migration, `store.go`, `api.go`,
   `client.go`, `mcp.go` and `daemon.go`.
4. Expiry and re-check: watcher, plus the migration, config and the Waiting panel
   (`internal/waiting`, the web panel).
5. Drift check and the docs convention: daemon (`nextmatrix.go`) and `docs/`.
