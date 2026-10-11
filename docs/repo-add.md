# Adding and removing repos without running init again

Status: **approved as written** by the maintainer (2026-10-11). Implementation is two
merge requests, core first; each is its own lane.

## The problem

A repo is managed only if `shepherd init` put it in the store. Everything else about a
repo (its profile, its project) lives in `config.yaml` and is reloaded on SIGHUP, so the
two can disagree and nothing says so:

- A repo is re-cloned or cloned new under the workspace root, its profile is already in
  `config.yaml`, and `shepherd daemon reload` changes nothing, because reload reads the
  config and never the disk.
- `config.yaml` profiles and `projects.<name>.repos` entries that name a repo the store
  does not manage produce one log line at load and nothing else.
- Re-running `init` interactively works but is clumsy. `init --only a,b` means "exactly
  these": `SaveWorkspace` only adds and updates (it never deletes), so it does not drop
  other repos from the store, but the printed result and the CLI's own wording say
  "manage exactly these", which is not what the store does. `--only` is therefore both
  misleading and no use for "add one more".

There are also no commands to see or remove a managed repo, and the store has no way to
delete one.

## Decisions to make

One decision for the person: approve the design below, or change a numbered choice. My
recommendation is marked in each.

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | Command shape | `shepherd repo add/remove/list`; or flags on `init` | `repo` subcommands; `init` stays the first-time workspace chooser |
| 2 | Remove semantics | Delete the repo row; or mark it unmanaged and keep history | Mark unmanaged (`removed` timestamp), keep lanes, runs and history, refuse with open lanes |
| 3 | Discovery | Silent auto-add; or offer only | Offer only: a finding, never an add |
| 4 | `init --only` | Keep "exactly these"; or warn | Keep the flag, make it honest: it only adds, and it prints which managed repos it did not list (it never removed any). Add `--prune` to unmanage the unlisted ones, with the same open-lane refusal |
| 5 | Web | Settings page, or later | Settings page, cheap: the data and endpoints are the same as the CLI's |

## 1. CLI

```
shepherd repo list [--json]
shepherd repo add <path|name>... [--workspace <name>]
shepherd repo remove <name> [--why <text>]
shepherd repo scan [<workspace>]
```

All four are daemon clients (the CLI never opens the store). No restart: the daemon
changes the store and the live config view in process, the way a Settings save already
reloads.

**`repo list`**: one row per repo: name, workspace, path, remote, open lane count, and a
state: `managed`, `profile only` (named in `config.yaml`, not managed), `project only`
(named by a project, no profile and not managed), or `missing` (managed, but the
directory is gone). Unmanaged repos found on disk show as `found`.

**`repo add <path|name>...`**:

- A *path* is resolved (canonical, as `validateSave` does) and must be a git repo with
  a `.git` directory, inside a known workspace root. A path outside every workspace is
  refused with "run `shepherd init <dir>` to create a workspace"; adding never creates
  a workspace.
- A *name* is the repo's name in the workspace (its path relative to the root). It is
  looked up in the last scan, then on disk below the root, so `shepherd repo add kitg`
  works when `~/git/kitg` exists. An ambiguous name lists the candidates and refuses.
- Several arguments are all validated before any is added: one bad one adds none.
- Idempotent: an already-managed repo is reported "already managed", exit 0. A repo
  that was removed earlier is managed again (same row, `removed` cleared), keeping its
  history.
