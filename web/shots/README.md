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

It takes the chat page (with the backlog rail), the board, a lane, a run log, decisions
and the timeline, each in `-light` and `-dark`, named `<page>-<theme>.png`. Every shot
emulates `prefers-reduced-motion: reduce`, so the timeline's pulse and breathing are frozen
and a shot repeats; its static markers are drawn for that.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CHROME` | found on this machine | Chrome or Chromium executable |
| `SHOTS_PORT` | `5199` | the stub's port |
| `SHOTS_OUT` | `web/shots/out` | where the images go |
| `SHOTS_SIZE` | `1440x900` | CSS pixels |
| `SHOTS_SCALE` | `2` | device scale factor |
| `SHOTS_THEMES` | `light dark` | themes to take |
| `SHOTS_PAGES` | all | page names to take, such as `timeline board` |
| `SHOTS_NO_BUILD` | unset | set to `1` to use `web/dist` as it is |

To look at the stub in a browser: `node web/shots/stub-daemon.mjs web/dist 5199`, then
open `http://127.0.0.1:5199/`. `NODEC=1` leaves no open decisions or requests.

`BUSY=1` serves a heavy day for the timeline instead of the small one: about 25 lanes in
five repos, each with 3 to 12 runs through the working hours so far, all four agents, two
failures, a decision waiting and two answered, a request that needs routing and six that
were routed, three merges, a running bar in five lanes, three idle lanes and a spend curve
that crosses 80% of the budget. It is built from the current time and a fixed seed. For
the timeline alone, at the sizes of the merge request's shots:

```sh
BUSY=1 SHOTS_PAGES=timeline web/shots/shots.sh                            # 1440x900, light and dark
BUSY=1 SHOTS_PAGES=timeline SHOTS_SIZE=390x844 web/shots/shots.sh         # the narrow layout
BUSY=1 SHOTS_PAGES=timeline SHOTS_SIZE=1600x900 SHOTS_SCALE=2 web/shots/shots.sh   # the blog image
```

The default stub's history endpoints answer with its few runs, so the timeline has bars
without `BUSY`.

The stub's data is built from the current time, so ages stay believable. It covers
decisions waiting, a failed pipeline, an idle lane, runs in flight, a merge request
waiting for a merge, spend near its warning, a quota hold, the forge's poll status, and
the lane fields `last_activity`, `last_activity_by`, `mr_created`, `mr_updated`,
`pipeline_started` and `pipeline_finished`. It serves the front desk's event stream
(`/v1/desk/stream`) so the chat thread fills in as it would live. When the API changes,
update the stub with it: the web UI's types are in `web/src/api/types.ts`.
