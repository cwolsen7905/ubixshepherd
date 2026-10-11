# Swarm timeline

Status: **proposal**, for the maintainer to approve or change. No code was written for it.

One view of the whole day's work at once: a horizontal track per lane, grouped by repo,
time running left to right, each run a bar coloured by its agent. The point is to make
Shepherd's parallelism visible at a glance: many agents in many lanes, not colliding. It is
also the picture for the next blog post, so it has to look intentional, not like a
dashboard template.

Everything here was checked against the code this branch was cut from (`dev` at b759322): `web/src/`, `internal/api`,
`internal/daemon` and `internal/store`. The visual direction follows the frontend-design
guidance, and the chart follows the dataviz method (form first, then colour, validated
with its palette script).

## What the person sees

```
 Timeline   Today  24 h  7 days                          3 running, 1 waiting, 2 merged
 ─────────────────────────────────────────────────────────────────────────────────────────
              06:00      08:00      10:00      12:00      14:00      16:00      18:00 now
 acme-api
   feat/login-sessions   ▕▇▇▇▇▇▇▇▇▇▇▕    ▕▇▇▇▇▇▕ ◆       ▕▇▇▇▇▇▇▇▇▇▇▇▇▇▕
   fix/cache-inval.            ▕▇▇▇▇▇▇▕✗   ▕▇▇▇▇▇▕                         ◆
   refactor/queue-worker                  ▕▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▕░▒▓●
 acme-web
   feat/export-csv                                    ▕▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▕░▒▓● ✦
   chore/bump-deps       ▕▇▇▇▇▇▕                                                          
 acme-docs
   docs/install-guide              ▕▇▇▇▇▇▇▇▇▇▕                                            
 ─────────────────────────────────────────────────────────────────────────────────────────
 spend   $0 ╱‾‾‾‾╲___╱‾‾‾‾‾‾‾‾╱‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾╱‾‾‾‾‾‾‾‾‾‾‾╱‾‾‾  ·········· budget $25
```

`▇` a run, coloured by agent. `░▒▓●` the leading edge of a running bar, with its soft
pulse. `✦` a decision waiting on the person (it glows). `◆` a merge. `✗` a failure. Thin
arcs (not drawn above) join a request's asker to the lane it was routed to.

## Decision summary

One decision for the person. Everything below is the reasoning, and each choice has a
recommendation, so "approve" means "all of the recommendations".

| # | Question | Recommendation |
|---|---|---|
| 1 | Where does it live? | Its own page, `#/timeline`, with a nav entry. A compact strip on the board only after the page has been seen. |
| 2 | Data | One new read-only endpoint, `GET /v1/timeline`, answering the whole view in one call. The live snapshot overlays it. |
| 3 | Rendering | Hand-written SVG, no new dependency. |
| 4 | Ranges | Today (the default), Last 24 hours, Last 7 days. |
| 5 | Scope | Widen the lane to the files in the table at the end, in three phases, with the first phase on screen without any daemon change. |

## 1. Where it lives

**Recommendation: its own page, `#/timeline`, and a "Timeline" entry in the nav** (key
`g t`, joining `g b` and `g d`). The board stays the place that answers "what needs me".

| Option | For | Against |
|---|---|---|
| Own page | Full shell width and a tall canvas, so the hero picture has room (about 1440 by 900 shows 16 to 24 tracks). The board's calm is untouched. Linkable and screenshot-able at an address. | One more click from the board. |
| Band on the board | Always in view. | The board's job is attention order; a chart above it competes with the lantern band, the one loud thing. Tracks need 16 to 24 rows of height, which pushes the board off screen. A compressed band loses what makes it good. |
| Both | Best of each. | Two things to keep in step before either is proven. |

Follow-up, only if the page earns it: a one-row-per-repo strip at the top of the board,
no taller than the nav bar, linking to the page. It reuses the same model code, so it is
cheap to add later and not worth designing now.

The page also gets lane and run click-through: a bar opens the run's log
(`#/runs/ID/log`), a track's name opens the lane (`#/lanes/ID`), a decision marker opens
`#/decisions?d=ID`. All three routes exist.

## 2. Data

### What exists today