- It reuses `workspace.Scan`'s `inspect` for remote and stacks, so an added repo looks
  the same as one init found. A repo that is not a clean first-time candidate (the
  scan's `Suggest` is false, with a `Reason`) is added when it is named explicitly,
  and the reason is printed once.
- After adding, it reports whether the repo has a profile (`config.yaml` `repos.<name>`)
  and, if not, that defaults apply. It never writes `config.yaml`.

**`repo remove <name>`**: refuses with the open lanes named ("kitg has 2 open lanes:
feat/a, fix/b: close them first"), also refuses while a run is active in the repo.
Otherwise it marks the repo unmanaged. It does not touch the directory, the git
remote, `config.yaml` or the repo's lane/run history. The profile stays, so a later
`repo add` restores the repo with its settings; `repo list` then shows it as `profile
only`, which is accurate.

**`repo scan`**: asks the daemon to rescan the workspace root(s) now and print the
findings (section 3). It adds nothing.

## 2. Operator tools and the web UI

MCP tools in `internal/cli/mcp.go`, each mapped onto its CLI command (the existing
rule: add the command first, then the mapping):

| Tool | Maps to | Notes |
|---|---|---|
| `repo_list` | `repo list --json` | reads; also what the desk shows for "which repos do you manage?" |
| `repo_add` | `repo add <names>` | `names`: array of path or name |
| `repo_remove` | `repo remove <name>` | refuses with open lanes, like the CLI |
| `repo_scan` | `repo scan` | optional; lets the desk answer "did you see the new clone?" |

The desk is the person's agent, so `repo_add` and `repo_remove` are caller class
`desk` (not operator-only) and go through `s.acted`, so they are recorded as desk
actions on the feed like lane close. They are state changes inside the person's own
workspace, reversible, and need no extra confirmation beyond the person asking. Worker
(lane agents) get none of them: an agent must not widen its own reach.

The web **Settings** page gets a *Repos* section: the managed list with an
*Unmanage* button (disabled with the reason when lanes are open), and a *Found, not
managed* list with an *Add* button each, fed by the same `GET /v1/repos` and
`POST /v1/repos`. Session rules are those of Settings: a session that cannot save
sees no buttons. The existing *add a repo* box in the sidebar adds a config profile;
it gains a note when the named repo is not managed, and an *Add to managed repos*
link.

## 3. Discovery

Shepherd offers; it never adds.

- **Findings, not adds.** The daemon keeps an in-memory list of findings of two kinds:
  - `new_repo`: a git repo under a workspace root (using the existing
    `workspace.Scan`, same depth, same skips, `*-worktrees` ignored) that is not
    managed.
  - `config_unmanaged`: a name in `config.yaml` `repos` or `projects.*.repos` that is
    not managed *and* exists on disk under a workspace root, or exists in neither
    place (then the finding says "named in config, not found on disk").
- **When.** At daemon start, on every config reload (SIGHUP, `daemon reload`, a
  Settings save), and on demand by `repo scan` or `GET /v1/repos?rescan=1`. No timer in
  v1: a periodic walk of a large workspace costs disk reads for a rare event, and the
  reload is what the person already runs. A timer is easy to add later behind
  `daemon.repo_scan: <duration>` (default off) if wanted.
- **Where it shows.**
  - One feed line per *new* finding, once (the finding set is diffed against the
    previous one so a reload does not repeat it): "kitg has a profile in config.yaml
    but is not managed: `shepherd repo add kitg`". Kind `config` (existing), so the
    feed's closed set of kinds does not change.
  - `shepherd status` and the web daemon status show "N repos found, not managed".
  - The desk's brief mentions them, so the person can answer "yes, add kitg" in chat
    and the desk calls `repo_add`.
  - `config.Load`'s existing log line stays; the finding is the user-visible
    version of it.
- **No exceptions that surprise.** A repo the person removed with `repo remove` is
  remembered (`removed` is set), and its `new_repo` finding is suppressed, so it is
  not offered again every reload. `repo scan` shows suppressed ones with a marker, and
  `repo add` of it still works.

## 4. `init --only`

Keep `--only` and its name, because scripts may use it, but fix the wording and the
behaviour the person actually gets, since the store never deletes:

- Help text: "manage these repos (comma-separated names), adding to those already
  managed". Today's "exactly these" is wrong.
- After saving, print "also still managed, not listed: a, b" when `--only` left managed
  repos out, with the line "to stop managing one: `shepherd repo remove <name>`".
- `--prune` (new, with `--only` or interactive) unmanages the managed repos not picked,
  through the same code path and open-lane refusal as `repo remove`. Interactive init's
  list already shows managed repos ticked; unticking one now does what it looks like it
  does, after a "this stops managing N repos" confirmation.
- `init` prints, at the end, "to add another repo later: `shepherd repo add <name>`".

The alternative, making `--only` truly exact without a flag, would be a surprising
destructive change to a command people already run; it is not recommended.

## 5. API, store, tests, docs

### API (`internal/api`, `internal/daemon`)

| Method and path | Class | Purpose |
|---|---|---|
| `GET /v1/repos` | desk, web | managed repos plus findings; `?rescan=1` rescans first |
| `POST /v1/repos` | desk (acted) | body `{workspace?, repos: [path or name]}`; adds, returns the details |
| `POST /v1/repos/{id}/unmanage` | desk (acted) | refuses 409 with open lanes (names them) or an active run |

`POST /v1/workspaces` is unchanged, so `init` keeps working and older clients are not
broken. `internal/client` gains `Repos`, `AddRepos` and `UnmanageRepo`.

### Store (`internal/store`, `internal/store/sqlite`)

One appended migration, never an edit:

```sql
ALTER TABLE repos ADD COLUMN removed TEXT NOT NULL DEFAULT '';
```

`''` means managed; otherwise the RFC 3339 time it was unmanaged. Interface changes:

- `Repos(ctx, workspaceID)` returns managed repos only (filters `removed = ''`),
  so every existing caller (lanes, watch, work, desk) sees only managed repos with no
  change; a new `AllRepos` includes removed ones for `repo list` and finding
  suppression.
- `SaveWorkspace` clears `removed` when it upserts a repo, so `init` and `repo add`
  restore a removed repo with its history.
- `SetRepoRemoved(ctx, id, time.Time)` and `OpenLaneCount`-style check: reuse `Lanes`.

Lanes, runs, usage and history keep their `repo_id`, so a removed repo's past stays
readable in history and stats.

### Fold and watch

`internal/fold` and `internal/watch` read `Store.Repos`, so they stop acting on a
removed repo with no change. The runner's startup check that stops agents left by a
crash is unaffected.

### Tests

- `internal/store/sqlite`: migration applies on an existing database; `Repos` hides a
  removed repo; `SaveWorkspace` restores it; history of a removed repo is still read.
- `internal/daemon`: add by path and by name; ambiguous name refused; outside a
  workspace refused; batch is all-or-nothing; remove refused with an open lane and
  with an active run, allowed otherwise; idempotent add; findings after reload for a
  profile-only repo, diffed so the feed line appears once; a removed repo is not
  re-offered; worker class gets 403.
- `internal/workspace`: a helper that resolves a name under a root, ambiguity included.
- `internal/cli`: `repo add/remove/list/scan` against a fake daemon; `init --only`
  prints the not-listed note; `--prune` refuses with open lanes.
- `internal/cli/mcp_test.go`: each tool maps to its command, and worker mode lists none.
- Real git repos with a bare origin for any test that needs one, as the Fold's tests do.

### Docs

- `README.md` and `docs/INSTALL.md`: after the `init` line, "add a repo later with
  `shepherd repo add <name>`; see what was found with `shepherd repo scan`".
- `docs/config.md`: a repo is managed in the store, not by `config.yaml`; a profile
  does not manage a repo; the finding that says so.
- `docs/v1.md` and `docs/roadmap.md`: a line under M2 (workspace) for repo
  add/remove.
- `docs/web-settings.md`: the Repos section.
- `docs/desk.md`: the three tools and the finding in the brief.
- `CLAUDE.md`: one bullet under Code: where repo management lives and that the
  store marks repos removed rather than deleting them.

### Files it will touch

`internal/cli/repo.go` (new), `internal/cli/init.go`, `internal/cli/commands.go`,
`internal/cli/mcp.go`, `internal/api/api.go`, `internal/client/*.go`,
`internal/daemon/repos.go` (new), `internal/daemon/daemon.go` (routes),
`internal/daemon/reload.go` (scan on reload), `internal/store/store.go`,
`internal/store/sqlite/sqlite.go`, `internal/workspace/scan.go`, `internal/desk`
(brief), `web/` (Settings section), the docs above, and the tests listed.

## Sequencing

MR !121 (`feat/backlog-desk-events`) has merged, so `internal/store`, `internal/daemon`,
`internal/api` and `internal/client` are free. Two merge requests, each green on its own:

1. **Core**: migration, store, API, client, `repo add/remove/list/scan`, `init --only`
   wording and `--prune`, tests, README and INSTALL.
2. **Surfaces**: findings on reload and in the feed and status, the MCP tools and desk
   brief, the Settings section, the remaining docs.

Part 1 alone fixes the reported case: `shepherd repo add kitg` with no restart.

## Not in scope

Writing `config.yaml` from `repo add` (a profile is the person's, edited in Settings),
cross-workspace moves of a repo, deleting a repo's history, cloning a repo that is not
on disk, and the typed cross-repo work orders of M3.
