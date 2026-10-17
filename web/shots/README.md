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

It takes every view and the detail pages, each in `-light` and `-dark`, named
`<page>-<theme>.png`: the Conversation (the thread, with work shown, the return card and a
decision swapped in), the Inbox (a merge request in the side panel and as a page, a decision
in the main body), the Board, a lane (panel and page, with its Changes, Files, Artifacts and
Runs tabs, and a closed lane), Next up (with an item blocked by another), Artifacts (a plan kept
local, an image, a committed code file, an undecided one), Files, Runs (the run panel, a run
page, a failed run and the run log), the Timeline, History, Projects and one project, Rules,
Usage, Settings and Status. `SHOTS_PAGES` lists the names. The stub holds the next-up list in
memory, so its actions work. Every shot emulates `prefers-reduced-motion: reduce`, so the
timeline's pulse and breathing are frozen and a shot repeats; its static markers are drawn for that.

Several sizes in one go: `SHOTS_SIZE="1440x900 2560x1440@1 390x844" web/shots/shots.sh` writes
`out/1440x900/`, `out/2560x1440/` and `out/390x844/`; `@1` sets that size's device scale factor.
The Inbox shots wait 16 seconds, because the page reads what finished today again only after
15 seconds once the workspace is known.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CHROME` | found on this machine | Chrome or Chromium executable |
| `SHOTS_PORT` | `5199` | the stub's port |
| `SHOTS_OUT` | `web/shots/out` | where the images go |
| `SHOTS_SIZE` | `1440x900` | CSS pixels, or several sizes separated by spaces |
| `SHOTS_SCALE` | `2` | device scale factor |
| `SHOTS_THEMES` | `light dark` | themes to take |
| `SHOTS_PAGES` | all | page names to take, such as `timeline board` |
| `SHOTS_NO_BUILD` | unset | set to `1` to use `web/dist` as it is |

To look at the stub in a browser: `node web/shots/stub-daemon.mjs web/dist 5199`, then
open `http://127.0.0.1:5199/`. `NODEC=1` leaves no open decisions or requests.

`LONG=1` makes the lane's changes, its runs, the run log and a kept plan long enough to scroll,
for the `*-scrolled` pages (`lane-changes-scrolled`, `lane-runs-scrolled`, `run-log-scrolled`,
`mr-page-scrolled`, `artifact-view-scrolled`, `artifact-page-scrolled`, `run-page-scrolled`, `decision-page-scrolled`, `files-scrolled`). Each scrolls the
window and any column that scrolls itself by 600 px before the shot, so the page header must still be
whole at the top:

```sh
LONG=1 SHOTS_PAGES="lane-changes lane-changes-scrolled" web/shots/shots.sh
```

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
the acme sample (repos `acme-api`, `acme-web` and `acme-docs` on `git.example`, one project
of the first two, closed and merged lanes, stored artifacts with a generated PNG, a files tree
per lane, a work matrix per project, usage by model and lane), decisions waiting, a failed pipeline, an idle lane, runs in flight, a merge request
waiting for a merge, spend near its warning, a quota hold, the forge's poll status, and
the lane fields `last_activity`, `last_activity_by`, `mr_created`, `mr_updated`,
`pipeline_started` and `pipeline_finished`. It serves the front desk's event stream
(`/v1/desk/stream`) so the chat thread fills in as it would live. When the API changes,
update the stub with it: the web UI's types are in `web/src/api/types.ts`.
