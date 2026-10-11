# Shepherd web UI

A browser client of the Shepherd daemon's HTTP API, like the CLI and `shepherd chat`. It
shows the board (lanes grouped by what they need from you, with the terminal dock's rules,
and below them the lanes that finished), a lane's runs and timeline, the open decisions,
every run, any run's log, and the front desk's chat, live. What it changes is a decision's answer and what you say to the front desk, only from
your click or your Enter.

Vite, React 19 and TypeScript (strict). No router, no state library, no CSS framework.

## Use it

The daemon serves the built app on its own address. Sign a browser in with:

```sh
shepherd web          # opens a one-time sign-in link in your browser
shepherd web --print  # prints the link instead (a shared machine, or a tunnel)
shepherd web --sign-out-all
```

The link works once, for a minute. Opening it gives the browser a session cookie and
sends it to the board; the browser never sees the operator token. The session lasts 12
hours, or until you sign out, run `shepherd web --sign-out-all`, or the daemon restarts.

From another machine, use an SSH tunnel to the daemon's loopback port. Pin the port in
the daemon's config (`daemon.listen: 127.0.0.1:7400`), then:

```sh
ssh -L 7400:127.0.0.1:7400 HOST      # leave it open
ssh HOST shepherd web --print        # open the printed link here
```

## History

The board ends with a **Finished** section: lanes closed in the chosen range, newest first,
each with how it ended (**merged** with its merge request's number and link, **MR closed**,
**dropped** for a lane closed while its merge request was still open, or **closed, no MR**),
its number of runs, its cost and when it closed. A row opens the lane page, which works for
a closed lane too. The **Runs** page lists every run in the range with its agent, lane,
outcome, commits, cost, start, duration and the first line of its task, filtered by agent and
by lane, and totals the spend for the range.

Both take **Today** (since local midnight, the default) or **Last 7 days**. The choice is in
the address (`#/?range=week`, `#/runs?range=week&agent=claude&lane=12`), so a reload or a
shared link keeps it. They read two endpoints beyond `/v1/lanes` and `/v1/runs`, which
list open lanes and the newest 200 runs:

| Endpoint | Answers |
|---|---|
| `GET /v1/history/lanes?workspace_id=&state=&since=` | Lanes with run count, cost and outcome. `state` is `closed` (default), `open` or `all`; `since` (RFC 3339) keeps lanes closed at or after it. `lane_id=` returns one lane in any state. |
| `GET /v1/history/runs?workspace_id=&since=&agent=&lane_id=&state=` | Runs newest first, without prompts, with the count and cost of every match and the agents and lanes the range holds. |

The daemon does not record whether a close was forced, so a lane's outcome is the forge's
last word on its merge request.

## Usage

The **Usage** page answers whether a 1M-context model is worth its cost, from
`GET /v1/usage?from=DAY&to=DAY` (see [usage-stats.md](../docs/usage-stats.md)). Its range is
**Today**, **Last 7 days** (the default), **Last 30 days** or **Custom** with two day pickers,
and is in the address (`#/usage?range=custom&from=2026-10-01&to=2026-10-07`). It shows:

- tiles: total cost, the desk's cost against the agents', runs and turns, and how many
  records peaked over 200k;
- a chart of the largest context each agent and model, desk model and lane reached, against
  a line at 200k and a tick at the model's window when it fits the scale;
- sortable tables by agent and model, for the front desk and by lane, with tokens (input,
  cache read, cache write, output), peak context, compactions and cost.

The endpoint answers totals per group, not per record, so the chart shows each group's
largest peak and how many of its records went over 200k, not one mark per run. Copilot and
Cursor runs carry cost only and are left out of the chart.

## Timeline

The **Timeline** page (`#/timeline`, key `g t`) shows the day's work at once: a track per
lane, grouped by repo, time running left to right, each run a bar coloured by its agent.
It is the picture of what Shepherd's parallelism looks like. The design is
[docs/swarm-timeline.md](../docs/swarm-timeline.md); this is the first phase of it, built
from the live snapshot and the history endpoints above, with no daemon change.

