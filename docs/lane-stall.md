# Lane stall alert

An open lane can be left behind: commits sit unpushed, or its merge request's pipeline
failed, and no agent has run in it for hours. The lane stall alert tells you once.

## What counts as a stall

An open lane is stalled when all of these hold:

- No run is in progress in it.
- Its last run ended (or, with no run yet, the lane was opened) at least the threshold ago.
  The default is 4 hours.
- It has commits not on its branch at origin, or its merge request's head pipeline failed.

## What you get

One feed item of kind `lane_stall`, naming the lane, its repo, how long it has been quiet
and what holds it. It is raised once per stall, not on every check.

The stall ends, and the lane may alert again later, when:

- a run starts in the lane (the quiet time then counts from that run's end),
- the condition goes away: the commits are pushed, or the pipeline is no longer failed, or
- the lane closes.

## Behaviour notes

- The check reads the store and runs `git rev-list` in each open lane's worktree; it never
  calls the forge. The pipeline state is what the watcher last recorded.
- A lane that is not stalled when the daemon restarts is checked afresh; a stall already
  announced before a restart is announced once more.
- The alert text goes through the redactor like other agent-facing text.
- It is separate from the keep-going stall alert, which is about approved Next-up items
  that nothing can start.

## For contributors

The check is `LaneStall` in `internal/daemon/lanestall.go`. `Server.CheckLaneStalls` runs
one pass with default options; `Server.SetLaneStallOptions` replaces the options (threshold,
clock, unpushed counter).
