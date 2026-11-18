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
