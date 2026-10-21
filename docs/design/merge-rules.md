# Merge rules and shadow mode: implementation plan

Status: proposed, for the maintainer's approval. No code changes with this document.
Work item: next-up 126.

## Goal

A repo can carry a merge rule: the conditions under which Shepherd may merge a lane's
merge request on its own. The first rule is `spec-verified`, for this repo. It starts in
**shadow mode**: Shepherd works out, for every open lane merge request, what the rule
would have done, records that with the facts it used, and acts on the old answer (the
person merges). After a clean record the person makes the rule live. Live merging is a
separate, later step and is not part of this item.

## Already on dev

Checked on 2026-10-20 against `dev` at `31a6762`. Most of the evaluator exists; the
wiring does not.

| Piece | Where | State |
|---|---|---|
| Rule file format, strict loader | `internal/crook/rulefile.go` | Done: `kind: merge`, `require`, `max_changed_lines`, `paths_not`, `mode`, `on_red_after_merge`. A merge rule not written `mode` defaults to shadow, and a live one warns |
| Merge evaluation | `internal/crook/evaluate.go` (`mergeVerdict`) | Done: all-met gives the rule's verdict, a shortfall that clears by itself (running pipeline) gives hold, any other gives ask. A missing fact fails closed |
| Shadow evaluation | `evaluate.go` (`shadowRules`, `Outcome.Shadow`) | Done: a shadow rule never decides; its would-be verdict is listed beside the real one |
| Rules-folder floor | `Evaluate` | Done: a change under `.shepherd/` is at least ask and never meets a merge rule |
| Layers | `internal/crook/layers.go` | Done: read and merge layer directories |
| Verdict log | `crook_verdicts` table, `crook.Record`, `shepherd crook why/log/rules` | Done for live verdicts |
| Forge reads | `internal/forge` | MR head, pipeline, threads, approvals, AI review job and its UNREVIEWED result. **No merge operation** |
| Watcher | `internal/watch` | Polls open lanes, reads MR and pipeline, retries an unreviewed AI review once |

Nothing outside `internal/crook` loads a rule file or calls `Evaluate`, and no code
gathers the facts a merge rule reads. That wiring is this item.

## What is missing

1. A rule file for this repo, read from the **base branch**, never from a lane's worktree.
2. A facts gatherer for a lane's open MR.
3. A shadow pass that runs it and records the result.
4. A way to read the record: what the rule would have merged, held and asked.
5. Two facts that depend on the checks-first work (`approved_checks`, `check_files_edited`).

## Design

### 1. The rule file

`.shepherd/rules/spec-verified.md` in this repo, as the format defines it:

```markdown
---
kind: merge
mode: shadow
require:
  - linked_requirement
  - approved_checks_pass
  - no_test_edits
  - gate_green
  - pipeline_green
  - ai_review_not_blocking
  - up_to_date
max_changed_lines: 600
paths_not: [".shepherd/**", ".gitlab-ci.yml", "**/migrations/**", "internal/crook/floor*"]
on_red_after_merge: revert_and_ask
then: proceed
---
```

Because it lives under `.shepherd/`, it is protected by design: a lane cannot add or
loosen it, and an MR that changes it is always the person's. This file is **not**
written by this item's lane (the path is outside the scope and a rules-folder change is
ask). The plan proposes the text above; the person lands it in a merge request of their
own, or approves a lane scoped to that one path.

The "migrations" glob must match the store's real migration location (they are appended
SQL in `internal/store/sqlite/sqlite.go`, not a folder). The implementation lane checks
this and the rule lists the real protected paths: `internal/store/sqlite/**`,
`internal/crook/floor*`, `.shepherd/**`, `.gitlab-ci.yml`, `.github/**`, and
`internal/**/auth*`. This is a point for the person's amendment (see "Open choices").

### 2. Reading the rule from the base branch

A new package `internal/mergerule` owns the wiring so the busy packages change by a call
or two.

- `Load(ctx, repo)` reads `.shepherd/rules/` from the repo's integration branch with
  `git show <base>:path` through `internal/git` (the same CLI the rest of Fold uses), then
  `crook.ReadLayerFS` and `crook.Merge`. Reading from the base ref means a lane cannot
  change the rule it is judged by, even in its own worktree.
- The result is cached by the base branch's commit id, so a poll costs one `rev-parse`.
- A load error is logged once per commit id and means no rule: the repo stays
  person-merged. Fail closed.

### 3. Facts for one lane