| Need | Where it comes from | Good enough? |
|---|---|---|
| Lanes, repo, state, created, closed | `GET /v1/lanes` (open) and `GET /v1/history/lanes?state=all&since=` (closed, with outcome, run count, cost) | Yes. |
| Runs with start, end, state, agent, model, cost, commits | `GET /v1/runs` (newest 200, with `progress`) and `GET /v1/history/runs?since=` (up to 1000, no prompt) | Mostly. See gaps. |
| Live progress of a running run | `RunView.progress` in the snapshot | Yes. |
| Decisions waiting | `GET /v1/decisions?state=open`, with `created` | Yes for open ones. |
| Decisions answered, with the wait | `GET /v1/decisions?state=` (all) has `created` and `answered` | Unbounded: no time filter. |
| Requests between lanes | `GET /v1/requests?state=a,b` has `from_run`, `from_lane`, `repo`, target `lane` name, `target_run`, `created`, `updated` | No time filter, no lane ids. |
| Merges | `mr_state` and `outcome` on the lane, `closed`, forge time `mr_updated` | No `merged_at`; see below. |
| Failures | `run.state` of `failed` or `interrupted`, with `ended`; `pipeline_status` and `pipeline_finished` on the lane | Yes. |
| Spend today and the daily budget | `GET /v1/spend` (`usd`, `budget`, `by_source`) | A total, not a curve. |
| Tokens of a run | `GET /v1/runs/{id}` has `usage`; `GET /v1/usage` has totals per group | Not per run in any list. |
| What just happened | the feed, newest 1000 items, polled every 1.5 s | Good for live, too short for a week. |

### The gaps

1. **A run that straddles the window is missed.** `history/runs?since=` keeps runs whose
   `started` is at or after `since`, so a run begun at 23:40 and still going at 00:10 is
   absent from "Today", and a long run is absent from a 7-day view that begins mid-run.
   The timeline needs runs that *overlap* the window.
2. **No per-run tokens in any list.** Hover needs tokens; today only a single-run read has
   them, and N reads per hover is wrong.
3. **Decisions and requests have no lane ids.** A decision has `run_id` and a lane name; a
   request has a lane name and a repo. Drawing a marker on the right track needs the lane's
   id (names repeat across repos), and an arc needs both ends.
4. **No time bounds on decisions and requests.** The only filter is `state`, so a history
   view reads every decision and request ever made.
5. **No spend over time.** `spend` is a per-day, per-source total. The band needs a curve.
   The `usage` table does carry a timestamp and a cost for every run and every front desk
   turn, but only group totals reach the API.
6. **No merge time.** The merge tick should sit at the moment of the merge. The daemon
   closes a lane when it sees the merge, and the forge reports `mr_updated`; neither is
   named `merged_at`.
7. **Continued runs.** A run that continues a session has `parent` and `session`, which
   the history list leaves out. The timeline wants to draw a continued run as the same
   lane at the same colour, which it will anyway, so this is nice to have: it lets a bar
   say "continues run 41".

### The proposal: one endpoint, `GET /v1/timeline`

A read-only, additive endpoint in `internal/api` (types and handler, tested the way
`history.go` is), registered for the same role as the history routes (`desk`, which a browser session has). It does the joins in
the daemon so the browser makes one call, whatever the number of workspaces:

```
GET /v1/timeline?workspace_id=N&from=RFC3339&to=RFC3339

{
  "from": "...", "to": "...", "now": "...",           // the daemon's clock
  "lanes":     [{ id, repo, name, state, created, closed?, outcome?,
                  mr?, mr_state?, mr_url?, merged_at?, pipeline_status?,
                  pipeline_finished? }],
  "runs":      [{ id, lane_id, agent, model?, state, started, ended?, commits,
                  cost_usd?, credits?, parent?, task,
                  usage?: { input_tokens, output_tokens, cache_read_tokens,
                            cache_creation_tokens, peak_context } }],
  "decisions": [{ id, run_id, lane_id, created, answered?, question }],
  "requests":  [{ id, kind, state, from_run, from_lane_id, to_lane_id?,
                  target_run?, created, updated }],
  "spend":     { day, budget, steps: [{ at, usd, credits?, source: "run" | "desk" }] },
  "truncated": false
}
```

