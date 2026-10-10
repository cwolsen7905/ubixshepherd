# Daemon status in the web UI

The dot in the web UI's top bar shows whether the daemon answers. Hovering it, focusing it
with the keyboard, or tapping it opens a compact popover with what the daemon is and what
it is doing. Escape closes it, as does moving away or tapping outside. The dot points at
the popover with `aria-describedby`.

## What it shows

| Group | Fields |
| --- | --- |
| Daemon | version and commit, build time, uptime and start time, OS/arch, host name |
| Config | path, when it was loaded or last reloaded, and the last reload's error if it failed |
| Activity | agents running, open lanes, decisions waiting, quota holds (agent and reset time) |
| Spend today | spent, against the daily budget when one is set |
| Forge watcher | time of the last successful poll, or its last error |
| This browser | event stream state, time since the last event, whether this session may save settings, when it ends |

While the connection is lost the popover says since when, and that it is retrying; the
daemon's own fields are not shown then, as the daemon is the one not answering.

## API

`GET /v1/status` is unchanged, so the probe every command makes stays small. With
`?detail=1` its answer gains a `detail` object (`api.StatusDetail`). It is read-only and
open to the operator, the front desk and browser sessions; an agent's token gets the
plain status. It holds no token, environment or secret: a reload error and a watcher
error pass through `internal/redact` and are cut to one line.

The page reads it only while the popover is open, and again every five seconds. The
session's own fields (`allow_settings`, `expires`) come from `GET /v1/web/session`.

Most of `detail` comes from what the daemon already keeps: the store (runs, lanes,
decisions, spend), the live config and the reload outcome. The commit and build time are
Go's VCS stamp in the binary, so they are absent from a `go install` build.

## Not reported yet

Two fields need state owned by packages outside this change. The server has hooks for
them (`Server.QuotaHolds`, `Server.ForgeWatch`); until the daemon's command wires them,
`quota_holds` and `forge` are absent and the popover says "not reported" rather than
"none":

- Quota holds: the runner keeps them privately (`dispatch.Runner`), with no accessor.
- Forge watcher: `watch.Watcher` keeps no last-poll time or last error, and the daemon
  command starts it.
