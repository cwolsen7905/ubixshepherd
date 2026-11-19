# Dispatch

How Shepherd starts an agent in a lane.

## The already-done check

Planned work goes stale: by the time a next-up item is picked up, its change may be on the
base branch already. Before the first run in a lane linked to an open next-up item, Shepherd
looks at `origin/<base>` as the repo last fetched it. It uses git only, with no fetch and no
forge call, so it is cheap enough for every start.

The item looks done when a commit on `origin/<base>`, made after the lane was opened,
either names the lane's branch in quotes (a merge commit does) or names the item's merge request
(`!<number>`, from the item's MR field). Matching is on the commit message. When the MR field is
a URL, the commit must cite that project (`group/project!<number>`, as the forge's merge commit
does), so another project's merge request with the same number does not count. A bare number
cannot say which project, so any `!<number>` matches.

A lane whose run would be checked is skipped when:

- the lane has no linked open item,
- the lane already has commits beyond its base (the work is in progress),
- the run continues an earlier one (an answer, a fix or a follow-up),
- the start was queued for a restart and is now replayed (the check ran when it was first requested, before it was queued), or
- the request sets `force`.

When the item looks done, no run starts. The start is refused with the reason and a hint to
use `force`, and a plan line goes on the feed: `run not started in lane <name>: next-up item
<id> looks already done: ...`. Both `lane_run` and a keep-going drive go through the same
start, so both are checked. A drive never forces: it reports the refusal like any other
refused start.

To start anyway, send `"force": true` in the run request to the daemon's HTTP API. This is a guard against wasted runs, not a security boundary: any caller allowed to start a run, the front desk included, can set it, and the desk acts for the person. The `shepherd lane run` command and the `lane_run` tool do not take it yet.

This check is separate from the instruction in the run's brief, which asks the agent to check
the base itself and report a summary that starts with `ALREADY DONE:`. The brief covers what
git history cannot show, such as work that landed under another branch and wording.

## Queued follow-ups

A lane runs one agent at a time. A start the person asks for while the lane's agent is
still going (`run continue` on the running run, or `lane run` in a busy lane) is queued,
not refused. The answer says `queued behind run N`, with HTTP status 202, and the run
starts as soon as run N ends.

- **Order.** The queue is first in, first out per lane. A start asked for while earlier
  follow-ups wait goes behind them, even if the lane is idle for a moment. If the oldest follow-up in a lane is held (its
  agent is out of quota, say), later ones in that lane wait behind it. Starts Shepherd makes
  on its own wait for queued follow-ups too.
- **Session.** A queued follow-up continues the lane's latest session with the same agent,
  which is the session the running run was using. A different agent asked for starts its
  own session.
- **Restarts.** A queued follow-up is a row in the store's held starts, so it survives a
  daemon restart and starts once the daemon is back and a run ends or the held starts are
  replayed.
- **Closing the lane.** A follow-up queued in a lane that has closed is dropped, and
  noted in the feed, the next time the queue is tried (when any run ends). It never runs
  in a closed lane. Dropping the queue at the moment of closing is not wired yet.
- **Not queued.** Starts Shepherd makes on its own (fixes, routed requests, nudges) are
  still held and retried by whatever began them, never queued. A continue of a run that
  has ended is unchanged.