- **Marks.** A run is a bar; a running one fades into a pulsing cap and grows with the
  clock. A failed run has a notch cut into its right end. A decision or request waiting on
  you is a lantern on its lane, which is lit along its rail; an answered decision leaves a
  dim `?`. A merge is a tick with a diamond, at the forge's last report of the merge. Runs
  too narrow to see (under 2 px) merge into one density tick.
- **Spend.** A band under the tracks shows the day's spend as it stepped, from each run's
  cost at the moment it ended, against the daily budget line. What the runs do not account
  for (the front desk's turns) is placed at now. It falls back to nothing at each midnight,
  and past the budget the part above the line turns red and says "over budget". A running
  run adds its cost when it ends.
- **Ranges.** **Today** (the default, from local midnight), **Last 24 hours** and **Last 7
  days**, in the address (`#/timeline?range=24h&agent=claude`). The agent in the legend
  filters: the others are dimmed to a quarter, not removed. A repo's tracks collapse, and
  "Show idle lanes" adds the open lanes that did nothing in the range.
- **Hover, focus and click.** Every mark takes keyboard focus and shows the same tooltip as
  hover. A bar opens the run's log, a lane's name opens the lane, a lantern opens the
  decision, a merge opens the lane. **View as table** below the chart lists the same runs,
  sortable, so nothing is reachable only by hovering.
- **Colour.** The four agents have fixed colours (light and dark) that were checked with
  the dataviz palette validator, all pairs, against each mode's surface. The agent's name
  is printed on a bar wide enough for it, and in the legend and table, because the
  colour-blind pairs are only in the validator's WARN band. An agent the page does not
  know gets a neutral slate. In forced-colors mode each agent takes a hatch angle. The
  tokens are in `src/timeline/timeline.css`, scoped to the page.
- **Motion.** Three things move unasked: the cap's pulse, the lantern's breathing and the
  now line. Under `prefers-reduced-motion: reduce` they stop and static markers take their
  place (a solid cap with a ring, a solid halo). One timer, once a second and not while
  the tab is hidden, sets the running bars' and the now line's attributes directly; React
  re-renders on a 15 second tick and when the lists or history change.

What it reads: the snapshot's lanes, runs (with progress), open decisions, requests that
need routing and spend, plus `GET /v1/history/runs` (newest 1000 per workspace, from
midnight of the range's first day or six hours before it, whichever is earlier, so a run
that began before the range is mostly there), `GET /v1/history/lanes` for lanes closed in
it, and `GET /v1/decisions?state=answered` for the dim `?` marks, which the daemon cannot
yet bound by time. The history is read again when the feed moves, at most once every 5
seconds. Until the daemon has a timeline endpoint (the plan's second phase), the picture
misses a run that began before the history window, has no tokens in the tooltip and draws
no arcs between lanes; the merge tick sits at the forge's last report, which can be a
minute or two after the merge.

## Build and develop it

You need Node 22 or later. `make build` at the repo root builds the app and embeds it in
the binary (`internal/webui`); without npm, or with `NO_WEB=1`, the binary builds without
it and serves a page saying how to build it. For development, run the dev server against
a running daemon (`shepherd daemon start`, or any `shepherd` command, which starts one):

```sh
cd web
npm install
npm run dev      # http://localhost:5178
npm run build    # the static app in web/dist
npm run check    # typecheck, lint and tests
```

## How it talks to the daemon

The app calls the API by root-relative URL (`/v1/lanes`, `/v1/feed`, ...), so it works
unchanged wherever it is served. It never sees a token.

Served by the daemon, the browser holds a session cookie (`HttpOnly`, `SameSite=Strict`,
for `/v1` only). Every request a browser makes must name a loopback host, and must not
come from another origin or site; a request that changes something also sends the
session's CSRF token, which the client reads once from `GET /v1/web/session`, in
`X-Shepherd-CSRF`. The daemon grants no CORS. Pages forbid framing and load nothing from
another origin (a strict Content-Security-Policy). A browser session can do what the
front desk can, talk to the front desk and answer decisions; the daemon's setup
(workspaces, scopes, hooks, imports, settings, shutdown) stays with the `shepherd`
command.

In development, `daemon-proxy.ts` is a dev-server middleware that forwards `/v1` to the
daemon. It reads the daemon's address and token from its runtime file, `daemon.json` in
`SHEPHERD_HOME` (`~/.shepherd` by default), on every request, so a restarted daemon is
followed, and adds the token to the forwarded request only. Nothing prints or logs it.
Since it adds the operator's token, it makes the daemon's browser checks itself first (a
loopback host, nothing from another origin or site, and an Origin on every write), and
forwards none of the browser's cookies or origin headers.

### Endpoints for the web

| Endpoint | What |
|---|---|
| `GET /v1/web/session` | The caller's role; for a browser, its CSRF token and expiry. A 401 means not signed in |
| `POST /v1/web/signout` | End this browser's session |
| `/v1/desk/*` | The front desk conversation (see `internal/api/desk.go`), open to a browser session |
| `GET /v1/feed/stream`, `GET /v1/desk/stream` | Server-sent events, for `EventSource` on the same origin |
| `POST /v1/web/signin`, `DELETE /v1/web/sessions` | Operator only: what `shepherd web` uses |

### The chat page

`#/chat` is a thin client of the daemon-hosted front desk, the same conversation `shepherd
chat` shows. It reads the thread from `GET /v1/desk/history`, then follows
`/v1/desk/stream` with an `EventSource`. An `EventSource` sends no headers, so the stream
resumes by `?after=<seq>` of the last stored event; after a drop the page reopens it with a
backoff (0.5 s doubling to 15 s), asks `/v1/desk/status` why when it fails (a 401 stops
the retries and shows the sign-in hint, `shepherd web`), and shows the connection state in
the page head. A `partial` piece carries no id: pieces grow one message, and the stored
`assistant` event replaces them, as in the terminal.

- Enter sends (`POST /v1/desk/turn`, with the CSRF header like every write), Shift+Enter
  adds a line. The box keeps your words if the daemon refuses: a full queue (429), a
  signed-out session (401) and a refusal (403) each say so.
- Interrupt shows while a turn runs. New conversation asks first; the daemon rotates the
  session.
- Events from the feed (a run ending, quota, a decision asked, and so on) appear as
  compact rows between messages with the board's glyphs. An open decision shows inline with
  its options and is answered through the same `Live.answer` call as the Decisions page.
- Everything a model or agent wrote is rendered as text or a small markdown subset
  (paragraphs, headings, lists, fenced code, `**bold**`, `` `code` ``, and links) built as
  elements: no `innerHTML`, no raw HTML, and links only for `http` and `https`, with
  `rel="noopener noreferrer"`.

Updates elsewhere come from polling: the feed with its cursor every 1.5 s (10 s while the tab is
hidden), which also triggers a reload of the lists; the lists themselves every 5 s (30 s
hidden). A server-sent event stream will replace the feed poll.

## Layout

| Path | What |
|---|---|
| `src/api/` | The API's JSON types (from `internal/api` and `internal/store`) and the client |
| `src/state/live.ts` | The one live view of the daemon: lists, the feed window, polling, what you have seen |
| `src/model/` | Pure logic: the board's grouping (`board.ts`, the dock's rules), event marks, feed refs, the log parser |
| `src/state/desk.ts` | The front desk thread: history, the event stream, resume, sends |
| `src/pages/` | Board, Lane, Decisions, Log, Chat, Timeline |
| `src/timeline/` | The timeline's stylesheet (agent colours, marks, motion, narrow layout) and its history reads |
| `src/styles/` | `theme.css` (tokens, light and dark) and `app.css` |

## Keys

`j`/`k` move on the board, `Enter` opens, `g b`, `g d` and `g t` go to the board, decisions
and timeline,
`c` marks everything done as seen, `/` searches a run log (`Enter`, `n`, `N` step through
matches), `t` expands tool calls, `f` follows the log, `?` lists the keys.
