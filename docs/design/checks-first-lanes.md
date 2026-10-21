# Checks-first lanes: implementation plan

Status: proposed, for the maintainer's approval. No code changes with this document.
Work item: next-up 127.

## Goal

A requirement's checks are written as test files in a lane of their own. The person
approves them as "this is what done means", they merge, and from then on no build lane
can change them. An agent that believes a check is wrong raises a decision instead of
editing the test it is measured by.

Two things must be true:

1. An approved check file is **marked**, and the mark survives restarts and is the
   person's alone to set or clear.
2. A lane's scope **excludes** marked files, in `lane_open`, in scope changes and in the
   pre-push hook, so a push that touches one is refused.

## Precedent

`internal/daemon/keepgoing_checks_test.go` is a set of checks behind the
`keepgoing_checks` build tag. They fail until the build lane lands, `make check` stays
green because the tag keeps them out, `make check-keep-going` runs them, and the file's
header says the build lane must not edit them. That convention is today a comment and
good manners. This plan turns it into something the scope guard enforces, and keeps the
build-tag convention for "expected to fail until the build lands".

## Current behaviour this builds on

| Piece | Where | What it does today |
|---|---|---|
| Scope globs | `internal/scope` (`Match`, `Any`, `Overlap`) | Repo-relative globs; a literal names a file or a directory |
| Lane open | `fold.Open` (`internal/fold/fold.go`) | Validates the scope (`checkScope`), refuses an overlap with another open lane (`scope.Overlap`), records the lane |
| Scope change | `fold.Rescope`, the desk's `lane_widen` with a decision | Adds or removes globs, same overlap check |
| Pre-push | `fold.CheckPushTo` (`internal/fold/hook.go`), reached through the daemon's hook endpoint | Lists `git diff --name-only --no-renames` from the push base and refuses files outside `lane.Scope` |
| Next-up link | `lane open --item`, `store.NextItem` | Ties a lane to an item; the item has `Plan` and `DoneWhen` |
| Person-only acts | `crook` actions such as `decision.answer`, `lane.widen` | Decide who may do what, and log verdicts |

## Design

### 1. Where the mark is recorded

**In the store, as rows the daemon owns**, keyed by repo and path, each pinned to the
file's content.

| Option | Verdict |
|---|---|
| The store (recommended) | Out of an agent's reach: an agent edits files in its worktree, never the daemon's database. Survives branch switches. Gives the web UI and the feed something to list. Records who approved and when |
| Repo config (`.shepherd/` or the profile) | Config is a file in the repo, so a lane that edits it could unmark the check. It would also need its own protection. Rejected |
| A header in the file itself | Editable by exactly the party we constrain. Useful only as a human hint, so the plan also asks for a one-line comment convention, but nothing trusts it |

New table, appended as the next migration (never edit an earlier one):

```sql
CREATE TABLE check_files (
    id          INTEGER PRIMARY KEY,
    repo_id     INTEGER NOT NULL REFERENCES repos(id),
    path        TEXT NOT NULL,          -- repo-relative, "/" separated
    blob        TEXT NOT NULL,          -- git blob id approved
    base_commit TEXT NOT NULL,          -- commit on the base branch it was approved at
    item_id     INTEGER NOT NULL DEFAULT 0,  -- the next-up item, 0 if none
    set_name    TEXT NOT NULL DEFAULT '',    -- optional label for a group, e.g. the requirement area
    approved    TEXT NOT NULL,          -- time
    approved_by TEXT NOT NULL,          -- "person" and the surface (cli, web, desk)
    released    TEXT NOT NULL DEFAULT '',    -- time, empty while marked
    release_why TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX check_files_live ON check_files(repo_id, path) WHERE released = '';
```

Released rows stay as history. A new approval of the same path after a release is a new
row. `store.Store` gains `CheckFiles(ctx, repoID, liveOnly)`, `ApproveCheckFiles`,
`ReleaseCheckFiles`. The interface change touches `store/sqlite` and any fake store in
tests.

