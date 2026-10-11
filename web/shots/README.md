# Web UI screenshots

Repeatable screenshots of the web UI with made-up data, for docs, blog posts and merge
requests. Nothing here talks to a real daemon, and no image is committed: `out/` is
ignored.

```sh
web/shots/shots.sh          # builds web/dist if needed, writes web/shots/out/*.png
```

It needs Node 22 or later and Chrome or Chromium. The script starts `stub-daemon.mjs`,
which serves the built app and canned `/v1` answers (acme-* repos on `git.example`),
drives headless Chrome over the DevTools protocol (`shoot.mjs`), and stops both.

It takes the chat page (with the backlog rail), the board, a lane, a run log and
decisions, each in `-light` and `-dark`, named `<page>-<theme>.png`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CHROME` | found on this machine | Chrome or Chromium executable |
| `SHOTS_PORT` | `5199` | the stub's port |
| `SHOTS_OUT` | `web/shots/out` | where the images go |
| `SHOTS_SIZE` | `1440x900` | CSS pixels |
| `SHOTS_SCALE` | `2` | device scale factor |
| `SHOTS_THEMES` | `light dark` | themes to take |
| `SHOTS_NO_BUILD` | unset | set to `1` to use `web/dist` as it is |

To look at the stub in a browser: `node web/shots/stub-daemon.mjs web/dist 5199`, then
open `http://127.0.0.1:5199/`. `NODEC=1` leaves no open decisions or requests.

The stub's data is built from the current time, so ages stay believable. It covers
decisions waiting, a failed pipeline, an idle lane, runs in flight, a merge request
waiting for a merge, spend near its warning, a quota hold, the forge's poll status, and
the lane fields `last_activity`, `last_activity_by`, `mr_created`, `mr_updated`,
`pipeline_started` and `pipeline_finished`. It serves the front desk's event stream
(`/v1/desk/stream`) so the chat thread fills in as it would live. When the API changes,
update the stub with it: the web UI's types are in `web/src/api/types.ts`.
