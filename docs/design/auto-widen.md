# Auto-widen

Shepherd can widen a lane's scope by itself when the request is small, near the lane's
work and safe, so that it stops waiting on the person for it. No model decides: the rule
is a pure function (`internal/autowiden`) of the request, the lane, the other open lanes
and the repo's settings. When any condition fails, nothing changes and the request goes
to the person as before.

## The rule

All of these must hold:

1. The repo opts in with `widen: auto` (default `human`).
2. The request names exact files of the lane's own repo: no globs, no directories, no
   absolute paths, nothing that leaves the repo.
3. The files are free: no other open lane's scope overlaps them. The check is the Fold's
   own (`scope.Overlap`), and `Fold.Rescope` repeats it under the repo lock.
4. The request is small: at most `widen_max_files` files (default 3), each near the
   lane's work:
   - in the same directory as a file the lane has changed or names literally in its
     scope, or
   - a test of such a file (`x_test.go`, `x.test.ts`, `x.spec.ts`, `test_x.py`,
     `x_spec.rb`, in the same directory or its `__tests__` directory).
5. None is protected. The defaults cover CI definitions, migrations, dependency
   manifests and lockfiles, deploy and secret files and the work tracker. A repo adds to
   them with `widen_protected`.
6. The lane has used fewer than `widen_cap` auto-widens (default 3).

Following imports is not part of the rule yet; the nearness test is the place to add it.

## Config

Per repo (or under `defaults`): `widen`, `widen_max_files`, `widen_cap`,
`widen_protected`. See [config.md](../config.md#auto-widen).

## Triggers

| Trigger | Where | What it widens for |
|---|---|---|
| A run ends with files outside the scope | `Runner` after the run is recorded, before routing and shipping | The run's `Outside` files |
| An agent asks a decision about the scope | `Runner.Ask` | Files named in backticks in the question, when it mentions the scope or widening |

In both cases the Runner asks the Fold to widen (`Fold.Rescope`, the same call the
person's `widen_scope` fix and the desk's widen use), records the change, and continues
the run's session telling the agent its scope now includes the files, so it can commit
and push. A decision that was widened for is answered with a note and closed, so it does
not wait.

## Record

Each auto-widen writes one feed item of kind `lane_opened`, like the person's widen:
`lane L scope auto-widened: +files (run N); checks passed: ...; scope is now ...`. The
count of such items for a lane is what the cap counts, so it survives a restart. When a
rule fails, the feed says nothing new; the diagnosis and decision the person sees today
are unchanged.

The merge request description lists a lane's auto-widens when Shepherd opens it; that
text is built in the ship path.

## Wiring

The Runner has a `Widener` hook (`Rescope`). The daemon sets it from the Fold, as it does
for `ArtifactSnapshot`. A Runner without the hook never auto-widens.

## Out of scope

Choosing files, widening on a model's judgment, globs, files in other repos (that is a
typed cross-repo work order), and overriding the protected list for one request.
