# Architecture audit: SOLID review of the Go core

A read-only review of `cmd/` and `internal/` on `dev` at `cb9f321`. No code was changed.
Line numbers are for that commit and will drift. The web front end is out of scope except
where Go serves it. About 65,000 lines of Go including tests, 35 packages, one binary.

## Before you read

- **Rule conflict.** CLAUDE.md says audits do not go in this repo. This file is here
  because the lane's scope names it as the only path to change. Move it to the lane's
  report or the maintainer's private repository if you would rather not merge it.
- **Earlier attempt.** The branch `docs/architecture-audit` (closed without merging) was
  read for leads. Every claim below was re-measured on this commit; two of its claims no
  longer hold or were wrong (see "Corrections to the earlier draft").
- **Already on Next up.** Items 218 (split `client` from `daemon`) and 219 (OpenCode
  globals in `dispatch`) are the tracked refactors. Findings that overlap them are marked
  so the refactor list does not duplicate them.

## Package map and dependency direction

Measured with `go list`. Arrows mean "imports". Leaves have no internal imports.

| Layer | Packages | Imports internally |
|---|---|---|
| Leaves | `paths`, `redact`, `scope`, `work`, `version`, `webui`, `forge`, `service`, `workspace` | nothing |
| Small leaves | `autowiden` (scope), `crook` (redact), `git` (redact), `config` (none), `configedit` (config) | one or none |
| Data | `store` (none), `store/sqlite` (store, auth), `convo` (paths, redact, store), `auth` | store, paths |
| Domain | `fold` (config, crook, forge, git, scope, store, ...), `standards`, `remote`, `mergerule` (crook, git), `muster` (config, git, store, waiting) | domain and leaves |
| Engine | `dispatch` (fold, forge, crook, standards, autowiden, convo, store, ...), `watch` (dispatch, fold, forge, mergerule, muster, ...) | domain |
| Read models | `api` (fold, config, convo, store, work), `waiting` (api, dispatch, store), `desk` (api, dispatch, waiting, store) | engine and domain |
| Servers and clients | `daemon` (16 internal imports incl. api, desk, dispatch, fold, waiting), `client` (api, **daemon**, dispatch, fold, store), `chat` (api, client, desk, dispatch, store, convo) | everything below |
| Composition root | `cli` (about 25 imports), `cmd/shepherd` (cli only) | everything |

**Cycles:** none. `go list` builds, and there are no cycle-avoiding tricks.

**Layering breaks** (an arrow that points the wrong way for the layer names):

1. `client -> daemon` (`internal/client/client.go:20`, used at `:63` and `:117` for
   `daemon.ReadRuntime`, defined at `internal/daemon/daemon.go:2047`). A client depends on
   the server and so, transitively, on `desk`, `waiting`, `watch`, `muster` and `store`.
   `client` also imports `dispatch` and `fold` only to name response types
   (`client/client.go:169`, `:193`, `:206`, `:217`; `client/artifacts.go:9`;
   `client/lane_diff.go:8`). **This is item 218.**
2. `api -> fold` (`api/api.go:10`, `api/lane_files.go:18`, `api/next.go:7`,
   `api/lane_diff.go:11`, `api/lane_artifacts.go:10`). The package that is meant to hold
   the wire types also holds handlers that call the domain. Wire types and handlers share
   a package, so everything that wants a wire type imports `fold`.
3. `waiting -> dispatch` and `desk -> dispatch` (`waiting/waiting.go:19`,
   `desk/desk.go:17`, `desk/claude.go:23`, `desk/summary.go:12`), `chat -> dispatch`
   (`chat/model.go:721`). A read model and a UI reach into the largest package for small
   pure helpers (`dispatch.Unpushed` at `waiting/waiting.go:256`, `dispatch.EventReport`
   at `:341`, `dispatch.SessionDelta`, `dispatch.Today`).
4. `store/sqlite -> auth` (`store/sqlite/account.go:9`). A storage driver knows an
   authentication package; `store` itself should own the types the driver needs.
5. `convo` imports the SQLite driver directly (`convo/opencode.go:5`, `:13`) to read
   OpenCode's own database. This is a legitimate read of a foreign file, but it means
   `convo` cannot be built or tested without the driver and is a second place that opens
   SQLite.

Fan-in worth knowing: `store` is imported by 12 packages and held as the whole
`store.Store` in six structs (`daemon/daemon.go:40`, `desk/desk.go:44`, `fold/fold.go:45`,
`dispatch/runner.go:105`, `muster/muster.go:105`, `watch/watch.go:36`). `dispatch` is
imported by `cli`, `client`, `chat`, `desk`, `waiting`, `watch` and `daemon`.

## What is already good

- `core-boundary` keeps product logic out of the core. `crook`, `scope`, `redact`, `work`
  and `forge` are small and import little.
- `store` is an interface with an implementation in a separate package, so
  `CGO_ENABLED=0` cross-compiles.