- **Window semantics.** A run is in when `started < to` and (`ended >= from` or it has not
  ended). A lane is in when it has a run in the window, was created or closed in it, or has
  a decision waiting. Open lanes with nothing in the window are left out of the answer and
  offered by a "show idle lanes" switch that asks for them with `idle=1`.
- **`merged_at`** is the forge's `mr_updated` when `mr_state` is `merged`, else the lane's
  `closed` when its outcome is `merged`. It is a time as of the daemon's last poll, and the
  doc says so; the hover shows "merged about 14:07".
- **`spend.steps`** come from the `usage` table's own `created` and `cost_usd`, plus the
  run's `ended` and `cost_usd` for any run with no usage record (older runs, Copilot and
  Cursor, which report cost only). The curve therefore ends at the day's total from
  `GET /v1/spend`. A running run adds its cost when it ends, since the agents report cost
  at the end; the band says so in its hover ("a running run adds its cost when it ends").
- **Bounds and safety.** At most 1000 runs, newest kept, with `truncated` set so the page
  can say so. `task` and `question` are clipped and go through `internal/redact`, as the
  history endpoints do. The window is capped at 31 days.
- **No new store tables.** One new read in `store.Store` (`TimelineRuns` joining runs and
  usage, `DecisionsBetween`, `RequestsBetween`), additive, with the existing `Decisions` and
  `Requests` left alone. No migration is needed.

Alternative considered: keep the endpoints and extend each (`history/runs?overlap=1` and a
`usage` field on a run summary, `since` on decisions and requests, lane ids on both, a new
spend-steps read). It adds no new route but touches five handlers that the board, the
Runs page and the MCP tools already depend on, and the page still makes six calls per
workspace. The single endpoint keeps the timeline's shape in one place and leaves the
others untouched.

### Live updates

The page uses what the app already has and adds nothing to polling:

- Backfill from `/v1/timeline`, fetched with the same `useHistory` hook the board uses,
  which refetches when the feed's cursor moves. A short throttle (no more than one fetch
  per 5 seconds) keeps a busy feed from refetching on every commit.
- Overlay from the `Live` snapshot, which is already fresh: `runs` with `progress`, open
  `decisions`, `requests` needing routing, `spend`. A run that started a second ago shows
  at once from the snapshot, before the next timeline fetch reaches it.
- **Growth of a running bar is the clock, not data.** Its right edge is "now"; nothing is
  fetched to make it grow (see section 3).

Phase 1 can ship with no daemon change at all: it reads `/v1/runs`, `/v1/lanes`,
`/v1/decisions` and `/v1/requests` from the snapshot, plus `/v1/history/runs` and
`/v1/history/lanes` for the Today range. It misses the straddling runs (gap 1) and has no
tokens, which phase 2 fixes. That is deliberate: the wow is on screen first.

## 3. Rendering

**Recommendation: SVG, hand-written, no charting dependency.**

| Question | Answer |
|---|---|
| Why SVG and not canvas? | Each bar, marker and arc needs hover, keyboard focus, a link and an accessible name. SVG elements give all four for free; canvas would reimplement hit-testing and lose the accessibility tree. Crisp at 2x for the screenshot. |
| How many marks? | A heavy day is 40 lanes, 12 runs each, 500 bars, about 150 markers and 30 arcs. That is well inside what SVG handles smoothly. A 7-day view of 1000 runs is still fine. |
| Level of detail | A bar narrower than 2 px at the current scale is merged with its neighbours on the same track into one density tick. This is a pure function over the model, and it is what keeps 7 days readable. |
| Many tracks | Past about 30 visible rows the track list scrolls inside the page, with the time axis and the spend band sticky. Collapsing a repo group (the board already stores collapsed groups in local storage) hides its tracks. |
| A library? | No. The page needs a linear time scale and tick generation at local-time boundaries (15 minutes, hour, 3 hours, day): about 80 lines in `model/timeline.ts`, unit tested. d3 or a chart kit would be a heavy dependency for that and would fight the CSP and the house style. |

**Live without re-rendering everything.**

- The model layer (`model/timeline.ts`) turns the data into positioned marks as a pure
  function of the data and the visible window. Marks keep object identity when nothing
  about them changed, and each bar is a memoised component keyed by run id, so a new
  fetch re-renders only the bars that changed.
