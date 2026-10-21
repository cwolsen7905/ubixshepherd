# Muster: the upkeep pass

Status: **partly built**. The pass, its checks, the log and undo are in the daemon's
watcher. The routes, CLI and web screens that show and act on its findings are not built
yet, and nor are the required links on new questions and items.

Lists that lie stop being used. Muster is a pass that runs on the watcher's tick and
looks over artifacts, lanes, runs, next-up items, questions and decisions for what has
gone stale. It is plain code: it reads the store and the disk and never calls a model.

It does one of two things with what it finds:

- **Tidies on its own** what is safe and can be undone, writes a feed line, and logs it.
- **Raises a finding** for the rest. A finding waits in a "To tidy" group with a reason
  and, where there is one, a one-click fix. The person can apply it, or dismiss it.

Every finding and every applied fix is one row in the muster log with its reason, and an
applied fix can be undone for a number of days. A finding is made once: whatever became of
it (applied, undone, dismissed), the same finding is not made again. A finding that stops
being true on its own is marked cleared.

## What it checks

| Area | Found | What happens |
|---|---|---|
| Artifacts | A live artifact whose file is gone from the worktree | Marked gone on its own; it leaves the list and stays in history |
| | A live artifact on a closed lane | Takes the state its intent calls for: committed if the branch holds it, kept if a copy is stored, gone otherwise (unpinned scratch is gone) |
| | Artifacts undecided for `muster.batch_days` | One batched finding; the fix keeps them all |
| | Identical content in one lane | Shown once (`muster.Collapse`, for the list to use) |
| Board | A lane whose worktree or branch is gone | Flagged |
| | A run marked running whose process is dead | Marked interrupted, after two passes in a row see it dead; cannot be undone |
| | A lane with no activity for `muster.stale_days` | Flagged stale |
| | A lane whose merge request was closed without merging | Flagged, to reopen or close the lane |
| | An approved scope change the lane never got | Finding; the fix adds the paths to the lane's scope |
| Next up | An item with no repo | Finding that asks for one |
| | An item whose lane closed without a merge | Finding; the fix puts it back to approved and clears the lane link |
| | An item whose cited requirements are now implemented | Proposed as done, when the daemon is given a way to know |
| | Items with near-identical titles | Proposes dropping the later one (shared words, no model) |
| Inbox | A question or decision whose lane or item is closed or done | Closed as out of date, with the reason; undo reopens it |
| | A question or decision unanswered for `muster.digest_days` | Goes into one digest finding |
| | A question unanswered for `muster.skip_days` more | Marked skipped (stale); undo reopens it. Decisions are never skipped: an agent is waiting on the answer |
| | A question with no repo and lane or item | Flagged: it cannot be told when it goes out of date |

## Settings

```yaml
muster:
  off: false         # turn the whole pass off
  stale_days: 3      # 0 turns the check off
  batch_days: 3
  digest_days: 3
  skip_days: 7
  undo_days: 7       # how long a tidied item can be undone
```

An unset value takes the default shown. Negative values are refused.

## Links

A question carries the repo and the lane or next-up item it is about (`repo`, `lane_id`,
`next_id`). A decision is about the lane of the run that asked it. A next-up item has a
repo. `muster.ValidateQuestion` and `muster.ValidateNext` are the rules, and
`muster.AboutQuestion`, `AboutDecision` and `AboutNext` give the label. Questions made
before the links existed are flagged rather than guessed at.

## Where it lives

`internal/muster` is the pass; `internal/watch` runs it each tick after the other checks;
`internal/cli/daemon.go` builds it. Findings are `store.MusterItem` rows
(`muster_items`), read by `waiting.Tidy`. `Pass.Gate` is where policy can stop a fix from
being applied on its own; the fix then waits as a finding.
