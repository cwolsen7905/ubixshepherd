# Seeing what is in a lane: `lane diff` and the lane page's changes view

Work in a lane is a real worktree, and `lane diff` shows what it holds against
its base, read-only: it changes no lane, run, or config state.

## `shepherd lane diff <lane>` and the `lane_diff` tool

Against the lane's base (`origin/<base>` when it exists, else the base branch):

1. **Commits**: `git log` of `<base>..HEAD`, oldest first: short SHA, subject, author,
   date, and the files-changed count.
2. **Committed changes**: the diff of `<merge-base>..HEAD`, per file with added and
   removed line counts, rename detection on.
3. **Uncommitted changes**: tracked files modified in the worktree (staged and unstaged,
   against HEAD), with counts.
4. **Untracked files**: path and size, listed and never expanded (their content is only
   shown on request with `--file`, so an invented large file cannot flood anything).

Each file also says whether it is inside or outside the lane's scope (the same glob
check the pre-push hook uses), so an out-of-scope file stands out in the list rather
than at push.

The desk sees at most `desk.tool_output_chars` (default 4000), keeping start and end
of anything longer. A whole diff does not fit, so the read has three modes rather than
one truncated one:

| Mode | Flag | Content |
|---|---|---|
| Stat (default) | `--stat` | head and base SHAs, counts, the commit list (capped at 30), then one line per file: status, scope mark, `+a -d`, path |
| Files | `--files` | the stat plus each file's own hunk header summary, no bodies |
| One file | `--file <path>` | that file's patch (committed plus uncommitted, as the lane would ship it), or an untracked file's start |

Limits are enforced in the read itself, so the web and the CLI get the same bounds:
30 commits, 200 files, 64 KiB per file, 256 KiB total patch bytes. When any limit
cuts, the answer says what was cut and the flag that reads more. A binary file shows
as `binary, N bytes`, never as content. Generated and vendored files are not
special-cased.

The desk's default call is stat; the mapped `lane_diff` tool takes `file` and `files`
arguments so the desk can then read one file at a time. The desk's operator tools
allowlist gains `lane_diff` (it is read-only).

Every string that leaves the read (commit subjects, paths, patch bytes, untracked file
starts) goes through redaction, as CLAUDE.md prescribes. Redaction runs before the
size cut so a cut cannot split a secret into an unmatched half, and the answer says
how many spans were masked.

The API is `GET /v1/lanes/{id}/diff` (`mode`, `file`, `since`, `until`, `run`),
open to the operator, the desk, and browser sessions like other reads, and refused
to agents' worker tokens. An agent reads its own tree itself, and the worker tool set
has no `lane_diff`.

## The gate badge

Shepherd records gate results whenever its own gate command runs in a lane (the
end-of-run push and `lane ship` go through it), with the commit it ran on, the
command, pass or fail, the time, and the source. A run over a worktree with
uncommitted changes is not recorded, since it says nothing about the head.

The badge has four states and never claims more than it knows:

| State | Meaning |
|---|---|
| passed on head | a passing record for the lane's current HEAD SHA |
| failed on head | a failing record for HEAD |
| stale | the last record is for an earlier SHA: commits since the gate ran |
| not run | no record; for a `push: human` repo this is the normal state |

The badge is on `LaneView.gate` (the lane list and a project's open lanes) and on
`LaneDiff.gate` (against the lane's HEAD), so a diff read and the board agree. The
standards checks and the commit rules are not the gate and are not recorded.

## The run view

The diff read takes `run=<id>`, which the daemon resolves to the run's
`start_sha..end_sha`. A run with no recorded end reads from its start and includes
the worktree. It refuses a run of another lane, a run with no start commit, and
`run` together with `since` or `until`. The CLI equivalent is
`shepherd run diff <id>`, a thin wrapper that resolves the lane and passes the range.
The desk gets no second tool: `lane_diff` takes an optional `run` argument instead.

For a run still going, or one that left work behind, the view also shows the
worktree's uncommitted changes, labelled "left uncommitted" and "in the lane's
worktree now", and only for the lane's latest run. Any earlier run hides them,
since that work belongs to the lane.

## The web lane page

The lane page has a third tab, **Changes**, beside Overview and Conversation: the
file list from the stat read (status, out-of-scope mark in the warning tone,
`+a -d`, path; groups for committed, uncommitted, untracked), the commit list above
the files, and each file's patch (`?file=`) fetched on demand and rendered as a
unified diff. Binary and untracked files are never expanded into a diff; an
untracked file shows its start as plain text.

Beside the worktree path is an **open in editor** link, built from a template in
config:

```yaml
web:
  editor_url: "vscode://file/{path}"   # also {path}:{line} for a file's link
```

`{path}` is the worktree's absolute path (or the file's inside it, in the Changes
view), URL-escaped. The default is `vscode://file/{path}`; setting it to `""`
hides the links. The template is validated at load (only `{path}` and `{line}`
placeholders, and only a non-`javascript:`/`data:` scheme). The link is
informational: the browser opens the editor on the person's machine; if the daemon
is remote the worktree path is the daemon's, which the page says in a tooltip.
This is a link, not an in-app editor.

The run page has the same Changes view narrowed to the run, and the gate badge is
on the lane page's facts aside, the Changes summary line, the board's rows, and a
project's open lanes, with the commit, time, and command on hover. The desk doc's
"Reading a lane's changes" section ([desk.md](desk.md)) describes the tool's modes
from the desk's side.
