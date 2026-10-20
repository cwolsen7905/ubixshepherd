# Keep going: a switch that lets the desk carry on

Status: **proposed**, approved in outline by the maintainer (2026-10-11), with the
defaults in section 4. Merge request 1 of section 7 (Core) is built: the store, the
daemon's limits and `Allow`, `GET/POST /v1/keep-going`, `shepherd keep-going` and the
`keep_going:` config. Merge request 2 (Desk) is built except the daemon's gate in the
lane and run tools: the `keep_going` tool, the one-place event text and brief paragraph,
and the terminal `/keep-going`. The web switch (sidebar footer), the `keep_going` feed
kind, a feed item per autonomous action and `status`'s recent actions are built.
Implementation is four merge requests, each its own lane (section 7).

## The problem

On every `[Shepherd]` event turn the front desk is told "do not start new work they have
not asked for" (`DeskBrief` in `internal/chat/desk.go`, `digest` in `internal/desk/desk.go`,
and the terminal chat's event text in `internal/chat/model.go`). So it reports and then
waits, even when the next step is obvious: an approved Next-up item with no lane, a
failed pipeline, a run the daemon's restart interrupted. The person asked for a flag,
toggled from chat, that tells the desk to keep going unless a decision needs a human.

This is a mode, not a new kind of authority. Everything it lets the desk do is already
approved by something else (the Next-up list, an answered decision, a repo's autonomy
settings). The flag only removes the wait between those approvals and the action.

## Decisions

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | Where the flag lives | `config.yaml` / the store | The store: it is toggled at run time and must not need a reload or a commit |
| 2 | Scope | per repo / per workspace | Per workspace, like the desk's session |
| 3 | After a daemon restart | stays on / resets to off | Stays on, and re-checks its limits first (section 4) |
| 4 | Who enforces | the brief / the daemon | The daemon. The brief says the mode; the caps live in Go |
| 5 | Who may turn it on | anyone / the person only | The person only; a `[Shepherd]` event or an agent never does |

## 1. Where the flag lives and how it is toggled

One row in the store (an appended migration): `on`, `since`, `by` (cli, web, desk or
limit), `spent_at_start`, `reason_off`, and the counters of section 4. It is per workspace
and survives a restart, so a morning restart does not silently stop an overnight run. It
never survives past its limits: a restart that finds the day over, or the idle time spent,
turns it off before the desk's first turn and says why in the feed.

| Surface | How |
|---|---|
| CLI | `shepherd keep-going on [--cap USD] [--runs N] [--idle HOURS]`, `off`, `status`, `--json` throughout. A client of the daemon's `GET/POST /v1/keep-going`; no command opens the store |
| Desk tool | `keep_going` with `on`, `off` or `status`, mapped onto the CLI command first (per `internal/cli/mcp.go`). `on` is refused outside a turn the person started, the rule `decision_answer` follows; `off` is always allowed |
| Chat | The person says "keep going" or "stop and wait for me" and the desk calls the tool. Not a slash command: the desk's words are the toggle, the daemon's refusal rule is the guard |
| Web | A switch in the chat page's header, beside the spend and forge-health items ([web-chat.md](web-chat.md)), calling the same endpoint |
| Terminal | A badge in the dock; `/keep-going on\|off` in `shepherd chat` calls the endpoint directly, as `/model` does for the model |

## 2. What the desk may do on its own while it is on

Each is already authorised elsewhere; keep-going only lets the desk act on it unprompted.

| Action | Authorised by | Mechanism |
|---|---|---|
| Start the next approved Next-up item with no lane | the person's approval of the item ([backlog-next.md](backlog-next.md)) | `lane_open` with `item`, then `lane_run`. Only the first approved item, one at a time |
| Continue a run whose next step is already approved, such as push or open an MR | the repo's `autonomy.push` and `autonomy.merge` | the existing `lane ship` or a continued session. A repo whose rule is human is skipped |
| Follow up a failed pipeline | the watcher's fix loop | the desk may start one the watcher did not, still capped by `MaxFixTries` |
| Widen scope the person already approved | an answered decision naming the paths | the desk's scope-widening tool ([backlog-next.md](backlog-next.md) seeds it as planned); refused without a decision to cite |
| Re-run a run the daemon's restart interrupted | the original run's own approval | `Runner` resume, once per run |

## 3. What always stops for the person

| Stop | Why |
|---|---|
| An agent's decision | `decision_answer` takes the person's words; the desk never answers |
| Approving, deferring, dropping or reordering Next-up items | already the person's, and `[Shepherd]` events cannot move one |
| A merge, or a push where the repo's rule is `human` | repo autonomy decides, not this flag |
| Tags, releases and deploys | `autonomy.tag` and `autonomy.deploy`, and "agents never tag or publish unasked" |
| The budget | crossing the daily budget turns the mode off; raising it is the person's |
| Inventing work | an item the desk thinks of is `planned`, never started |

When the next step is one of these the desk says so in one line and leaves it, as today.

## 4. Safety limits and how it turns itself off

| Limit | Default | Setting | Effect |
|---|---|---|---|
| Spend since turning on | $5 | `keep_going.cap` | at the cap, off |
| Concurrent runs while on | 2 | `keep_going.max_runs` | a start past the cap is refused, not queued |
| Daily budget | the existing budget | none | crossing it, off |
| Idle time with nothing started or answered | 4 hours | `keep_going.idle_hours` | off |
| End of day | local midnight | `keep_going.day_end` | off |
| Loop guard | 3 autonomous starts per lane per day | `keep_going.lane_starts` | the lane is skipped and the person is told once |

