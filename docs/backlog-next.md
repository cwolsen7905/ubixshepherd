# Next up: work that is planned but not being acted on

Status: **approved as written** by the maintainer (2026-10-11). Merge request 1 (the core)
is built: the table, API, `shepherd next`, `lane open --item` and the `next_*` desk tools.
The web page and rail toggle (2) and the matrix read-through and `plan` feed kind (3) are
not. Implementation is three merge requests, each its own lane (section 6).

What the core did differently from section 6, and why:

- **Done is settled on read, not by a hook in `Fold.CloseMerged`.** The watcher closes the
  lane and records the merge request as merged; the daemon derives `done` and `done_at`
  from that when the list or an item is read, and keeps it. No second status, and no
  change to the fold or the watcher.
- **The link is made in the daemon's lane-open route**, not in `internal/fold`, from an
  `item` field next to the open request.
- **`shepherd next seed`** adds the items in section 5, skipping titles already present.
  It is the person's step to run, not a migration.
- **The worker tool `next_propose` is not built** (section 1): the core's list does not
  include it. Agents still use `ask_shepherd` to suggest an item.
- **A state or move from the desk outside a turn the person started is refused**, the rule
  `decision_answer` follows; adding an item as `planned` and editing text are open to it.

## The problem

The chat page's backlog rail ([web-chat.md](web-chat.md), `web/src/model/backlog.ts`) shows
live state only: lanes, runs, decisions and requests. Work that is planned but has no lane
is invisible, so "what do we do next" is answered from memory:

- approved plans with nothing started (for example, steps 2 and 3 of
  [lane-diff.md](lane-diff.md));
- items a plan defers or puts out of scope (an in-app editor, comparing two lanes);
- leftovers noted when a phase shipped;
- ideas raised in chat that nobody picked up.

This adds a **Next up** view beside the rail, in the web page, the CLI and the front desk.
The rail itself is unchanged and nothing already in it is dropped.

## Decisions to make

Approved with the recommendations below.

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | Where items live | parse plan docs / the per-repo work matrix only / a store table | A store table, with work-matrix rows read through (section 1) |
| 2 | Who may approve | anyone / the person only | The person only; agents and the desk propose |
| 3 | How an item reaches a lane | by hand / a link set when a lane opens | A link set at `lane open` (`--item`), with a name hint as the fallback |
| 4 | Priority | labels, scores / one manual order | One manual order, approved above planned above deferred |
| 5 | Web placement | a rail tab / a page / both | Both: a rail toggle for the top few, a page for the list |

## 1. Where items come from and live

| Option | For | Against |
|---|---|---|
| Parse `docs/*.md` plans | no new state; items sit with their prose | plans have no common shape; "approved" and "deferred" are prose; a chat idea has no doc; every edit is a public commit |
| Work matrix only (`docs/WORK.md`, [work-matrix.md](work-matrix.md)) | already parsed, with lane and PR links and drift | per repo, in git and so public, and a status change is an MR; no place for a cross-repo item or a private idea |
| **Store table** (recommended) | the daemon owns it like lanes and decisions; cross-repo; one command adds an item; links update without a commit | a second place that can drift from the prose, so each item links to its plan doc |

**Recommendation: a `next_items` table in the store**, an appended migration, with the
work matrix read through: a repo's `docs/WORK.md` rows with status `todo` or `blocked`
appear in the view as read-only items marked `matrix`, edited in the file as today.
Nothing is copied into the table, so there is one source per item. Plan docs are not
parsed; an item carries a `plan` path and an optional anchor, and the page links to it.

An item holds: `id`, `title`, `repo` (optional), `plan` (path, optional), `lane` (a name
hint, optional), `lane_id` (the link), `mr` (number or URL, optional), `state`, `rank`,
`source`, `note`, `created`, `updated`, `done_at`.

How an item is added:

| By | Through | Starts as |
|---|---|---|
| The person | `shepherd next add`, the web page's add box | `planned`, or `approved` with `--approved` |
| The desk | `next_add` tool, when the person says "put X on the list" | `planned` |
| An agent | worker tool `next_propose`, for follow-ups it found but is not scoped to do | `planned`, source `agent` |

Agents never approve ([CLAUDE.md](../CLAUDE.md)). Approval is the person's: the CLI, the
web page, or the desk acting on the person's words in a turn the person started, the same
rule `decision_answer` follows. Agent-proposed items show as "proposed" until then.

## 2. States, links and how items move

| State | Meaning | Set by |
|---|---|---|
| `planned` | proposed, not yet approved | add |
| `approved` | the person has said do it | person |
| `deferred` | acknowledged, on purpose not now | person |
| `started` | linked to an open lane | derived |
| `done` | the linked lane's MR merged, or the item was marked done by hand | derived or person |
| `dropped` | will not be done; kept so it is not re-proposed | person |

Only `planned`, `approved`, `deferred` and `dropped` are stored as chosen. `started` and
`done` are derived from the link, so they cannot go stale.

**Links.** `plan` is a path under the repo. `lane_id` is set when a lane opens for the
item. `mr` comes from the lane's merge request, which the forge watcher already reads.

**Moving on its own:**

- `lane open --item <id>` (and the same argument on the `lane_open` tool) links the item.
  Without it, a lane opened in the item's repo whose name equals the item's `lane` hint
  links to it when exactly one open item matches. Anything ambiguous is left unlinked and
  the page offers "link to lane".
- A linked lane that is open shows the item as `started`, with the lane's own state
  (running, idle, MR open) borrowed from the lane view, so no second status is kept.
- The watcher's merge-closure path (`Fold.CloseMerged`) sets `done` and `done_at` on the
  linked item. Closing a lane without a merge returns the item to its stored state and the
  page says why; the person decides whether it is `deferred` or `dropped`.
- One item per merge-request-sized step. A plan with three merge requests is three items,
  ordered by rank, so each moves on its own.

## 3. Where it shows

| Surface | What |
|---|---|
| Web page `#/next` | the list: approved, then planned, then deferred (collapsed); each row has title, repo, plan link, lane and MR links, and actions: approve, defer, drop, move up and down, link to lane. **Start** prefills the chat draft ("open a lane for item 7"), as the backlog cards do today; it does not start a run itself |
| Rail toggle | the chat page's rail header gains **Now / Next up**, remembered in browser storage like `shepherd.chat.backlog`. **Now** is today's rail; **Next up** shows the top five approved items and a link to the page |
| CLI | `shepherd next [list]`, `add`, `show <id>`, `approve`, `defer`, `drop`, `move <id> top\|up\|down`, `link <id> --lane <name>`, `done <id>`, `--json` throughout |
| Desk tools | `next_list` (read, so the desk can say "next up is ..."), `next_add`, `next_update`. The desk may not approve unless the person's words in this turn do, and `DeskBrief` says so |
| Feed | one new closed kind, `plan`, for an item added, approved or finished, so the thread and history show it and the desk is told |

Per CLAUDE.md, each tool is a CLI command first, then its mapping in
`internal/cli/mcp.go`. The CLI is a client of the daemon's HTTP API
(`GET/POST /v1/next`, `PATCH /v1/next/{id}`); no command opens the store. The terminal
dock is unchanged in this plan; the desk's `next_list` covers the terminal.

## 4. Ordering and priority

One integer `rank`, lowest first, set by `move`. New items go to the bottom of their
state. The list is `approved` by rank, then `planned` by rank, then `deferred`. There are
no priority labels or scores: the person's order is the priority, and "what is next" is the
first approved item with no lane.

## 5. Seeding