### 2. What approving means

Approving is allowed only for files that are **already on the repo's base branch**
(`origin/<base>`), which makes "merged before the build lane starts" a precondition the
daemon checks rather than a hope. `approve` reads the blob id of each path at
`origin/<base>`, refuses a path that is missing, is not a regular file, or sits in a
currently open lane's scope as a writable path (a revision in flight), and records the
blob and commit.

Approval is the person's act. A new Crook action `checks.approve` (and `checks.release`)
is allowed only for the person actor, like `decision.answer`: an agent's token, a worker
tool and the desk acting on its own are refused with the rule named. The desk may carry
out an approval only with the id of a decision the person answered, the way `lane_widen`
takes `--decision`.

### 3. How exclusion works

One function in `internal/fold`, used by every path that reads a lane's scope:

```go
// Protected returns the repo's live check files.
func (f *Fold) Protected(ctx, repoID) ([]store.CheckFile, error)
// InScope: scope.Any(lane.Scope, file) && !protected[file]
```

`internal/scope` stays pure (globs only); the protected set is passed in, so the package
gets one new helper, `scope.Without(globs, files, protected)`, that returns the
matching files minus the protected ones, and `Any` is untouched.

**Pre-push (`CheckPushTo`).** After the existing out-of-scope test, any changed file in
the diff that is protected is its own problem line, so the person and the agent see the
real reason:

```
changes approved check files (decided by the person; raise a decision to change them):
    internal/daemon/keepgoing_checks_test.go
```

Deleting or renaming a protected file is a change: the diff already uses `--no-renames`,
so a rename shows as a delete plus an add. The check runs for every lane, whatever its
scope says, so a broad scope such as `internal/**` cannot reach a marked file.

**Ship.** `lane ship` runs the same check against the lane's diff before it pushes,
because the hook can be skipped with `--no-verify`. As a second line it also compares the
blob id of each protected path at the lane's head with the approved blob and refuses a
difference, which also catches a rebase or merge that carried an edit in.

**Lane open and scope changes.** A scope glob that overlaps a marked file is not an
error: `internal/daemon/**` is a reasonable scope that happens to contain a check file.
Instead:

- `Open` and `Rescope` compute the protected files the new scope would reach and report
  them in `Opened.Notes` (and the CLI, MCP and web results) as "excluded: N approved
  check files (list)". The lane is recorded with the scope as given; exclusion is applied
  when it is judged, so a later release or a new approval takes effect without editing
  every lane.
- A scope whose **every** match is a protected file, or a literal naming a protected file,
  is refused: the lane could do nothing, and the most likely cause is a mistake. The
  message says the files are approved checks and how to ask for a change.
- The overlap test between lanes (`scope.Overlap`) ignores protected files, so a checks
  lane and a build lane can hold neighbouring globs, and two build lanes are not
  refused because both reach a file neither may change.
- `lane_widen` and `lane scope --add` apply the same two rules.

**The lane that revises checks.** Changing a check is the person's decision, made in
two steps: `checks release` (below) unmarks the files, then a checks lane edits them, they
merge, and the person approves again. While released, any lane could in principle edit
them, so release is allowed only when no build lane for the same item is open, or with
`--force` that the person types, and the feed records it.

### 4. Releasing