Idle time counts from the last autonomous action or the last message from the person. The
loop guard counts starts per lane (an item's lane, a fix, a re-run), so a lane that keeps
failing is handed back rather than re-run for ever. The defaults are conservative; each
is a `config.yaml` setting under `keep_going:`, validated in `internal/config` and
reloaded on SIGHUP, and the CLI flags set the per-session cap and run limit.

Turning itself off is a recorded event: `reason_off` (cap, runs, budget, idle, day end,
person) goes to the feed, and the next desk turn is told in the event text.

## 4a. The daemon starts the work

While the mode is on, the daemon itself keeps work moving: nothing depends on a client
being attached, on `desk.wake`, or on the desk choosing to act in an event turn. On every
tick, and when a run ends, `KeepGoing.EvaluateAll` checks the limits and then drives
(`internal/daemon/keepgoing_drive.go`):

- It continues first: an approved item whose open lane never ran, or whose last run a
  crash interrupted. A run stopped for a restart is resumed by the restart, not here.
- Then it starts approved items with no lane, in rank order, until the run slots are
  full. A new lane takes the whole repo as its scope, so a second item in the same repo
  waits for the first lane to close.
- An item needs a plan doc or an acceptance line. Without either, only that item is held
  and the feed says so once.
- Every start passes the same gate as the desk's (`permit`): the spend cap, run cap, idle
  time, end of day and starts per lane all hold, with the reason in the feed.
- Each start is named in the feed with the item, its title, the lane and the run.

An item is held only by its own blockers: a dependency not done, an open decision or a
held merge request on its lane, or an open question linked to the item, its lane or its
topic. A question linked to none of them blocks nothing. The stall alert fires only when
nothing can start, and lists each item with its own blockers.

`shepherd keep-going` status ends with a summary of what the mode did since it was turned
on: items started, runs finished and failed, merge requests, and why and when it stopped.

The drive never answers a decision, approves, merges, pushes for a person, tags,
releases or deploys. A failed pipeline is still followed up by the watcher, capped as
before.

## 5. How the desk's instructions change and how Shepherd enforces the limits

**Instructions.** The three copies of "do not start new work they have not asked for"
become one function that picks its text from the flag: the unchanged sentence when off,
and when on, "Keep-going is on: start the next approved item and continue approved next
steps, unless a decision needs the person. Limits and refusals come from Shepherd; do not
work around them." `DeskBrief` gets the standing paragraph describing the mode and the
list in section 3. Today the three copies can drift; the change puts them in one place.

**Enforcement is server-side**, because a prompt is not a limit:

- Each autonomous start goes through one daemon check (`KeepGoing.Allow(action, lane)`)
  in the desk's lane and run tools. It refuses when the flag is off, a cap is reached, the
  lane's loop count is spent, the repo's rule is human or the item is not approved. The
  refusal is a tool result with the reason, which the desk relays.
- Autonomous actions are those the desk takes in a turn the person did not start (an
  event turn). A turn the person started is unchanged: they asked, so there is no cap
  to apply.
- Counters, `since` and spend are read from the store and the existing run costs, not
  from the model. The daemon evaluates idle, day end and the budget on a timer and on
  every run end, and turns the flag off itself.
- An agent's token reaches none of it, and the endpoint's turn-on is checked against the
  origin of the call, as for `decision_answer`.

## 6. How it shows

| Where | What |
|---|---|
| Header badge (web) and dock badge (terminal) | "keep going: on, 2 of 3 runs, $1.20 of $5", warning colour from 80% of a cap; off shows nothing, or "off: idle" for a stop in the last hour |
| Feed | one closed kind, `keep_going`, for on, off (with its reason) and each autonomous action (started item 7 as lane x, re-ran run y, followed up pipeline on lane z), so the thread and history carry the audit |
| `shepherd keep-going status` | the same numbers, the limits, and the last ten actions |
| Desk | `keep_going` `status` for "what did you do while I was away" |

## 7. Sequencing

Four merge requests, each green alone:

1. **Core**: migration and store methods, the daemon's `KeepGoing` type with `Allow`, the
   timer and run-end evaluation, `GET/POST /v1/keep-going`, the client, `shepherd keep-going`,
   `keep_going:` config and tests (caps, auto-off, loop guard, restart, the origin rule,
   with fake time). Scope: `internal/store`, `internal/daemon`, `internal/api`,
   `internal/client`, `internal/cli`, `internal/config`.
2. **Desk**: the `keep_going` tool, the gate in the lane and run tools, the one-place
   event text and the `DeskBrief` paragraph, a terminal `/keep-going`. Depends on 1.
   Scope: `internal/desk`, `internal/chat`, `internal/cli`.
3. **Web**: the header switch, the dock badge and `docs/web-chat.md`. Depends on 1.
4. **Feed and audit**: the `keep_going` feed kind in the thread and history, and
   `status`'s last actions. Depends on 1; may land with 2.

## Out of scope

- Per-repo or per-lane flags, and per-item "keep going until".
- Letting the desk approve, answer, merge, tag or deploy under any mode.
- A schedule ("on from 9 to 5"); the limits above are the schedule.

## Risks

- A runaway: the caps and the loop guard are daemon-side, but a wrong cap is still
  spend. The defaults are low and `off` is one word.
- A silent mode: someone forgets it is on. The badge, the end-of-day off and the idle
  off exist for that.
- Approvals quietly standing in for intent: the Next-up approval is old. The doc keeps it
  as the person's call, and the page shows an item's age.