- Running bars and the "now" line are moved by **one** timer that sets attributes
  directly through refs (once a second, and not at all while the tab is hidden). React
  does not re-render on that tick. The visible window's end is recomputed every 15 seconds,
  which is when React re-renders positions, with a 2% headroom so nothing runs off the
  edge between recomputes.
- The pulse is CSS, animating `opacity` and `transform` of a small cap element only (never
  width or position), so it stays on the compositor.

**Constraints from the web UI's setup.**

- The CSP is `style-src 'self'`: no inline `<style>`, and no `style="..."` attribute in
  markup. Positions are SVG attributes (`x`, `width`), and any computed style is set
  through React's `style` object, which sets it through the DOM and is allowed (this is how
  the app's existing meters and the context chart work).
- `font-src 'self'`: no web fonts from elsewhere. The page uses the app's existing
  stacks (`--font-ui`, `--font-mono`), so there is nothing to bundle.
- Text that comes from agents (a task line, a question) is inserted as text content only,
  never as markup, as the rest of the UI does.

## 4. Visual design

### Direction

The product's own tokens already say "fog and slate by day, night pasture by night, with
one warm colour, the lantern, kept for what waits on the person". The timeline takes that
literally. It is **the pasture seen from above at night**: the tracks are quiet rails, the
agents are lit paths moving along them, and the only warm light on the page is a lantern
at each decision that waits for you. That is the one memorable thing: you can see, from
across the room, where the flock is held up on a person.

This is a choice, not the default. The cliches to avoid here are a dark dashboard with an
acid accent, cards in a grid, and tracks drawn as a Gantt table with zebra stripes.

| Element | Treatment |
|---|---|
| Tracks | A 1 px hairline rail in `--rule`, not a striped row. No row backgrounds, so the bars carry the picture. Hover or focus on a lane lights its rail and its arcs. |
| Repo groups | A left-aligned group heading in `--font-ui` 600 at `--t-sm`, with the lane count in `--faint`, as the board's group heads. Not upper-case. |
| Lane names | `--font-ui` 600 at `--t-sm`, clipped with a title, in the left gutter; repo shown only by the group. |
| Runs | A bar 0.9rem tall with a 4 px rounded end, a 2 px gap between neighbours, coloured by agent. A bar wide enough carries its agent's name in `--t-2xs`; a narrow one does not. |
| Running | The bar's right end fades through a soft gradient into a pulsing cap in the same agent colour, 2.4 s ease-in-out, opacity 0.55 to 1. Nothing else moves. |
| Failure | A red notch cut into the bar's right end (a shape, not only a colour), with a `✗` in the tooltip and the table. Uses `--broken`. |
| Decision waiting | A lantern glow: a small lantern-coloured diamond on the track at the moment of asking, with a soft radial halo that breathes once every 3.5 s while it waits, joined to the bar's end by a short line. Answered decisions leave a dim `?` at the same place. Uses `--lantern`. |
| Merge | A short vertical tick on the track in `--ok`, with a `◆` glyph. |
| Routed request | A thin arc (1 px) from the asking bar to the receiving lane's bar, in `--muted` at 55% opacity, with a 3 px dot at the receiving end. Arcs bow away from each other where they would overlap. |
| Now | A 1 px vertical line in `--ink` with the time in mono above it. |
| Time axis | Mono tabular numerals in `--faint` at `--t-xs`, local time, at hour or 3-hour ticks. Day boundaries are named ("Mon 6 Oct"). |
| Spend band | 3.5rem tall, under the tracks and sharing their x axis: a stepped area in `--lichen` at 25% fill with a 2 px line, and a dashed `--ink` line at the daily budget, labelled at its right end ("$25 budget"). It also serves as the brush for scrubbing (phase 3). Past the budget the fill above the line turns `--broken`, with the words "over budget" as well. |

### Colour by agent

The agent hues must not be amber, red or green: those belong to `--lantern`, `--broken`,
`--ok` and `--lichen`, and a status colour must never impersonate a series. That leaves
the blue to magenta arc, with lightness doing the rest of the separating.

