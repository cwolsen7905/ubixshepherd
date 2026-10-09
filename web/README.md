# Shepherd web UI

A browser client of the Shepherd daemon's HTTP API, like the CLI and `shepherd chat`. It
shows the board (lanes grouped by what they need from you, with the terminal dock's rules),
a lane's runs and timeline, the open decisions, and any run's log, live. The one thing it
changes is a decision's answer, and only from your click.

Vite, React 19 and TypeScript (strict). No router, no state library, no CSS framework.

## Run it

You need Node 22 or later and a running daemon (`shepherd daemon start`, or any `shepherd`
command, which starts one).

```sh
cd web
npm install
npm run dev      # http://localhost:5178
npm run build    # the static app in web/dist
npm run check    # typecheck, lint and tests
```

## How it talks to the daemon

The app calls the API by root-relative URL (`/v1/lanes`, `/v1/feed`, ...), so it works
unchanged once the daemon serves the built app on its own address. It never sees a token.

In development, `daemon-proxy.ts` is a dev-server middleware that forwards `/v1` to the
daemon. It reads the daemon's address and token from its runtime file, `daemon.json` in
`SHEPHERD_HOME` (`~/.shepherd` by default), on every request, so a restarted daemon is
followed, and adds the token to the forwarded request only. Nothing prints or logs it.

Updates come from polling: the feed with its cursor every 1.5 s (10 s while the tab is
hidden), which also triggers a reload of the lists; the lists themselves every 5 s (30 s
hidden). A server-sent event stream will replace the feed poll.

## Layout

| Path | What |
|---|---|
| `src/api/` | The API's JSON types (from `internal/api` and `internal/store`) and the client |
| `src/state/live.ts` | The one live view of the daemon: lists, the feed window, polling, what you have seen |
| `src/model/` | Pure logic: the board's grouping (`board.ts`, the dock's rules), event marks, feed refs, the log parser |
| `src/pages/` | Board, Lane, Decisions, Log |
| `src/styles/` | `theme.css` (tokens, light and dark) and `app.css` |

## Keys

`j`/`k` move on the board, `Enter` opens, `g b` and `g d` go to the board and decisions,
`c` marks everything done as seen, `/` searches a run log (`Enter`, `n`, `N` step through
matches), `t` expands tool calls, `f` follows the log, `?` lists the keys.