`shepherd checks release <path...> --why TEXT` marks the rows released with the reason
and the person's surface, and writes a feed event. The agent's route is a decision linked
to the item (the existing worker `ask_human`), whose text names the check and why it is
wrong. Nothing about the answer releases a file by itself: after the person answers, the
person (or the desk, on the decision's id) runs the release.

### 5. Surface

**CLI** (`internal/cli/checks.go`, a client of the daemon like every command):

| Command | Does |
|---|---|
| `shepherd checks list [--repo R] [--all] [--json]` | Live marks; `--all` includes released |
| `shepherd checks approve <path...> [--item N] [--set NAME] [--repo R]` | Marks files merged on the base branch |
| `shepherd checks release <path...> --why TEXT [--repo R]` | Unmarks, with a reason |
| `shepherd checks status [--repo R]` | For each mark, whether the base branch still has the approved blob |

`lane open` and `lane scope` print the excluded files.

**HTTP** (`internal/api`, handlers in `internal/daemon/checks.go`): `GET /v1/checks`,
`POST /v1/checks/approve`, `POST /v1/checks/release`. The `Lane` view gains
`excluded []string` so clients need no second call.

**MCP** (`internal/cli/mcp.go`): `checks_list` for every client, since an agent should
know which files it may not touch. `checks_approve` and `checks_release` only on the
desk's tool set and each requires a `decision` id the person answered, matching
`lane_widen`. The worker server (`--worker`) gets `checks_list` only. A tool is added by
adding the CLI command first, then the mapping.

**Web** (`web/src`): the lane page lists "Approved checks excluded from this lane"; the
next-up row and project page show the item's check files with their state; approving and
releasing happen in the side panel with the approve action in the lantern style and a
release as a destructive confirm that asks for the reason. The waiting list gains no new
kind: an approval is a button on the checks lane's merge request, not a queue of its own.
Both themes are checked, per the web rules.

**Waiting and feed.** Events `checks_approved` and `checks_released` join the closed set
of feed kinds. A merged lane that touched files later marked needs nothing.

### 6. What this does not do

- It does not decide which lane is a "checks lane" or whether an item's checks exist.
  Readiness (every wanted requirement has a check) is a later feature; this plan only
  protects what the person approved.
- It does not run the checks. How to run a set stays the repo's convention (a build tag
  and a make target, as keep-going does). `set_name` and `item_id` leave room for a merge
  rule to find them later.
- It does not protect against a person with push rights pushing by hand to the forge;
  the forge's own protected paths are the control there.

## Files and packages the build lane would touch

| Area | Paths |
|---|---|
| Scope helper | `internal/scope/scope.go`, `internal/scope/scope_test.go` |
| Store | `internal/store/check.go` (new), `internal/store/store.go` (interface), `internal/store/sqlite/check.go` (new), `internal/store/sqlite/sqlite.go` (next migration), `internal/store/sqlite/check_test.go` (new), `internal/store/sqlite/upgrade_test.go` |
| Fold | `internal/fold/checks.go` (new), `internal/fold/fold.go` (`Open`, `Rescope`), `internal/fold/hook.go` (`CheckPushTo`), `internal/fold/checks_test.go` (new), `internal/fold/fold_test.go` |
| Ship | the `lane ship` path in `internal/fold` and its test |
| Crook | `internal/crook/crook.go` (two actions), `internal/crook/` rule defaults and tests |
| API and daemon | `internal/api/checks.go` (new), `internal/api/api.go` (lane view), `internal/daemon/checks.go` (new), `internal/daemon/daemon.go` (routes), `internal/client/` |
| CLI and MCP | `internal/cli/checks.go` (new), `internal/cli/commands.go`, `internal/cli/lane.go`, `internal/cli/mcp.go`, tests beside them |
| Feed | the feed kind map in `internal/daemon/feedhub.go` or wherever the closed set lives |
| Web | `web/src/pages/Lane.tsx`, `web/src/pages/Next.tsx`, `web/src/pages/Project.tsx`, `web/src/api/`, `web/src/components/` or `web/src/ui/` for a checks panel, `web/src/styles/components.css`, their tests |
| Docs | `docs/checks-first-lanes.md` (new, user page), `docs/cli.md`, `docs/config.md` if a setting is added, README's "Read next" table |

Suggested lane scope: the rows above, as globs. `internal/cli/mcp.go`, `internal/fold/fold.go`
and `internal/store/sqlite/sqlite.go` are shared with other work; check for overlapping
lanes at open. Splitting into two lanes is sensible: store, fold, crook and API first;
CLI, MCP, web and docs after.

