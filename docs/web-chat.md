# Web chat: the backlog rail

The chat page in the web UI (`web/src/pages/Chat.tsx`) shows the front desk's thread with
a backlog beside it: the work that waits on the person or has gone quiet. It reads only
what the board already polls (lanes, runs, decisions, requests, spend, the feed), so it
needs no daemon change. The terminal chat has its own dock and is not touched.

## What is listed

The rules come from the board (`web/src/model/board.ts`), so the two never disagree
about what is broken or working. The model is `web/src/model/backlog.ts`. Most urgent
first, and within a group the longest-waiting first:

| Group | What it holds | Age is |
|---|---|---|
| needs you | open decisions, then requests needing routing | since asked |
| broken | a failed or interrupted run, or an open MR whose pipeline failed | since the run ended |
| idle | an open lane with no run in flight and nothing to wait for | since its last run ended, else since the lane was created |
| working | a run in flight, with its progress line | since it started |
| to review | an open MR whose pipeline passed | lane age |

Left out: finished work (the board's "done unseen" is history), closed lanes, a lane
waiting on a running pipeline (that is waiting, not idle), a lane the person has already
been asked about (it is listed once, as needing them), and runs the person dismissed.

## Rows and links

A row shows the lane (qualified with its repo when two repos share the name), one line of
detail, its age and the MR and pipeline badge. The name leads to the decision, or to the
lane's page. **in thread** jumps to the thread row where the work last spoke and flashes
it: the latest typed row about the lane that the loaded thread shows (a run's end, its
MR, a decision). An open decision always has a place in the thread, in its own row or at
the bottom, so it can be answered there. Work with no loaded thread row has no jump,
including work older than the thread's loaded window; use the lane's page then.

## Layout

On a wide screen the rail takes about a third of the width, sticky beside the thread; it
shows 8 rows, then "+N more" expands the list and links to the board. On a narrow
screen it sits above the thread and its header is the counts bar, opened by a tap. The
choice is kept in the browser's storage (`shepherd.chat.backlog`); without one, it starts
open on a wide screen and closed on a narrow one.

The rail's header also carries today's spend against the budget (warning colour from
80%) and a notice for any agent held for quota, read from the daemon's detailed status.
The window title already carries the board's counts, so a background tab shows that
something waits.

## What it cannot know yet

- **Idle is approximate.** The daemon has no lane-activity time, so idle runs from the
  last run's end, else the lane's creation. A lane edited by hand looks idle.
- **MR age.** The lane view has no MR or pipeline timestamps; the lane's age stands in.
- **Anchors need the feed.** A jump needs the row to be in the feed window the page holds
  (the newest 1000 items) and in the loaded thread.

True lane activity and forge timestamps would be a separate daemon change.
