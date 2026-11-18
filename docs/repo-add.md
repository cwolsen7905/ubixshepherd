# Adding and removing repos without running init again

A repo is managed only if `shepherd init` put it in the store. Everything else
about a repo (its profile, its project) lives in `config.yaml` and is reloaded
on SIGHUP, so the two can disagree. These commands see, add, and remove managed
repos without running init again. Adding takes effect at once, with no daemon
restart.

## CLI

```
shepherd repo list [--json]
shepherd repo add <path|name>... [--workspace <name>]
shepherd repo remove <name> [--why <text>]
shepherd repo scan [<workspace>]
```

All four are daemon clients (the CLI never opens the store). Every list reads
the disk, so a repo cloned a minute ago is there.

**`repo list`**: one row per repo: name, workspace, path, remote, open lane count,
and a state: `managed`, `profile only` (named in `config.yaml`, not managed),
`project only` (named by a project, no profile and not managed), or `missing`
(managed, but the directory is gone). Repos found on disk and managed nowhere
show as `found`, with the reason when the scan would not suggest them.

**`repo add <path|name>...`**:

- A *path* is resolved and must be a git repo with a `.git` directory, inside a
  known workspace root. A path outside every workspace is refused with "run
  `shepherd init <dir>` to create a workspace"; adding never creates a workspace.
- A *name* is the repo's name in the workspace (its path relative to the root).
  It is looked up on disk below the root, so `shepherd repo add my-service` works
  when `~/git/my-service` exists. An ambiguous name lists the candidates and refuses.
- Several arguments are all validated before any is added: one bad one adds none.
- Idempotent: an already-managed repo is reported "already managed", exit 0. A repo
  that was removed earlier is managed again, keeping its history.
- A repo that is not a clean first-time candidate is still added when named
  explicitly, and the reason is printed once.
- After adding, it reports whether the repo has a profile (`config.yaml`
  `repos.<name>`) and, if not, that defaults apply. It never writes `config.yaml`.

**`repo remove <name>`**: refuses with the open lanes named, and while a run is
active in the repo. Otherwise it marks the repo unmanaged (`removed` timestamp).
It does not touch the directory, the git remote, `config.yaml`, or the repo's
lane and run history. The profile stays, so a later `repo add` restores the repo
with its settings; `repo list` then shows it as `profile only`.

**`repo scan`**: prints what the daemon is not managing, from the same disk read
as `repo list`, filtered to the workspace named. It adds nothing.

## `init --only` and `--prune`

`init --only a,b` manages those repos, adding to those already managed. Today's
"exactly these" wording is kept honest: after saving it prints "also still
managed, not listed: ..." with the line "to stop managing one:
`shepherd repo remove <name>`". Nothing is ever removed without `--prune`.

`--prune` (with `--only` or interactive) unmanages the managed repos not picked,
through the same open-lane refusal as `repo remove`. Interactive init's list
shows managed repos ticked; unticking one now does what it looks like it does,
after a confirmation naming the repos. `init` prints, at the end, "to add another
repo later: `shepherd repo add <name>`".

## API and store

| Method and path | Purpose |
|---|---|
| `GET /v1/repos` | managed repos plus findings; every call reads the disk |
| `POST /v1/repos` | body `{workspace?, repos: [path or name]}`; adds, returns the details |
| `POST /v1/repos/{id}/unmanage` | refuses 409 with open lanes (names them) or an active run |

`POST /v1/workspaces` is unchanged, so `init` keeps working. The store marks
repos removed rather than deleting them: lane, run, usage, and history rows keep
their `repo_id`, so a removed repo's past stays readable. `SaveWorkspace`
restores a removed repo with its history.
