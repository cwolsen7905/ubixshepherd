# CLI reference

Commands that this page documents. `shepherd` with no arguments lists every command;
each is a client of the daemon's HTTP API.

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
```

Reads of Crook's verdict log, described in [crook-why.md](crook-why.md). `rules` counts
shadow rows apart from what Shepherd did, and `shadow` lists them: see
[merge-rules.md](merge-rules.md). Every form takes `--json`. `shadow` is also the
`crook_shadow` MCP tool (`rule`, `since`).