- `forge.Forge` and the `dispatch.Adapter` table use small interfaces and function fields
  that match the tests' fake-CLI approach. `forge` has optional role interfaces (`Linker`,
  `ThreadReader`, `ThreadCommander`), which is interface segregation done right.
- The CLI really is a client of the daemon; only the daemon command opens the store.
- Migrations are append-only. `redact` is a leaf used at every boundary.

## SOLID findings

### Single responsibility

| Where | What | Why it matters |
|---|---|---|
| `daemon/daemon.go:39` | `Server` has 41 fields and `daemon` has 791 functions in 41 files (`daemon.go` is 2,096 lines). `Handler` (`:155`) is a 168-line route table. The struct holds the store, config, fold, runner, desk, feed hub, web sessions, keep-going, update and upgrade state, reload state and several mutexes. | A feature adds fields and methods to one type. Lock ordering is hard to see: 12 non-test files in the package declare mutexes. The recent desk-reply and auto-widen races were found one at a time in this shared state. |
| `dispatch/runner.go:96` | `Runner` has 29 fields. `start` (`:334`) is 323 lines and `watch` (`:673`) is 211. Routing, budget, drain, ship, auto-widen, diagnosis and every adapter live in one package of about 100 methods on `Runner`. | `start` mixes quota holds, concurrency, worktree and brief setup, session handling, cost and feed writes. A change to one concern needs the whole function under test. |
| `cli/mcp.go:73` | `mcpTools` is one 919-line literal. | Every lane that adds a tool edits it, so merges conflict. No test shows that each mapped command exists. |
| `chat/model.go` | `update` (`:586`, 205 lines) and `handle` (`:855`, 200 lines). | Each slash command edits one switch. Lowest value of the group. |
| `fold` | One type holds lanes, leases, tags, import (`ImportAdopt` is 160 lines, `importer.go:249`), diff (`Diff` is 189 lines, `diff.go:123`) and artifact snapshots (678 lines). | Artifact snapshots and diff do not share state with leases. |
| `waiting/waiting.go:30` | `Collect` is 180 lines that gather every kind of waiting item. | Adding a kind means editing the one function. |
| `config/config.go` | 1,353 lines holding schema, defaults, validation (`validate` 134 lines, `:1033`) and merge (146 lines, `:1185`). | Every new setting touches three places in one file. |

### Open/closed

- **The agent list is repeated.** `config.Agents` (`config/config.go:415`) is the one
  list, but the same four names are written again at `config/models.go:338`, in
  `agent != "opencode"` at `config/models.go:98`, in three MCP schema enums
  (`cli/mcp.go:204`, `:229`, `:920`), in the name check `if name == "opencode"` in
  `dispatch.AdapterFor` (`dispatch/adapters.go:472`), and in the hard-coded desk usage
  agent (`desk/desk.go:800`, `"claude"`). A fifth agent compiles with any of these missed,
  and the MCP enums would then refuse an agent the CLI accepts.
- **Merge power is a GitLab check.** `dispatch/runner.go:663` tests
  `f.Name() == "gitlab"` and passes a `GitLab` flag into `crook.Facts`
  (`crook/log.go:169`). `glab` commands are written into prompts and permission lists in
  `dispatch/adapters.go:182`, `:275`, `:319`, `:625` and 11 places in
  `dispatch/opencode.go` (for example `:263`, `:269`). GitHub support is not implemented,
  so this is the file set that will all change at once.
- **Exported mutable lists.** `config.Agents`, `PermissionModes`, `WakeModes`,
  `UpdateChannels`, `HyperlinkModes`, `LogLevels` (`config/config.go:198` to `:460`),
  `Caps`, `Periods` and `Decisions` (`config/project.go:22`, `:45`, `:61`) are plain
  slices any package can append to or reorder.

### Liskov substitution

The code has few inheritance-style hierarchies, so Liskov shows up as "callers cannot
substitute a sibling":

- Callers cannot substitute another forge because of the GitLab checks above. `forge.For`
  returns an interface, and `dispatch` then branches on its name.
- `AdapterFor` returns an `Adapter` value, but OpenCode is made by a special wrapper
  (`openCodeFor`, `dispatch/adapters.go:473`) that reads global state, so it does not
  behave like the other three in tests.
- No violation was found in the `store.Store` implementation: `store/sqlite` has a
  compile-time assertion and there is one implementation.

### Interface segregation

- **`store.Store` is a 125-method interface** (`store/store.go:856`) over workspaces,
  lanes, runs, decisions, requests, feed, settings, briefs, next-up items, topics,
  usage, web sessions, artifacts and forge links. Free functions take all of it:
  `waiting.Collect` (`waiting/waiting.go:30`), `waiting.MergedArtifactItems`
  (`waiting/artifacts.go:16`), `dispatch.Spent` (`dispatch/budget.go:134`),
  `BudgetLines` (`:226`) and `DeskHeld` (`:317`). A fake for any of them must implement
  125 methods or embed the real store. About 20 test files build a real SQLite database
  to get one (for example `waiting/waiting_test.go`, `muster/watch_test.go`).