Items known today, added by the person (or the desk, on the person's go-ahead) with
`shepherd next add` once the CLI lands. The first four are approved; the rest start
`planned` or `deferred` as shown. Status was read from the docs and `git log` on `dev`.

| Item | State | Plan |
|---|---|---|
| Lane diff step 2: the web Changes tab and editor link | approved | [lane-diff.md](lane-diff.md) |
| Lane diff step 3: gate record, badge and the run page's range view | approved | [lane-diff.md](lane-diff.md) |
| This backlog: the Next up core (table, API, CLI, `next_*` tools) | approved on this plan's approval | this doc |
| Desk tool to widen a lane's scope when the person approves it in a decision | planned | none yet |
| Answered decisions read with no time bound | planned | none yet |
| Repo add part 2: findings kept in memory | planned | [repo-add.md](repo-add.md) |
| Timeline leftovers: `g t` in the Help dialog | planned | [swarm-timeline.md](swarm-timeline.md) |
| Timeline leftovers: arcs between runs | planned | [swarm-timeline.md](swarm-timeline.md) |
| Timeline leftovers: tokens in the tooltip | planned | [swarm-timeline.md](swarm-timeline.md) |
| Timeline leftovers: arrow-key walking | planned | [swarm-timeline.md](swarm-timeline.md) |
| Timeline phase 2: the daemon endpoint | planned | [swarm-timeline.md](swarm-timeline.md) |
| Lane diff: the in-app editor | deferred | [lane-diff.md](lane-diff.md) |
| Lane diff: two-lane comparison | deferred | [lane-diff.md](lane-diff.md) |
| Lane diff: `shepherd lane gate` to run and record the gate in a lane | deferred | [lane-diff.md](lane-diff.md) |
| Backlog actions: ship from a card, first run of a never-run lane, per-lane fix-try count, multi-select | deferred | [backlog-actions.md](backlog-actions.md) |
| Rail: run progress bar, per-project grouping, per-lane spend, board feed strip | deferred | [web-chat.md](web-chat.md) |
| Terminal dock grouped by project | deferred | [web-projects.md](web-projects.md) |
| Workspace-wide leases | planned | [roadmap.md](roadmap.md) |
| GitHub mirror proofs; typed cross-repo work orders and triage | planned | [roadmap.md](roadmap.md) |

Not seeded: the rail's "density default" and "all-pages live rail", which are open
choices for the person rather than work, and anything only in earlier chat threads that
this plan's author cannot read. Those go in with `next add` or by asking the desk to
list what is unowned. Check the three backlog-actions phases against `dev` before seeding;
the commit history suggests they have partly landed.

## 6. Sequencing

Three merge requests, each its own lane, each green alone:

1. **Core**: migration and store methods, `internal/fold` link and done hooks, API,
   client, `shepherd next` and the `lane open --item` flag, `next_*` MCP tools and the
   desk's allowlist and brief. Tests: store and state derivation against real lanes with a
   bare origin, the merge-closure hook with a fake forge, the MCP mapping and the
   approval rule. Scope: `internal/store`, `internal/fold`, `internal/api`,
   `internal/client`, `internal/cli`, `internal/chat` (brief), `docs/`.
2. **Web**: `web/src/model/next.ts`, the page, the rail toggle, the chat-draft **Start**,
   stub data and screenshots, `docs/web-chat.md`. Depends on 1.
3. **Matrix read-through and feed kind**: matrix rows in the list, the `plan` feed kind
   in the thread and history, and seeding documented. Depends on 1; the web part is small.

Seeding is a person's step after 1, not a migration, so no item is invented in the store
without them.

## Out of scope

- Estimates, owners, due dates, dependencies between items, and labels.
- Creating lanes or starting runs from this list; it prefills the chat and the person
  or desk acts.
- Parsing plan docs, and writing back to `docs/WORK.md`.
- Per-project filtering beyond the repo's project, until projects have their own view.

## Risks

- Two lists drifting: the store and the prose. Each item links its plan doc, and the
  derived states mean only the intent, not the progress, is hand-kept.
- A stale `planned` pile. The page shows age, and `drop` is one command.
- The matrix and the store naming the same work: a matrix row whose lane name a store
  item already links is shown once, as the store item.
