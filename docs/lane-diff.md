# Seeing what is in a lane: `lane diff` and the lane page's changes view

Status: **proposed**, for the maintainer's approval. Plan only: nothing here is built.
Implementation is three merge requests, each its own lane (sequencing below).

## The problem

Work in a lane is invisible until it reaches a merge request. Every lane is a real
worktree, but nothing in Shepherd shows what is in it. That has hidden:

- built and tested work left uncommitted in a lane;
- repos set to `push: human`, whose lanes never become merge requests, so nothing ever
  shows their content;
- an untracked file an agent invented, found only when `lane close` refused;
- a commit the gate never ran on;
- two models' work on similar tasks, which could be compared only by reading logs.

This plan adds one read, "what is in this lane against its base", and shows it in the
three places a person or the desk looks: the CLI, the desk's tools and the web lane page.
It is read-only throughout: it changes no lane, run or config state.

## Decisions to make

One decision for the person: approve the design below, or change a numbered choice. My
recommendation is marked in each.

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | Where the git work lives | `internal/fold` through `internal/git` / a new package | `internal/fold` (a `Fold.Diff` method), as the lane's owner |
| 2 | Output modes | one mode / summary, files and one file | Three: `--stat` (default), `--files`, `--file <path>` |
| 3 | Where "the gate passed on this head" comes from | recompute on read / record at gate time | Record at gate time, one new table; show "not recorded" honestly |
| 4 | Run view | link only / show the run's own diff | Show it, as a range `start_sha..end_sha` of the same read |
| 5 | Editor link | fixed `vscode://` / configurable template | Configurable template, default `vscode://file/{path}` |
| 6 | Transport | desk runs git in the worktree / daemon API | Daemon API only, like every command |

## 1. `shepherd lane diff <lane>` and the `lane_diff` tool

### What it shows

Against the lane's base (`origin/<base>` when it exists, else the base branch, the same
target `internal/fold/review.go` already uses; the lane's recorded `Base` is the name):

1. **Commits**: `git log` of `<base>..HEAD`, oldest first: short SHA, subject, author,
   date, and the files-changed count.
2. **Committed changes**: the diff of `<merge-base>..HEAD`, per file with added and
   removed line counts, rename detection on.
3. **Uncommitted changes**: tracked files modified in the worktree (staged and unstaged,
   against HEAD), with counts.
4. **Untracked files**: path and size, listed and never expanded (their content is only
   shown on request with `--file`, so an invented 40 MB file cannot flood anything).

Each file also says whether it is **inside or outside the lane's scope** (the same glob
check the pre-push hook and `Run.Outside` use), so an out-of-scope file stands out in the
list rather than at push.

### Modes and size limits

The desk sees at most `desk.tool_output_chars` (default 4000, minimum 500), keeping start
and end of anything longer. A whole diff does not fit, so the read has three modes rather
than one truncated one:

| Mode | Flag | Content | Typical size |
|---|---|---|---|
| Stat (default) | `--stat` | head and base SHAs, counts, the commit list (capped at 30), then one line per file: status, scope mark, `+a -d`, path | well under 4000 chars for most lanes |
| Files | `--files` | the stat plus each file's own hunk header summary, no bodies | list only; capped at 200 files with a "N more" line |
| One file | `--file <path>` | that file's patch (committed plus uncommitted, as the lane would ship it), or an untracked file's start | capped at 64 KiB, then cut on a hunk boundary with a note |

Limits are enforced in `Fold.Diff`, not only at the tool's output, so the web and the CLI
get the same bounds: 30 commits, 200 files, 64 KiB per file, 256 KiB total patch bytes.
When any limit cuts, the answer carries `truncated` and a line naming what was cut and
the flag that reads more. A binary file shows as `binary, N bytes`, never as content.
Generated and vendored files are not special-cased in v1.

The desk's default call is stat; the mapped tool takes `file` and `files` arguments so the
desk can then read one file at a time, which also fits its 4000-char cap.

### Redaction

Every string that leaves `Fold.Diff` (commit subjects, paths, patch bytes, untracked file
starts) goes through `internal/redact`, per CLAUDE.md. A secret an agent pasted into a
file is masked in the diff even though it is in the worktree. Redaction runs before the
size cut so a cut cannot split a secret into an unmatched half, and the answer says
how many spans were masked so a person knows the view is not the raw bytes.

### Where it lives

- `internal/git`: small helpers that wrap `git.Run` (`DiffNumstat`, `LogRange`,
  `UntrackedFiles`). The git CLI only, never a library, and every flag checked against
  the 2.31 floor (`diff --numstat -z`, `log --format`, `ls-files --others
  --exclude-standard -z`, `diff --no-ext-diff --no-color`; none newer than 2.31).
  `--no-ext-diff` and `--no-textconv` keep a repo's config from running programs.
