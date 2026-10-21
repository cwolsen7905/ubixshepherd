# Merge rules in shadow mode

A repo can have a merge rule: the conditions under which Shepherd may merge a lane's
merge request itself. A new merge rule starts in **shadow mode**. Shepherd works out what
the rule would have done for each open lane merge request and records it, but the person
still merges. After a record they trust, the person makes the rule live.

This page describes what is built. Shadow mode only reads and records: it never merges,
approves, retries a job or posts to the forge.

**Status.** The shadow pass, its fact gathering and the commands that read its record are
built and tested (`internal/mergerule`, `shepherd crook shadow`). The daemon does not call
the pass yet, so a running Shepherd records no shadow rows until it is connected to the
watcher. Going live (the forge merge call, the revert after a red base branch and the
switch that makes a rule live) is not built.

## The rule file

A merge rule is a rule file in the repo's own layer, `.shepherd/rules/<id>.md`, in Crook's
rule format (front matter, a one-paragraph statement, an optional "Why"). Front matter sets `kind: merge`, `mode: shadow` and the
conditions:

```markdown
---
kind: merge
mode: shadow
require:
  - linked_requirement      # the lane is tied to a next-up item that cites a requirement
  - approved_checks_pass    # that requirement's approved check tests exist and pass
  - no_test_edits           # no existing test or approved check file changed
  - gate_green              # Shepherd's gate passed at the merge request's head
  - pipeline_green
  - ai_review_not_blocking  # an unreviewed result is not a pass
  - up_to_date              # the base branch is already in the lane
max_changed_lines: 600
paths_not: [".shepherd/**", ".gitlab-ci.yml", "internal/store/sqlite/**", "internal/crook/floor*"]
then: proceed
---
```

The rule is read from the repo's base branch, never from a lane's worktree, so a lane
cannot change the rule it is judged by. A change to `.shepherd/` is always the person's
call and never meets a merge rule. A rule file that fails to load is not used and is
reported once; the repo stays person-merged.

## What the pass looks at

Every fact comes from proof, not from an agent's word:

| Fact | Where it comes from |
|---|---|
| Linked item and requirements | the lane's next-up item |
| Approved checks pass, check files edited | the recorded approved check files and the pipeline on the base branch; when they are not recorded, the condition is not met |
| An existing test edited | `git diff base...head`: a changed, deleted or renamed file that is a test (`*_test.go`, `*.test.*`, `*.spec.*`, `testdata/`). A test the lane adds does not count |
| Gate green | the lane's latest gate record, only if it is for the merge request's head |
| Pipeline green | the head pipeline; a running one makes the answer *hold* |
| AI review | `ok` only when the review job passed; blocking, unreviewed or absent is not met |
| Up to date | the base branch is an ancestor of the head; a moved base makes the answer *hold* |
| Size and paths | `git diff --numstat` and `--name-only` between the merge base and the head |

A fact that cannot be found is left out and the condition is not met: unknown is never a
pass.

## What it records

For each open lane merge request with a shadow merge rule, the pass records one verdict
in Crook's log (action `merge`, stage `shadow`): *proceed* if every condition holds,
*hold* if the only shortfalls clear on their own, *ask* otherwise, with the unmet
conditions in the reason and the facts used. It writes a row only when the head, the
verdict or the unmet conditions change, so a merge request that sits does not fill the
log. The first time a head would have merged, the lane's feed says so.

When the person merges a request the rule had an opinion on, one more row (stage
`shadow-outcome`) records what happened, so the record can be read as "the rule said
proceed, and the person merged it".

## Reading it

```sh
shepherd crook shadow                  # would-have verdicts, with the unmet conditions
shepherd crook shadow --rule spec-verified --since 30d
shepherd crook rules                   # shadow rows counted in a table of their own
```

`crook shadow` first counts, per rule, how many requests it would have merged, asked
about or held and how many the person merged, then lists each would-have verdict with
its reason. `--json` gives the same data. The `crook_shadow` MCP tool reads it too.

Live counts in `crook rules` never include shadow rows, and `crook log` and `crook why`
show them like any row, marked by the stage.
