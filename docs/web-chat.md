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
| needs you | open decisions, requests needing routing, and an open MR held at the require-approval job and not yet approved ("approve MR !N") | since asked; for an MR, since its pipeline reached that job (finished, if it failed there), else started, else the MR last changed |
| broken | a failed or interrupted run, or an open MR whose pipeline failed | since the run ended, or the pipeline finished |
| idle | an open lane with no run in flight and nothing to wait for | since the lane's last activity (a run, commit, decision, request, MR update or pipeline), which the row names: "last: commit" |
| working | a run in flight, with its progress line | since it started |
| to review | an open MR whose pipeline passed | since the pipeline finished, else since the MR last changed, else lane age |

An MR counts as waiting for approval when the lane view's `require_approval` (the status of the
head pipeline's require-approval job) is pending, manual, blocked, created, scheduled or failed
and `approved` is not set. A pipeline that failed at another job stays broken. Once the person
approves and the job runs again the lane drops back to waiting. The row leads to the lane's
page, where the Changes tab has the Approve button. Both fields come from the forge poll.

Left out: finished work (the board's "done unseen" is history), closed lanes, a lane
waiting on a running pipeline (that is waiting, not idle, unless it is held at require-approval, above), a lane the person has already
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
80%), the forge poll's health (when it last polled, or its error, which means MR badges
may be stale) and a notice for any agent held for quota. The last two are read from the
daemon's detailed status every 30 seconds.
The window title already carries the board's counts, so a background tab shows that
something waits.

## What it cannot know

- **Forge times are as of the last poll.** `last_activity` and the MR and pipeline times come
  from the daemon's lane view; the forge's are only as fresh as its poll (the rail header
  shows when it last ran). Against an older daemon without these fields, idle falls back
  to the last run's end and review to the lane's age.
- **Anchors need the feed.** A jump needs the row to be in the feed window the page holds
  (the newest 1000 items) and in the loaded thread.

## Next up

The rail's Now / Next up toggle and the `#/next` page show the next-up list
([backlog-next.md](backlog-next.md)). The page reads the list again whenever a `plan` item
arrives on the feed (an item added, approved, deferred, dropped or done), after each
action, and every 20 seconds, which also picks up work-matrix rows: rows with status
`todo` or `blocked` from each repo's `docs/WORK.md`, shown read-only in a "From work
matrix" group with their lane and PR links and no actions. The rail's Next up view lists
stored approved items only. The `plan` kind has the glyph `≡` in the thread, history and
the legend.

## Review list

Every element considered for the rail, and where it stands.

| Item | What it is | Status |
|---|---|---|
| Backlog list | needs you, broken, idle, working and to review, with links and "in thread" jumps | shipped in !107 |
| a. Spend vs budget | today's spend against the daily budget in the rail header, warning from 80% | shipped in !107 |
| b. Quota-hold notice | which agent is held for quota and until when, so an idle lane is explained | shipped in !107 |
| c. Forge poll health | when the forge was last polled, or its error, so stale MR badges are known | shipped in !107 |
| d. Attention count in the tab title | the board's counts in `document.title` for a background tab | already existed (`web/src/App.tsx`) |
| e. Run progress bar | a bar on running rows; the progress line already shows as text | deferred for review |
| f. Per-project grouping | group the rail by project with a toggle; urgency order stays the default | deferred for review |
| True idle-since | idle is dated from the lane's `last_activity` and names its source | shipped, from the lane view of !108 |
| MR and pipeline timestamps | review and failed-pipeline rows use `pipeline_finished` and `mr_updated` | shipped, from the lane view of !108 |
| Per-lane spend | the lane view carries none; per-run cost could be summed in the client | deferred for review |
| All-pages live rail | the dock counts and last feed rows in a rail beside every page at 1800px and up (UI audit phase 5) | deferred for review: needs a shell layout change, and the person has not chosen rail or room |
| Board feed strip | what just happened on the board, which has no feed of its own | deferred for review |
| Density default | `compact` (today's look) is the default; whether `comfortable` should be at wide sizes | deferred for review |