- `internal/fold/diff.go`: `Fold.Diff(ctx, laneID, DiffRequest) (LaneDiff, error)`, the
  types, the limits and the redaction. It refuses a lane with no worktree on disk with a
  plain message.
- `internal/api`: `GET /v1/lanes/{id}/diff?mode=&file=&since=&until=`, open to the
  operator, the desk and browser sessions like other reads, and refused to agents'
  worker tokens (an agent reads its own tree itself).
- `internal/client` and `internal/cli/lane.go`: `shepherd lane diff <lane> [--repo r]
  [--files | --file path] [--json]`. Plain text by default, JSON for the web and tests.
- `internal/cli/mcp.go`: `lane_diff`, mapped onto the command first (as CLAUDE.md
  prescribes): arguments `repo`, `name`, optional `files` (bool) and `file` (string).
  The description says what each mode costs in characters.
- The desk's operator tools allowlist gains `lane_diff` (it is read-only).

## 2. The web lane page

### Changes view

`web/src/pages/Lane.tsx` has tabs Overview and Conversation. A third tab, **Changes**,
is added, rather than another section in Overview, because the diff is large and
Overview is the glanceable page.

- File list from the stat read: status, scope mark (out-of-scope in the warning tone),
  `+a -d`, path. Groups: committed, uncommitted, untracked.
- Click a file: its patch (`--file`), fetched on demand and rendered as a unified diff
  with the line classes the web's existing monospace blocks use. No syntax highlighting
  and no new dependency in v1.
- The commit list above the files, each commit with its short SHA and subject.
- The read is refetched when the lane's feed advances (the page already keys
  conversation refresh on the feed's last item) and by a Refresh button; it is not
  polled on its own, since each read runs git.

### Open in editor

A link beside the worktree path, built from a template in config:

```yaml
web:
  editor_url: "vscode://file/{path}"   # also {path}:{line} for a file's link
```

`{path}` is the worktree's absolute path (or the file's inside it, in the Changes view),
URL-escaped. The default is `vscode://file/{path}`; setting it to `""` hides the links,
and `cursor://file/{path}`, `idea://open?file={path}` or any other scheme works with no
code change. The template is validated at load (only `{path}` and `{line}` placeholders,
and only a non-`javascript:`/`data:` scheme), and rendered client-side. The link is
informational: the browser opens the editor on the person's machine; if the daemon is
remote the worktree path is the daemon's, which the page says in a tooltip.
This is a link, not an in-app editor (out of scope).

### The gate badge

**Does Shepherd record gate results per commit today? No.** The gate runs in
`internal/dispatch/ship.go` (`Runner.gate`) and its outcome becomes a `gate` feed line
(`gate passed in lane X`) tied to a lane id and a time, not a commit. `LaneForge` keeps
`GateTries` only. So today the fact "the gate passed on this head" is not derivable: a
feed line says a gate passed at some time, and a later commit silently invalidates it,
which is exactly the "commit the gate never ran on" gap.

Options:

| Option | Cost | Verdict |
|---|---|---|
| Recompute on read: run the gate when the page opens | minutes of CPU, side effects | rejected |
| Parse the feed text for the last "gate passed" | brittle, still no SHA | rejected |
| Record at gate time: a `gate_results` row (`lane_id`, `sha`, `gate` command, `passed`, `at`, `source`) | one appended migration, written at the two call sites | **recommended** |

`source` is `ship` (Shepherd's own run before a push, the authoritative one) or `agent`
if a later change lets Shepherd record an agent's own gate run from its hook. v1 records
only Shepherd's runs, so the badge means "Shepherd ran the gate on exactly this head and
it passed". The badge has four states and never claims more than it knows:

| State | Meaning |
|---|---|
| passed on head | a passing record for the lane's current HEAD sha |
| failed on head | a failing record for HEAD |
| stale | the last record is for an earlier sha: commits since the gate ran (the gap above) |
| not run | no record; for a `push: human` repo this is the normal state, and the badge links to the explanation rather than looking like a failure |

The `LaneView` gains `gate` (state, sha, at, command), so the Board and the desk's
`lane_list` can show it too without a second read. Because `push: human` lanes never
reach Shepherd's gate, this plan also proposes (not part of the three merge requests, a
separate decision) a `shepherd lane gate <lane>` that runs the gate in the lane and
records it; the badge's "not run" state would then have an action. Listed here only so
the schema leaves room (`source`).

## 3. The run view

