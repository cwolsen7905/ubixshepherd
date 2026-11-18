# Crook rule layers

Crook's rules are files, one rule per `rules/<id>.md`, kept in layers. This page covers
how the layers are found and combined. For the verdict log see
[crook-why.md](crook-why.md); for merge rules see [merge-rules.md](merge-rules.md).

## The layers

Layers apply earliest first. A later layer's rule with the same id replaces the earlier
one, and a file that says `disabled: true` switches that rule off.

| Layer | Where it lives | Read from |
|---|---|---|
| builtin | Compiled into Shepherd | Not a file; it cannot be edited |
| workspace | A folder on this machine, shared by every repo | The folder's working files |
| project | A folder on this machine, shared by one project's repos | The folder's working files |
| repo | A folder inside the repo | The repo's base branch |

Each layer folder holds its rules in `rules/`. Only plain `.md` files count: a symlinked
file, or a `rules` folder that is a symlink, is not followed.

## The repo layer comes from the base branch

A lane works in a worktree it can edit, so a lane could loosen its own rules by editing
them there. Crook therefore reads the repo layer from the repo's base branch, as
committed, and never from a worktree or from a lane's branch. Uncommitted changes and
commits on a lane's branch have no effect until they reach the base branch.

Whether a person reviews that change depends on the forge, not on Crook. Crook does not
yet stop an agent from merging a change under the layer folder: where a repo sets
`autonomy.merge: agent`, only the forge's own rules (approvals, code owners, protected
paths) hold it back. To keep a lane from loosening its own rules, require a person's
approval on the layer folder in the forge, such as a code owners entry for it.

If the base branch cannot be read, the repo layer is reported as not loaded and the other
layers still apply. It is never treated as an empty layer without saying so. A repo with no
rules folder at the base branch simply has no repo rules.

## Where the folders are

The repo's layer folder defaults to `.shepherd`; the loader accepts another relative
path of plain names (no `..`, no absolute path, no `.git` part). The workspace and
project folders are directories handed to the loader. Nothing outside these places is
read.

## Replacing and loosening

When a later layer replaces or switches off a rule and the result is less restrictive
than before (the order is refuse, ask, hold, proceed), the change is marked as
loosening. Seeing the effective rules by layer shows each replaced rule, what it
replaced and whether it loosens, along with every rule file that failed to load and why.
A file that fails to load is left out and does not remove the earlier layer's rule.
