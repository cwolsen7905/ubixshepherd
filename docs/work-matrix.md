# The work matrix

A repo may keep a work matrix, `docs/WORK.md`: what is planned, in progress, blocked and
done, in one table. Shepherd reads it and shows it in the **Work** section of the
project page in the web UI, next to the lanes that do the work. It is optional: a repo
without one shows a quiet hint.

## Format

The file has a `## Work` heading, then one markdown table with exactly these columns:

```markdown
## Work

| ID | Area | Item | Status | Lane | PR | Notes |
|----|------|------|--------|------|----|-------|
| W1 | web  | Work page on the project page | doing | feat/work-view | #12 | |
| W2 | core | Typed cross-repo work orders | todo | - | - | needs design |
```

| Column | Holds |
|--------|-------|
| ID | A short id, unique in the file. |
| Area | Free text: the part of the product. |
| Item | What the work is. Required. |
| Status | One of `todo`, `doing`, `blocked`, `done`, `dropped` (any case). |
| Lane | A Shepherd lane name (or its branch), or `-`. |
| PR | `#N`, the pull request or merge request number, or `-`. |
| Notes | Free text. A literal pipe is written `\|`. |

Prose before the heading and after the table is ignored.

## How it is read

The parser (`internal/work`) is tolerant of whitespace, alignment rows (`|:--|--:|`),
header case and CRLF line endings, and strict on the columns and the status. A row it
cannot use is reported as a warning with its line number and the reason, and is not
shown as a row; nothing is dropped silently. A missing heading, a missing table or a
header that is not the seven columns in order is one warning for the whole file. A
duplicate ID keeps the first row and warns about the second.

The daemon reads the file from the repo's base branch as of the last fetch
(`origin/<base_branch>`, through `git`), never the working tree, so the page shows what
has merged. It does not fetch for this; the fold's own fetches move the ref. The parsed
file is cached per repo and keyed by the file's blob hash on that ref, so a read costs
one `git rev-parse` until the file changes.

## API

`GET /v1/projects/{name}/work` (and `GET /v1/unassigned/work` for the repos in no
project) returns, for each of the project's repos, whether the file was found, the ref
and blob it was read from, its rows and its warnings, then the project's drift. Each row
carries `lane_id` when the repo has a lane of that name, and `pr_url` built from the
repo's origin remote: `/pull/N` on github.com, `/-/merge_requests/N` on any other host
(taken to be GitLab, as elsewhere). An unreadable remote leaves `pr_url` empty and the
page shows plain `#N`. The client call is `Client.ProjectWork`.

## Drift

Drift is where the matrix and the lanes disagree. It is a flag, not an error.

| Kind | Meaning |
|------|---------|
| `lane_untracked` | A lane is open and no row names it. A `done` or `dropped` row does not count as naming it. |
| `doing_lane_closed` | A row is `doing` and its lane is closed. |
| `doing_lane_unknown` | A row is `doing` and names no lane, or a lane the repo does not have. |
| `no_work_file` | The repo has no `docs/WORK.md` on its base ref. Shown as a hint. |

Lanes are matched by name or branch within the row's own repo. Closed lanes are read from
the most recent 500 closed in the repo.

## The page

Rows are grouped by status: `doing` and `blocked` first, then `todo`; `done` and
`dropped` are collapsed. A row links to its lane page and to its PR or MR. Drift and
unreadable rows are listed beneath the groups.