- **`client` pulls whole packages for types** (see layering break 1).
- Good counter-example: `forge.Forge` plus its optional role interfaces.

### Dependency inversion

- **Globals that tests replace.** `dispatch/opencode.go:51` (`openCodeSettings`, which
  calls `paths.Home` and `config.Load` on every call, so it reads a file the daemon may
  have rejected or reloaded), `:64` (`openCodeIdle`), `:91` (`openCodeProbe`),
  `dispatch/drain.go:24` (`DrainGrace`), `cli/web.go:116` (`openBrowser`),
  `cli/links.go:13` (`linkForge`), `service/service.go:74` (`runCmd`),
  `autowiden/autowiden.go:74` (`DefaultProtected`). The OpenCode three are **item 219**.
  The others are the same pattern and cost less to fix with a field, but are low value.
- **`daemon.Server` seams.** The tests set exported function fields to fake pieces; this
  works but couples the test to the shape of `Server`.
- **Concrete high-level to low-level edges:** the layering breaks above (`client` to
  `daemon`, `waiting` and `desk` to `dispatch`, `store/sqlite` to `auth`).
- **Clocks and environment.** `time.Now()` is called directly 15 times in `dispatch` and
  `fold`, and `os.Getenv` appears in `dispatch`, `fold`, `desk` and `daemon`. Only fix
  where a test needs it; `fold.CommitClock` shows the pattern.

## Ranked refactors

Each row is one merge request. Ranked by value over cost. "Tracked" means already on
Next up, so no new item is needed.

| Rank | Refactor | Findings | Size | Tracked |
|---|---|---|---|---|
| 1 | Move `ReadRuntime` to `api` (or `paths`) and the response types the client names (`fold.Opened`, `fold.CloseResult`, `fold.GateRun`, `fold.Stale`, `dispatch.Shipped` and similar) into `api`; `client` stops importing `daemon`, `dispatch` and `fold` | Layering 1 | Small, mechanical | Yes, 218 |
| 2 | Inject OpenCode settings through `Opts` and the probe and idle limit through `Runner`; delete the three globals | DIP | Small | Yes, 219 |
| 3 | One agent list: build the MCP enums and the `config/models.go` set from `config.Agents`; add a test that `dispatch.AgentNames()` equals it; replace the `name == "opencode"` check with an optional `Prepare` field on `Adapter`; take the desk's agent name from its `Agent` value | OCP | Small | No |
| 4 | Split `store.Store` into role interfaces, keeping `Store` as their composition; change `waiting`, `dispatch/budget` and `watch` to take only their roles. One package per commit | ISP, DIP | Medium | No |
| 5 | Pull self-contained subsystems out of `daemon.Server` (updates and upgrade, web auth and sessions, keep-going, desk host), one per commit with no route changes | SRP | Medium to large | No |
| 6 | Move `SessionDelta`, `Today`, `Unpushed`, `EventReport`, the usage parser and the env names out of `dispatch` into a small package; `waiting`, `desk` and `chat` stop importing `dispatch` | Layering 3 | Small to medium | No |
| 7 | Split `Runner.start` into prepare, launch and record steps and extract the quota map, widen cache and held-start queue into their own types; no package move | SRP | Medium | No |
| 8 | Move the handlers that call `fold` out of `api` into `daemon` (or move wire types to a `wire` package); `api` imports no domain package | Layering 2 | Medium | No |
| 9 | Ask the forge for merge commands through a role interface (alongside `ThreadCommander`) and drop `GitLab` booleans and `glab` strings from `dispatch` and `crook` | OCP, LSP | Medium. Do it with the GitHub work, not before | No |
| 10 | Split the MCP tool table by area and add a test that every mapped command resolves | SRP | Small | No |
| 11 | Make the config lists unexported behind accessors; hang `convo`'s package-level cache on a reader; give `store/sqlite` the `auth` types through `store` | OCP, DIP | Small each | No |
| 12 | Table-driven slash commands in `chat.handle`; split `waiting.Collect` by item kind | SRP | Small each. Do when next touched | No |

Rows 1 to 3 are independent, small and low risk. Rows 4 and 5 have the largest payoff
and need the most review. Row 9 should not run ahead of the GitHub mirror work.

## Corrections to the earlier draft

- It said `store.Store` has 111 methods; it has 125 on this commit.
- It said `Server` has about 50 fields and 224 methods; it has 41 fields and 791
  functions across 41 files.
- It put `AdapterFor`'s special case at `:440` and the MCP enum at `:835`; they are at
  `dispatch/adapters.go:472` and `cli/mcp.go:920` now.