`mergerule.Facts(ctx, lane, mr)` builds the map `Evaluate` reads. Each comes from proof
(git, the forge, Shepherd's own gate record), never from an agent's word.

| Fact | Source |
|---|---|
| `item_linked`, `requirement` | the lane's next-up item (`lane open --item`) and the requirement ids it cites |
| `approved_checks` | `pass` only when every check file the item cites is marked approved, is on the base branch, and a pipeline on the base branch containing it passed; else `fail` or `unknown`. **Depends on checks-first** |
| `tests_edited` | `git diff --name-status <merge-base>..<head>`: any modified or deleted file matching a test-file pattern (`*_test.go`, `web/src/**/*.test.*`) that existed on the base. Added tests do not count |
| `check_files_edited` | any changed path in the marked check-file set. **Depends on checks-first** |
| `gate` | the latest `gate_results` row for the lane, `green` only if its sha equals the MR head |
| `pipeline` | the MR head pipeline: `green`, `running`, `red`, none |
| `ai_review` | `ok` only when the AI review job succeeded on the head pipeline; `blocking` for findings; `unreviewed` otherwise. Anything but `ok` fails the condition, so an unreviewed result holds |
| `up_to_date` | the base branch is an ancestor of the head (`merge-base --is-ancestor`) |
| `open_threads` | count from `forge.Threads` |
| `changed_lines` | `git diff --numstat` summed, additions plus deletions |
| `paths` | `git diff --name-only --no-renames` |

`unknown` is never `pass`. Until checks-first lands, `approved_checks` and
`check_files_edited` are absent, so the rule says **ask** with "approved_checks_pass not
met": a true statement that no spec-verified merge could have happened yet. The shadow
record is useful from the day this lands, because every other condition is already
reported.

### 4. The shadow pass

The watcher, after it has read an open lane's MR for a poll, calls
`mergerule.Observe(ctx, lane, mr)` (one call, in `watch.go`, with a nil-safe field on the
Watcher so tests that do not set it behave as today).

`Observe`:

1. Loads the rule set for the lane's repo; returns if there is no merge rule.
2. Builds the facts and runs `crook.Evaluate` with `Action: "merge"`, `Actor: daemon`.
3. If the outcome's `Shadow` list names the rule, records one verdict row: action `merge`,
   stage `shadow`, kind the would-be verdict, rule id and version, the reason (which
   conditions were short), repo, lane and facts. Uses the existing `crook_verdicts` table,
   so **no migration**: stage `shadow` is what separates a would-have row from an act.
4. Writes a row only when it changes: the key is the MR head sha, the verdict and the
   set of unmet conditions. A poll that finds the same thing writes nothing, so a
   long-running MR does not grow the log.
5. On the first `proceed` for a head, adds a feed line: "spec-verified would have merged
   !N (shadow)". No other verdict gets a feed line; they are in the log.
6. Never calls a merge, an approval or a retry. The only writes are the verdict row and
   the feed line. A forge or git error is logged at debug and the pass is skipped.

The real merge path is untouched: `autonomy.merge` keeps its meaning, and the person
merges. In shadow mode Crook's `merge` answer for the real act stays as it is today.

When the MR later merges, `Observe` (or the closing path) records one more shadow row,
`outcome: person-merged at <sha>`, so a review of the record can compare "would have
merged" against what actually happened, including whether `dev` went red afterwards.

### 5. Reading the record

- `shepherd crook rules` already lists per-rule counts. It is changed to count shadow
  rows apart: "would have merged N, held N, asked N" for a rule in shadow, and the live
  counts unchanged.
- `shepherd crook shadow [--rule id] [--since 14d]` lists lanes and MRs with the
  would-be verdict, the unmet conditions and what the person did. A MR the rule would
  have merged that the person then reworked or reverted is marked.
- `shepherd crook why <id>` prints a shadow row with "shadow: not acted on".
- The MCP tool for this is a mapping onto the new command, added after the command.
- The web Rules view (a separate lane) reads the same rows. Nothing here builds it.

### 6. Making it live is out of scope

Going live needs a merge operation in the forge client, the post-merge revert, and a
person-only switch. All three are a later item. This item stops at an evidence trail the
person can read. The plan notes what live would add so the design does not close it off:
`Facts` and `Evaluate` are the same, only the caller acts on `proceed`; the rule file's
`mode: live` is the switch; the head sha is passed to the forge merge so it refuses a
head that moved.

## Files I would touch

| Glob | Change |
|---|---|
| `internal/mergerule/**` | New: `Load`, `Facts`, `Observe`, with tests |
| `internal/crook/log.go`, `internal/crook/crook.go` | Small: a helper to record a shadow row; stage name constant |
| `internal/cli/crook*.go` | `crook rules` counts shadow apart; new `crook shadow` |
| `internal/cli/mcp.go` | One tool mapping for `crook shadow` |
| `internal/watch/watch.go` | One call to `Observe` and one field |
| `internal/daemon/**` | Construct the observer and give it to the watcher |
| `docs/crook-why.md`, `docs/cli.md`, new `docs/merge-rules.md` | User docs for what shipped |
| `.shepherd/rules/spec-verified.md` | The rule file (the person's act, see above) |
| `docs/design/merge-rules.md` | This plan; removed or reduced to a pointer when the docs page lands |

## Overlap with open lanes

| Lane | Holds | Where I meet it | How I avoid it |
|---|---|---|---|
| `feat/follow-through-owner` | `internal/watch/**`, `internal/config/**`, `internal/dispatch/**` | One call and one field in `watch.go`; possibly a config key if a rule needs an on or off switch | Put all logic in `internal/mergerule`. Make the `watch.go` edit last, after that lane merges, or ask that lane to add the hook. No config change: the rule file carries `mode`, so no new profile key is needed |
| `feat/checks-first-core` | `internal/store/**`, `internal/fold/**`, `internal/crook/**`, `internal/scope/**` | `approved_checks` and `check_files_edited` read its `check_files` rows; the shadow helper touches `internal/crook/log.go`; no store change from me | Do not edit `internal/store/**` or `internal/fold/**`. Read check files only through the `store.Store` methods that lane adds. Keep the `internal/crook` change to one helper, written after that lane merges, or let that lane own it. Until it lands, the two facts stay absent |

Order that avoids conflicts: build `internal/mergerule` and the CLI first (no overlap),
then the `watch.go` hook and `internal/crook` helper after the two open lanes merge.
`internal/daemon/**` and `internal/cli/**` are held by neither.

## Tests

- `internal/mergerule`: a table of facts to verdicts for each of the eight conditions,
  each failing alone; every missing fact; a size at and over the limit; each protected
  path; an UNREVIEWED review (hold or ask, never proceed); a gate result for an older sha;
  a base that moved; a modified existing test versus an added one.
- `Load` against a real repo with a bare origin (as Fold's tests do): the rule is read
  from the base branch; a lane that edits `.shepherd/rules/` in its worktree does not
  change the loaded rule; a malformed file means no rule and one logged error.
- `Observe` with a fake forge and the fake store: writes one row per change of state and
  none on a repeat poll; writes no merge call (the fake forge has none to call and the test
  asserts it was not asked for approvals or retries); a repo without a rule writes nothing;
  the feed line appears once.
- `crook` shadow rows are excluded from live counts and `crook why` marks them.
- Conformance: the existing `internal/crook` conformance cases still pass unchanged.
- A manual check on this repo: with the rule in place, open a docs-only lane and read the
  record with `shepherd crook shadow`.

## Open choices for the maintainer

1. **Landing the rule file.** Recommended: the person lands `.shepherd/rules/spec-verified.md`
   in a small merge request of their own, since it is a rules-folder change and so is ask
   by design. Alternative: widen this lane's scope to that one path for a single commit,
   which still ends in the person's approval.
2. **Protected paths.** The sketch's "migrations" glob names a folder that does not exist
   here. Recommended: list `internal/store/sqlite/**`, `internal/crook/floor*`,
   `.shepherd/**`, `.gitlab-ci.yml`, `.github/**`.
3. **Size limit.** The sketch has 600 changed lines. Recommended: keep 600 for shadow and
   tune it from the record.
4. **Order against the open lanes.** Recommended: start `internal/mergerule` and the CLI
   now; add the watcher hook and crook helper once the two lanes merge. Alternative:
   wait for both.
5. **Evidence before live.** Recommended: no live switch until the record covers at least
   ten lane merge requests and no would-have-merged one was later reworked or reverted.

## Scope I need

`docs/design/merge-rules.md`, `docs/design/merge-rules/**` (this plan), then for the build:

```
internal/mergerule/**
internal/cli/crook*.go
internal/cli/mcp.go
internal/daemon/**
docs/merge-rules.md
docs/crook-why.md
docs/cli.md
```

and, after the open lanes merge, `internal/watch/watch.go` and `internal/crook/log.go`.