The Runs table on the lane page and the run's own page show `commits` and `outside`
counts today. Recommendation: the run page gains the same Changes view, narrowed to the
run: its commits are `start_sha..end_sha` (both already on `store.Run`), which the diff
read accepts as `since` and `until`. For a run still going, or one that left work behind,
the view also shows the worktree's uncommitted changes, labelled "left uncommitted",
because that is the case this plan exists for. Uncommitted work belongs to the lane,
not the run, so the label is "in the lane's worktree now" and appears only for the lane's
latest run. The run's `Outside` list is shown as the scope mark on those files.

The CLI equivalent is `shepherd run diff <id>`, a thin wrapper that resolves the lane
and passes the range. The desk gets no second tool: `lane_diff` takes an optional `run`
argument instead.

## How it fits the existing pages and docs

- Lane page: a third tab and a gate badge in the lane facts aside, beside the existing
  MR and pipeline badge; the aside's "Commits" line becomes a link into Changes.
- Run page: the Changes view above.
- Board: only the gate badge (small), nothing else.
- Docs: a new `docs/web-lane-diff.md` is **not** added; this document becomes the
  reference once built, and the same sections move into it (the pattern of
  `docs/web-projects.md`). `docs/desk.md`'s "What tools return to the desk" gains one
  paragraph on `lane_diff`'s modes. `docs/config.md` documents `web.editor_url`.
  `docs/roadmap.md` and `CHANGELOG.md` get one line each. `docs/v1.md` is untouched.

## Out of scope

- An in-app editor, staging, committing, discarding or reverting from the page.
- Side-by-side comparison of two lanes: a later **#3**. Room is left for it: the diff
  read is keyed by lane and an optional range rather than by "the current lane", the
  types are `LaneDiff` values a comparison can hold two of, and the web's file and patch
  components take a read as a prop. #3 then adds a comparison of two reads, and perhaps a
  `base` argument, with no change to what is built here.
- Syntax highlighting, inline review comments, and diffing against anything but the base
  and a run's own range.
- Running the gate from the page (see above; a separate decision).

## Files it will touch

| Area | Files |
|---|---|
| git helpers | `internal/git/diff.go`, `internal/git/diff_test.go` |
| the read | `internal/fold/diff.go`, `internal/fold/diff_test.go` (real repo with a bare origin, per the package's rule) |
| API and client | `internal/api/api.go` (routes and types), `internal/api/lane_diff.go`, `internal/client/` |
| CLI and tool | `internal/cli/lane.go`, `internal/cli/run.go`, `internal/cli/mcp.go`, `internal/cli/mcp_test.go`, `internal/desk` (tool allowlist) |
| config | `internal/config/config.go` (`web.editor_url`), tests |
| gate record | `internal/store/store.go`, `internal/store/sqlite/` (an appended migration and the table), `internal/dispatch/ship.go` (the two call sites) |
| web | `web/src/pages/Lane.tsx`, `web/src/pages/Runs.tsx`, `web/src/components/` (`Changes`, `GateBadge`), `web/src/api/types.ts`, `web/src/api/client.ts`, `web/src/styles/`, their tests |
| docs | this file, `docs/desk.md`, `docs/config.md`, `docs/roadmap.md`, `CHANGELOG.md` |

## Sequencing

Three merge requests, each reviewable alone, each green on its own gate:

1. **Core read, CLI and desk tool** (`internal/git`, `internal/fold`, API, client, CLI,
   MCP mapping, docs/desk.md). Solves the desk's and the person's terminal's blindness
   first, and fixes the types the web will consume.
2. **Web Changes tab and editor link** (web, `web.editor_url`). Depends on 1.
3. **Gate record and badge** (migration, `ship.go`, `LaneView.gate`, the badge on the
   lane page and Board, the run page's range view). The only schema change, so the
   others do not wait on it; the web in 2 renders no badge until 3 lands.

Scope for each lane follows the file table above. Tests: `Fold.Diff` against real repos
with uncommitted, staged, untracked, renamed, binary and secret-bearing files, and each
limit; the MCP mapping and the desk cap in `mcp_test.go`; the web with fakes, as the
existing lane page tests do. A real desk call and a real browser check are manual
steps before each merge, as for other desk and web changes.

## Risks

- A lane with a huge untracked tree (a stray `node_modules`): `ls-files --others` is
  bounded by the file cap and honours `.gitignore`; the read stops listing at the cap
  rather than walking everything.
- Reads run git in a worktree a run is writing to. They are read-only
  (`--no-optional-locks`), but a diff mid-write can show a half-written file; the view is
  labelled with its read time.
- The gate record is only as good as Shepherd's own runs; the badge's four states say so
  rather than implying coverage it lacks.
