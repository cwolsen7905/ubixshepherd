# Timeline

One view of the day's work at once: a horizontal track per lane, grouped by repo,
time running left to right, each run a bar coloured by its agent. The point is to
make Shepherd's parallelism visible at a glance: many agents in many lanes, not
colliding.

The page lives at `#/timeline`, with a Timeline entry in the nav (key `g t`). The
board stays the place that answers "what needs me"; the timeline is the picture
for the day.

## Ranges

Today (the default), Last 24 hours, and Last 7 days. A range's answer is read when
the range opens and again when the feed moves (throttled to at most one read per
5 seconds), and a failed read keeps the last answer with the error shown. Running
bars grow with the clock between reads; nothing is fetched to move them.

## What is drawn

- **Tracks**: one per lane with something in the window, grouped by repo. Lanes
  with nothing in the window are hidden behind a "show idle lanes" switch.
- **Runs**: a bar per run, coloured by agent. A bar wide enough carries its agent's
  name; a narrow one does not.
- **Running**: the bar's right edge fades into a pulsing cap in the same agent
  colour, with the now-line beside it.
- **Waiting**: a lantern diamond on the track at the moment of asking for an open
  decision or a request that needs routing, joined to the asking bar. Answered
  decisions leave a dim `?` at the same place.
- **Merge**: a short tick on the track in `ok`, with a `◆` glyph, at the moment of
  the merge as of the daemon's last forge poll.
- **Failure**: a red notch cut into the bar's right end for a failed or interrupted
  run, with a `✗` in the tooltip and the table.
- **Spend band**: under the tracks, sharing their x axis, a stepped area in lichen
  for cumulative spend today with a dashed line at the daily budget. A running run
  adds its cost when it ends, since agents report cost at the end; Copilot and
  Cursor report credits or nothing, which the band says in its hover.

Every bar is a link with an accessible name (agent, lane, state, duration,
commits, cost): it opens the run's log (`#/runs/ID/log`), a track's name opens
the lane (`#/lanes/ID`), and a decision marker opens the decision. Below the chart,
a "View as table" disclosure holds the same runs as a sortable list, so nothing is
reachable only by hovering. The legend doubles as an agent filter; the page keeps
the newest 1000 runs with a note when the daemon counted more.

On a narrow screen (the app's one breakpoint, 760 px) the range control becomes
a select. Under `prefers-reduced-motion` the pulse and breathing stop and are
replaced by static markers. When the daemon is unreachable the last render is
held under the app's banner, as the history pages hold their data.

## Where the data comes from

The page reads what the app already polls plus the history: the live snapshot
(lanes, runs with progress, open decisions, requests needing routing, spend) for
now, and `GET /v1/history/runs`, `GET /v1/history/lanes`, and answered decisions
for the range. There is no dedicated timeline endpoint. A run that began before
the window and is still in it is drawn from the window's left edge; spend over
time is the day's total from the daemon plus the runs' own costs, not a
per-moment curve. Merge times are as of the last forge poll.

Colour follows the agent, never its rank or the filter. The agent's name is
printed on any bar wide enough and always in the legend and table view, so no
meaning depends on colour alone. Text never wears an agent colour. Lantern means
waiting on the person and nothing else; red is failures only.
