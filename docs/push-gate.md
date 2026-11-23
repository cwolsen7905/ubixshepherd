# Push gate

In a repo whose profile says `autonomy.push: agent`, an agent Shepherd started may push
its own lane's branch to the repo's origin. It may do so only on a commit the repo's gate
has passed on. The repo's gate is the `gate` command in its profile, for example
`make check`.

## How it works

1. The agent commits its work, so the worktree is clean.
2. It calls the `gate` worker tool, or runs `shepherd lane gate` in the lane's worktree. The daemon runs the repo's
   configured gate there, against the lane's head commit, and records the result for that
   exact commit. The tool takes no arguments and acts only on the run's own lane, so it works
   for an agent whose shell may not run `shepherd`. The agent never supplies the command or the result.
3. It pushes. The pre-push hook looks for a passing record for each pushed tip. With one,
   the push goes ahead. Without one, the hook refuses and says to run
   `shepherd lane gate`.

A new commit needs a new run: a pass for one commit says nothing about the next. A record
also stops counting if the repo's gate command changes, and a failing run is recorded as a
failure, so it never lets a push through. `shepherd lane gate` refuses to run with
uncommitted changes, because the gate would then be checking something other than the
commit that is pushed.

## What a pass does and does not show

The gate runs the lane's own checked-out code, so the agent controls what the command
does: a commit that edits the gate's script or its dependencies can make it pass. A
recorded pass shows that the configured command passed on that commit. It does not show
that the commit left the gate's definition alone. Check files the person has approved are
judged separately: a push that changes one is refused, whatever the gate says. Review the
commit as you would any other. Only the operator and the agent's own run may call the
gate route; the front desk cannot.

## What it leaves alone

- Your own pushes. The check applies while an agent runs in the lane.
- Repos with no `gate` in their profile: there is nothing to check.
- Repos set to `push: shepherd`: `shepherd lane ship` already runs the gate before it
  pushes.

The hook is a guard, not a boundary: the forge's rules are the enforcement. The push
block, the deny rules for `--no-verify` and the other checks on an agent's push still
apply.

## Scope on push

The pre-push hook also refuses a push that changes files outside the lane's scope. The
scope includes the lane's siblings: the test file of a Go source file in scope, the root
`CHANGELOG.md`, and the docs pages the lane was opened with, each while no other open lane
holds it. A sibling another lane holds is refused and the lane is named, as in
`CHANGELOG.md (held by lane feat/other)`. See [lanes](lanes.md#siblings-of-the-scope).
Approved check files are judged first and refused for every lane.

## Command

```
shepherd lane gate [name] [--repo name] [--json]
```

It prints whether the gate passed and, on a failure, the end of its output and where the
whole output is. A gate run is bounded at 20 minutes. It exits non-zero when the gate
fails.
