# Push gate

In a repo whose profile says `autonomy.push: agent`, an agent Shepherd started may push
its own lane's branch to the repo's origin. It may do so only on a commit the repo's gate
has passed on. The repo's gate is the `gate` command in its profile, for example
`make check`.

## How it works

1. The agent commits its work, so the worktree is clean.
2. It runs `shepherd lane gate` in the lane's worktree. The daemon runs the repo's
   configured gate there, against the lane's head commit, and records the result for that
   exact commit. The agent never supplies the command or the result.
3. It pushes. The pre-push hook looks for a passing record for each pushed tip. With one,
   the push goes ahead. Without one, the hook refuses and says to run
   `shepherd lane gate`.

A new commit needs a new run: a pass for one commit says nothing about the next. A record
also stops counting if the repo's gate command changes, and a failing run is recorded as a
failure, so it never lets a push through. `shepherd lane gate` refuses to run with
uncommitted changes, because the gate would then be checking something other than the
commit that is pushed.

## What it leaves alone

- Your own pushes. The check applies while an agent runs in the lane.
- Repos with no `gate` in their profile: there is nothing to check.
- Repos set to `push: shepherd`: `shepherd lane ship` already runs the gate before it
  pushes.

The hook is a guard, not a boundary: the forge's rules are the enforcement. The push
block, the deny rules for `--no-verify` and the other checks on an agent's push still
apply.

## Command

```
shepherd lane gate [name] [--repo name] [--json]
```

It prints whether the gate passed and, on a failure, the end of its output and where the
whole output is. A gate run is bounded at 20 minutes. It exits non-zero when the gate
fails.
