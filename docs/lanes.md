# Lanes

A lane is one branch, its worktree and the scope of paths its work stays inside. Open
one with `shepherd lane open` (see [the CLI reference](cli.md#lane-open)).

## Created and adopted branches

| | Created (default) | Adopted (`--adopt`) |
|---|---|---|
| Branch | New, cut from `origin/<base>` | Must exist on origin; checked out at `origin/<branch>` |
| Upstream | None until first push | `origin/<branch>` |
| Refused when | The branch exists locally or on origin | Origin lacks it, another open lane holds it, or a differing local copy exists |
| Scope and push rules | Apply | Apply |
| Close | Deletes the branch once merged | Removes the worktree, keeps the branch |

Adopting is for work on a branch someone else opened, typically a merge request that
needs review fixes. The lane records that it adopted the branch (in the repo's git
config, as `branch.<name>.shepherdadopted`), so a close never deletes a branch the lane
did not create, including when the forge reports the request merged.

## Siblings of the scope

A lane's scope lists the files it works on. Three kinds of file travel with that work, so
a lane may change them without widening its scope, provided no other open lane holds
them:

| Sibling | Allowed when |
|---|---|
| `foo_test.go` | the scope covers `foo.go` in the same directory |
| `CHANGELOG.md` | always: the changelog at the repo root |
| A docs page the lane was opened with | the page is named in the lane's `docs` list at open |

Nothing is added to the lane's scope. The rule is applied each time a path is judged:
by the pre-push check (see [the push gate](push-gate.md)) and by the lane diff, which
marks a sibling as in scope. A sibling is not a lease: two lanes may both reach
`CHANGELOG.md` as a sibling, and a lane may open over a file another lane has as a
sibling.

A sibling that another open lane's scope covers is held by that lane and stays outside
this one. The push refusal names the holder: `src/foo_test.go (held by lane b)`. When the
holder closes, or narrows its scope off the file, the sibling is allowed again. A path
that is not one of these three kinds is outside the scope as before, and so is an
approved check file, which is judged first for every lane. Other languages' test files
(`foo.test.ts`, `test_foo.py`) are not siblings; list them in the scope.

The docs list is exact repo-relative file paths, not globs. It is the `docs` field of the
lane open request in the HTTP API; it is kept in the repo's git config under
`branch.<branch>.shepherddocs` and cleared when the lane closes. This is a different
thing from the scope widening a run can ask for (the repo's `widen` setting, in [config](config.md)), which
changes the scope itself and needs the repo to opt in.