## Tests

Following the repo's rules: fold tests build real repos with a bare origin, the daemon
and CLI tests use fakes, no command opens the store.

| Area | Cases |
|---|---|
| `scope` | `Without` drops exactly the protected files; globs, literals and directories; `Overlap` ignores protected files |
| Store | Approve, list, release, re-approve; the unique live index refuses a duplicate; upgrade from the previous schema keeps data; round trip through a fresh database |
| Fold: approve | Refuses a path not on `origin/<base>`, a directory, a missing file; records the blob; refuses a path in an open lane's writable scope |
| Fold: open | Broad scope opens with the excluded files in the notes; a scope that matches only protected files is refused; a literal naming one is refused; two lanes with globs reaching the same protected file both open |
| Fold: rescope and widen | Same rules; widening onto a protected file is refused even with a decision |
| Fold: push | Editing, deleting and renaming a protected file are each refused with the check-file message and the file named; a legitimate file in the same push is still reported separately; a broad scope does not help; a lane with no marks behaves as before; after a release the same push passes |
| Fold: ship | Refuses a protected-file change; refuses a head whose blob differs from the approved blob |
| Crook | Agent actor and worker token refused for approve and release; person allowed; the desk allowed only with an answered decision |
| API and daemon | Endpoints validate input; the lane view carries `excluded`; events appear in the feed |
| CLI and MCP | Each command's output and exit code; the MCP mapping is one-to-one; the worker set lists only `checks_list` |
| Web | The lane page shows the excluded list; approve and release flows stage and confirm; offline behaviour; style lint passes |
| Manual | One real lane with a real agent tries to edit an approved check and is refused at push, then raises a decision; recorded in the lane's report |

The build lane's own checks for this feature follow the keep-going precedent: if the
person wants them written first, they are a separate checks lane, approved with this
very mechanism, which is the best proof of it.

## Open questions

1. **Which lanes does the exclusion apply to?** Option A: every lane in the repo.
   Option B: only lanes linked (`--item`) to the item the checks belong to.
   *Recommendation: A.* An unlinked lane is the more likely route to weakening a test, and
   no lane needs to edit an approved check without the person releasing it first.
2. **Who may approve from the desk?** Option A: person only (CLI and web), the desk can
   only list. Option B: the desk too, on an answered decision's id.
   *Recommendation: B*, same as `lane_widen`, so approving works from chat, with the
   person's words recorded.
3. **Is the pre-push hook enough?** Option A: hook only. Option B: hook plus the ship-time
   check and blob comparison.
   *Recommendation: B.* The hook can be skipped with `--no-verify`; Shepherd's own ship
   path should not rely on it.
4. **Should approval pin the blob, or just the path?** Pinning lets `checks status` and
   ship notice a change that arrived by rebase or merge, at the cost of a release plus
   re-approve for every legitimate edit. *Recommendation: pin.* Revising an approved check
   should be deliberate.
5. **Release while a build lane is open for the item.** Option A: refuse unless
   `--force`. Option B: always allow. *Recommendation: A.*
6. **How are checks that are expected to fail kept out of the gate?** Option A: the repo's
   convention only (build tag plus a make target), as keep-going does. Option B: Shepherd
   learns a per-set command and treats a named-failing test as accepted. *Recommendation:
   A for now*; B belongs with the merge rule that needs to run the set.
7. **Is a one-line header in approved check files wanted** (a comment naming the set and
   "do not edit; ask via a decision")? It helps a human reader and an agent that reads
   the file, and nothing trusts it. *Recommendation: yes*, as a documented convention in the
   user page, not enforced.
8. **Branch protection on the forge.** Should Shepherd also offer to configure protected
   paths on the forge for the same files? *Recommendation: not in this lane*; mention in
   the user page.
9. **Lane split.** One lane or two (store, fold, crook and API; then CLI, MCP, web and
   docs)? *Recommendation: two*, because the second depends on the first and the shared
   files are many.
