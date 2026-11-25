# CLI reference

Commands that this page documents. `shepherd` with no arguments lists every command;
each is a client of the daemon's HTTP API.

## run

```sh
shepherd run status <id> [--json]
```

`run status` reports on one agent run, leading with what matters: the agent's final
report (its latest report that is not progress, with the full summary text), then a
`needs` line naming what waits on the person (open decisions the run asked and the
questions it asked that are pending or have a reply ready for it to read, or `none`;
questions already routed to another lane or waiting on the front desk are not listed), then the run's header fields
(state, agent, time, cost, commits), then the last 40 lines of its log. With `--json`
it prints the run's view as `GET /v1/runs/{id}` answers it, with `report_status`,
`report_text`, `needs_person` and `log_tail` added; the view's own fields are
unchanged.

The same report is the `run_status` MCP tool (`id`).

## workspace

```sh
shepherd workspace rename <name|path> <new-name>
```

Renames a registered workspace without running `shepherd init` again, so the repo picker
is not shown.

- The first argument is the workspace's current name or its path (`shepherd status` lists
  both).
- The new name must be non-empty, contain no slashes, and not be used by another
  workspace. Otherwise the command refuses and says why.
- Only the name changes. The workspace's path, repos, lanes, runs and history are keyed by
  its id and stay as they are.
- The web UI follows the workspace by id, so it is unaffected.
- Anything that names the workspace by its old name needs the new one: `--workspace NAME`,
  `$SHEPHERD_WORKSPACE`, and a host's `workspace` in `hosts.yaml`. Until then a command
  fails with a "no workspace" error that lists the names that exist.

The same operation is the `workspace_rename` MCP tool (`workspace`, `name`), and the HTTP
route `POST /v1/workspaces/{id}/rename` with `{"name": "..."}` (operator token only).

## crook

```sh
shepherd crook why <id> | why run <id> | why lane <name>
shepherd crook log [--rule R] [--action A]
shepherd crook rules [--since 7d]
shepherd crook shadow [--rule R] [--since 14d] [--limit N]
shepherd crook layers [--repo R]
```

Reads of Crook's verdict log, described in [crook-why.md](crook-why.md). `rules` counts
shadow rows apart from what Shepherd did, and `shadow` lists them: see
[merge-rules.md](merge-rules.md). Every form takes `--json`. `shadow` is also the
`crook_shadow` MCP tool (`rule`, `since`). `layers` shows the effective rule files for a
repo, by layer, and is described in [crook.md](crook.md).

## lane open

```sh
shepherd lane open <name> --scope GLOB... [--repo R] [--branch B] [--item N] [--adopt]
```

`lane open` cuts a new branch from the repo's fresh `origin/<base>` and adds a worktree
for it. It refuses a name or branch that already exists, and says to use `--adopt`.

With `--adopt` the branch must already exist on origin (an open merge request's, for
example). The worktree checks it out at `origin/<branch>` with that upstream, instead of
cutting a new branch, so the agent can push review fixes to it. Everything else holds:
the scope must not overlap another open lane's, and the push rules apply. It is refused
when another open lane already holds the branch, or when a local copy of the branch
differs from origin's. Closing an adopted lane removes the worktree but never deletes
the branch, merged or not. See [lanes](lanes.md).

The `lane_open` MCP tool takes the same choice as `adopt: true`.

## cluster and worker

```sh
shepherd cluster status [--json] | join <https URL> <code>
shepherd worker add <name> | list [--json] | revoke <name> | remove <name> | confirm <name>
```

Clustering is opt-in, described in [cluster.md](cluster.md). `cluster status` says what
this daemon is (a controller, a worker or alone), and `cluster join` enrols it as a worker
with a code from the controller. The `worker` subcommands run on a controller and manage
the machines that link to it. `cluster status` is also the `cluster_status` MCP tool.

`shepherd member ...` is the old name of the `worker` subcommands: it still runs them for
one release, after a note on stderr.
