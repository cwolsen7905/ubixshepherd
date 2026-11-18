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
- the run continues an earlier one (an answer, a fix or a follow-up), or
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