I ran the dataviz validator over candidates in that arc (OKLab, protan and deutan
simulated, **all pairs** because any two agents' bars can sit on adjacent tracks):

| Mode | Result of the search |
|---|---|
| Light (surface `#f8fafb`) | Four-colour sets exist with every mark at 3:1 or better and a worst normal-vision difference of about 16 (floor 15). The worst colour-blind pair is about 7, in the validator's WARN band, which is legal only with a second channel. |
| Dark (surface `#1c242b`) | Four-colour sets exist at a worst normal-vision difference of 16 to 17 and a colour-blind pair of 9 to 10, but the deepest hues (the blue and the plum) fall to about 2.4 to 2.6:1 against the surface, which the validator reports as "relief required". They need a lighter step in the final pass, or the printed name and table view as relief. |

Indicative light values from that run, before the final pass:

| Agent | Light (indicative) | Hue |
|---|---|---|
| claude | `#0f68a2` | ink blue |
| opencode | `#7d8bdb` | periwinkle |
| cursor | `#8c467e` | plum |
| copilot | `#d4679f` | rose |

The dark values are not chosen yet: the search above is a feasibility result, not a
palette. The final hexes, light and dark, are fixed by running `validate_palette.js` per
mode when the page is built (`--pairs all`, each mode's own surface), and the report goes
in the phase 1 merge request. The rules that do not change:

- **Colour follows the agent, never its rank or the filter.** The order of the four is
  fixed. An agent the page does not know (a future adapter) gets a neutral slate with its
  name always printed.
- **Because the colour-blind pair is in the WARN band, the second channel is mandatory**:
  the agent's name is printed on any bar wide enough, and the legend and table view
  always carry it. In forced-colors mode each agent also takes a hatch angle.
- The legend sits above the chart in one row, mirroring the marks (a bar swatch, not a
  dot), and doubles as an agent filter (the Runs page's agent filter, same address
  parameter).
- Text never wears an agent colour; names and values stay in `--ink`, `--muted`, `--faint`.

### Type

No new face. Lane names and group heads use `--font-ui` (the app's Avenir Next to Segoe
stack); every time, cost and token count uses `--font-mono` with tabular numerals, so the
axis and the tooltips line up. The scale is the app's own (`--t-2xs` to `--t-xl`), with
nothing below `--t-2xs`. The page's one big type is its heading ("Timeline") and a single
summary line under it in `--muted`: "3 running, 1 waiting on you, 2 merged, $14.20 of $25".

### Motion

Three things move without being asked, and only these: the leading-edge pulse, the
lantern's slow breathing, and the "now" line. Everything else answers a touch: a bar
lifts 1 px and its colour steps lighter on hover, arcs fade in on lane hover, the range
change crossfades the bars over 160 ms. Under `prefers-reduced-motion: reduce` (which
`theme.css` already honours globally) the pulse and the breathing stop and are replaced
by static markers: a solid cap with a ring, and a solid halo. Nothing is lost; the waiting
decision is still the brightest thing on the page.

### Empty, quiet and busy

| State | What shows |
|---|---|
| Nothing ran in the range | The axis and an honest sentence: "No run started today. Start one with `shepherd lane run`, or ask the front desk." No ghost bars. |
| Daemon unreachable | The last render held at 55% opacity, under the app's existing red banner, as the history pages hold their data. |
| One lane | A single, taller track; the arcs and spend band are unchanged. |
| A quiet day | Idle lanes are hidden; "show idle lanes (4)" is a link. |
| Busy (the hero) | Tracks pack at 1.75rem a row; repo groups are collapsible; the legend filter dims the others to 25%. |
| Truncated | "Showing the newest 1000 runs of 1,340" in `--faint` under the chart. |

### Narrow screens

Under 760 px (the app's existing breakpoint) the lane names move onto the rail as a
second line above each track instead of a left gutter, the range control becomes a
select, arcs are dropped (they read as noise at that width), and the spend band keeps its
height. The table view below stays the same.

### Accessibility, and the table

- Every bar is a link with an accessible name: "claude, feat/export-csv, running 14 minutes,
  2 commits, $0.84." It takes keyboard focus in lane order, with the same details as
  hover. Arrow keys move along a track and between tracks.
- Below the chart, a "View as table" disclosure holds the same runs as a sortable list
  (the Runs page's row, reused), so nothing is reachable only by hovering.
- The tooltip lists values first and labels after, uses the agent's colour only as a short
  key stroke, and sets all agent text as text content.
- Focus uses the app's `--focus` ring. Each marker has a shape of its own (diamond, tick,
  notch), so none depends on colour alone.

### Hover and click

| Target | Hover shows | Click |
|---|---|---|
| A run | Agent and model, state, start to end and duration, cost, tokens (input, output, cache read and write, peak context), commits, first line of the task | Opens the run's log |
| A decision | The question, how long it has waited, the recommended option | Opens the decision |
| A merge | The merge request number and when | Opens the lane |
| A failure | The run's diagnosis summary when there is one | Opens the run |
| An arc | "claude in feat/a asked opencode in feat/b", kind, state | Opens the request on the Decisions page |
| The spend band | Cumulative spend at that time and what the last step was | None |

The tooltip is one component positioned from the pointer, flipped near edges, and also
shown on keyboard focus.

## 5. Testing

**Unit tests (Vitest, in the existing layout):**

| File | What it checks |
|---|---|
| `model/timeline.test.ts` | Window and range maths (Today from local midnight, across a daylight-saving change, 24 h, 7 days). Lane and group ordering. Overlap clipping (a run begun before the window is drawn from the left edge, flagged as continuing). Level of detail merging. Tick generation. Spend steps to a cumulative curve and the budget crossing. Arc end points, including a request with no receiving lane yet. Stable identity of unchanged marks. |
| `model/timeline.fixtures.ts` | A busy day built in code: 24 lanes in 5 repos, 4 agents, a straddling run, a failure, an answered and an open decision, three routed requests, two merges, a quota gap. Shared with the stub. |
| `components/Timeline.test.tsx` | The page renders one track per lane, a bar per run, the right markers, an accessible name on each bar, the table view, and the empty and truncated states. Hover and focus show the same tooltip. Reduced motion removes the animated classes. |
| `pages/Timeline.test.tsx` | The route, range links and address parameters, the agent filter, and a refetch when the feed moves (with the throttle). |
| Go: `internal/api` and `internal/daemon` | Window semantics (a straddling run in, a finished one before the window out), lane ids on decisions and requests, spend steps ending at the day's total, redaction of `task` and `question`, the 31-day cap and `truncated`, and the same role gate as the history routes. Store tests build a real database as the history tests do. |

jsdom has no layout, so geometry (bar widths, label fit, arc collisions) is covered by
unit tests on the model's numbers, and the look by screenshots.

**Screenshots, with the existing stub tool (`web/shots/`):**

- `stub-daemon.mjs` gains `/v1/timeline`, built from the current time as its other data
  is, and a `BUSY=1` switch that generates a heavy day: about 24 lanes across five repos,
  each with 3 to 12 runs through the working hours, all four agents, two failures, a
  decision waiting and two answered, six routed requests, three merges, a running bar in
  five lanes, and a spend curve that crosses 80% of the budget. The default stub stays
  small.
- `shoot.mjs` gets `timeline` in its page list, taken as `timeline-light.png` and
  `timeline-dark.png`. For the blog image the same script runs at `SHOTS_SIZE=1600x900`
  and `SHOTS_SCALE=2`.
- The pulse and breathing are frozen for the capture (the script already emulates media
  features, so it adds `prefers-reduced-motion: reduce`), which makes the image
  repeatable. The static markers are designed to look right in the still.
- `web/shots/README.md` documents the new page and the `BUSY` switch.

Each phase's merge request attaches its before-and-after screenshots; there is no
automated visual diff and this proposal does not add one.

## 6. Files it will touch

The lane's scope is `docs/swarm-timeline.md` only; building it needs the scope widened.
**`web/src/styles/**` is held by `fix/chat-split`, and this plan avoids it**: the page's
CSS lives in a new folder beside its code (the board already imports a page stylesheet
from a component, `../styles/projects.css`), and the agent tokens are scoped to the
timeline's root, so `theme.css` and `app.css` are not edited. If the maintainer prefers the
tokens in `theme.css`, it is a two-line move once that lane lands.

| Path | Change | Phase |
|---|---|---|
| `web/src/timeline/timeline.css` | New. Layout, marks, motion, agent tokens in light and dark, reduced motion. | 1 |
| `web/src/model/timeline.ts`, `timeline.test.ts` | New. Pure model: window, positions, level of detail, arcs, spend curve. | 1 |
| `web/src/components/Timeline.tsx`, `TimelineTooltip.tsx`, `Timeline.test.tsx` | New. The SVG chart, tooltip and legend. | 1 |
| `web/src/pages/Timeline.tsx`, `Timeline.test.tsx` | New. The page, range control and table view. | 1 |
| `web/src/router.ts`, `router.test.ts` | A `timeline` route with `range` and `agent`, and `href.timeline`. | 1 |
| `web/src/App.tsx` | A nav entry and the `g t` key. | 1 |
| `web/src/api/types.ts`, `web/src/api/client.ts`, `client.test.ts` | `Timeline` types and `timeline()` fetcher. | 2 |
| `web/src/test/fixtures.ts` | Timeline fixtures if they are shared. | 1 |
| `web/shots/stub-daemon.mjs`, `shoot.mjs`, `README.md` | The endpoint, `BUSY`, the page in the list. | 1 and 2 |
| `web/README.md` | The Timeline page and its endpoint in the tables. | 2 |
| `internal/api/timeline.go`, `timeline_test.go` | New. Types, `LaneOutcome`-style helpers, the handler. | 2 |
| `internal/store/store.go`, `internal/store/sqlite/timeline.go`, `timeline_test.go` | New reads, additive. No migration. | 2 |
| `internal/daemon/daemon.go` | Register `GET /v1/timeline` for the same role as the history routes. | 2 |
| `internal/daemon/auth_test.go` | The route in the role table. | 2 |
| `CHANGELOG.md` | An Unreleased entry per phase. | each |
| `docs/roadmap.md`, `docs/web-*.md` | One line each, if the maintainer wants the page indexed. | 3 |

Nothing in `internal/` names a product, so `core-boundary` is unaffected, and nothing here
needs a new Go or npm dependency.

## 7. Phases

Each phase is one merge request with screenshots, and each leaves the app working.

| # | Lands | What is on screen | Needs daemon change |
|---|---|---|---|
| 1 | **The picture.** The page, route and nav entry, SVG tracks grouped by repo, bars by agent, running bars growing with the pulse, decision glow, merge ticks, failure notches, the spend band against the budget, Today / 24 h / 7 days, the legend, hover with what the snapshot has (agent, model, cost, commits, duration) and click-through, the table view, light and dark, narrow layout, reduced motion, the busy stub and screenshots. Built from `Live` and the existing history endpoints. | The hero screenshot, minus arcs and tokens. | No. |
| 2 | **The truth.** `GET /v1/timeline`: straddling runs, per-run tokens, lane ids, timestamped spend steps, `merged_at`, routed-request arcs, the idle-lanes switch, and the truncation note. The page moves from the snapshot-and-history stitching to the one call. | Arcs, tokens in the hover, an exact spend curve, a correct "Today" across midnight. | Yes. |
| 3 | **The craft.** Scrub and zoom (wheel with a modifier, drag to pan, the spend band as a brush, `+` and `-`), keyboard walking, the board strip if wanted, commit ticks on bars from the feed, docs and index entries. | A day you can move through. | No. |

Phase 1 can be reviewed and shot before any Go is written, which is the point of the
order: the picture is what the person is buying, and the endpoint only makes it exact.

## Risks and what is not promised

- **Spend is as the agents report it.** Cost arrives when a run ends; Copilot and Cursor
  report credits or nothing. The band says so rather than interpolating a running cost.
- **Merge times are as of the last forge poll.** The tick can sit a minute or two after
  the merge.
- **Four colours in a colour-blind-safe set is tight.** The validator puts the worst pair
  in its WARN band; the name on the bar is what makes the page safe, so it is not optional.
- **A busy week is a lot of ink.** Level of detail and the legend filter carry it; if the
  7-day view still reads as noise, it becomes lane-level density ticks and the day view is
  the picture to share.
- **No scrub or zoom in phase 1.** The three presets are the controls until phase 3.
- **The `docs/.ui` proposal's live rail (a vertical dock beside every page at 1800 px and
  up) is a different feature**, and this page does not depend on it. At that width the
  timeline simply gets the shell's width, as the board does.
