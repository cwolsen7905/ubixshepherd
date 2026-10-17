# Changelog

All notable changes to uBixShepherd are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows
[Semantic Versioning](https://semver.org/) as described in
[docs/VERSIONING.md](docs/VERSIONING.md). While Shepherd is in beta, a change to a public
interface (the CLI, the config file schema, the HTTP API, the MCP tools or the hook
protocol) is listed under **Interface changes** in the release that makes it.

## [Unreleased]

## [0.5.0-beta.1] - 2026-10-14

A beta of uBixShepherd with a keep-going mode that lets the front desk carry on with
what is already approved, a Waiting on you list that now also shows blocked lanes and
unapplied scope widenings, tracked questions from the desk to the person, review threads
handed back to a lane's agent, GitLab links in reports, and a daemon upgrade from the
update notice. Nothing is removed or changed incompatibly.

### Interface changes

- Keep-going mode: new routes `GET /v1/keep-going?workspace_id=N` and `POST /v1/keep-going`,
  the command `shepherd keep-going on [--cap USD] [--runs N] [--idle HOURS]`, `off` and
  `status`, the MCP tool `keep_going` (`on`, `off`, `status`) and `/keep-going on|off` in
  `shepherd chat`. A new `keep_going` section in `config.yaml` takes `cap` (default 5
  dollars), `max_runs` (2), `idle_hours` (4), `day_end` (local midnight, `HH:MM`) and
  `lane_starts` (3 a lane a day). A person-started turn, the CLI and the web UI are not
  limited by it; an event-turn desk that opens a lane, starts or continues a run or takes
  a fix is refused with a 403 and its reason when the mode is off or a limit is reached.
  An appended store migration adds the mode's row and per-lane start counts.
- `GET /v1/waiting` gains kinds `blocked` (an open lane whose last run reported blocked,
  with no run going) and `scope` (an answered scope-widening decision the lane's scope
  does not yet cover), both with action `open`. An approved Next-up item whose lane closed
  without a merge is listed too.
- New routes `GET|POST /v1/questions`, `POST /v1/questions/{id}/answer` and `/skip`, with
  MCP tools `question_open`, `question_answer` and `question_list` and the command
  `shepherd question`. `POST /v1/desk/turn` takes `reply_to`. `GET /v1/waiting` gains kind
  `question` and `counts.questions`.
- New route `POST /v1/update/upgrade`, an `upgrade` object in `GET /v1/update`, the
  commands `shepherd update upgrade [--now]` and `/update upgrade [now]`, and config key
  `update.auto` (default `false`).
- `next_add` from the front desk now stores source `chat`, and takes `propose` (with
  `shepherd next add --propose`, desk only) to put an item up for approval as before.
- New config keys `watch.fix_tries` (default 2, `0` off) and `watch.review_threads`
  (default on), under `defaults` or a repo. The fix budget is now shared by failed
  pipelines and review threads.
- New command `shepherd links` and MCP tool `links_thread`, which turn a discussion id
  into its note URL.
- Appended store migrations add `lane_forge.threads_seen` and relabel planned `desk`
  items as `chat`. They apply when the daemon starts, and a binary refuses a store
  migrated by a newer one.

### Added

- **Keep-going mode lets the front desk carry on with what is already approved.** Until
  now every `[Shepherd]` event turn told the desk not to start work it had not been asked
  for, so it reported and waited even when the next step was obvious. With the mode on,
  the desk may start the next approved Next-up item, continue an approved next step and
  follow up a failed pipeline, within limits the daemon enforces and not the prompt: a
  spend cap, concurrent runs, idle time, end of day and starts per lane per day. It
  turns itself off when a limit is reached and records why. It never answers a decision,
  approves, merges, tags or deploys, and only the person turns it on. The mode is per
  workspace, lives in the store and survives a restart. The desk's "do not start new
  work" sentence now comes from one function that reads the flag. The web switch and the
  `keep_going` feed kind are not built yet. See [docs/keep-going.md](docs/keep-going.md).
- **Waiting on you lists blocked lanes and unapplied scope widenings.** A lane whose last
  run reported blocked, an answered scope-widening decision the lane's scope does not
  cover, and an approved Next-up item whose lane closed without a merge now wait on the
  person. A merge request the forge merged never lists, whatever its pipeline status. See
  [docs/waiting-on-you.md](docs/waiting-on-you.md).
- Clickable GitLab links in reports: agents' briefs and the desk's instruction require links for merge requests, pipelines, jobs and review threads (a thread links to its first note), Shepherd's own watcher messages emit them, and `shepherd links` (MCP tool `links_thread`) turns a discussion id into its note URL. See [docs/links.md](docs/links.md).
- **Shepherd sends unresolved review threads back to the lane's agent, and the fix budget
  is configurable.** The watcher reads an open lane merge request's unresolved threads
  and continues the agent with each thread's id, file and line and text, once per thread
  (remembered across restarts). It shares one per-request budget with failed pipelines,
  now `watch.fix_tries` (default 2, `0` off) under `defaults` or a repo, with
  `watch.review_threads: false` to stop thread handoff. A failure of the `require-approval`
  job alone is never handed to an agent: it waits for a human reviewer. A migration adds
  `lane_forge.threads_seen`. See the README's "Watching the forge".
- **Work the desk registers from chat is marked as coming from chat and does not wait on
  you.** `next_add` from the front desk now stores source `chat`: the item shows as planned
  with a "chat" badge in Next up and is left out of "Waiting on you". The new `propose`
  option on `next_add` (and `shepherd next add --propose`, desk only) puts an item up for
  approval as before. A migration relabels planned `desk` items already on the list as
  `chat`. See [docs/backlog-next.md](docs/backlog-next.md).
- **The front desk's questions to you are tracked and cannot be missed.** The desk marks a
  question with the new `question_open` tool when a reply asks you something; it then waits
  in the "Waiting on you" list as kind `question`, in the header, the dock and the desk's
  `[Waiting on you (N)]` line. The header counter has up and down arrows that step through
  everything waiting and scroll to and highlight it in the thread. A question's message
  shows "Reply here" (an inline reply linked to the question) and "Skip". The desk marks
  one answered with `question_answer` when a main-chat reply plainly covers it. See
  [docs/desk-questions.md](docs/desk-questions.md). **Interface changes:** new routes
  `GET|POST /v1/questions`, `POST /v1/questions/{id}/answer` and `/skip`, `reply_to` on
  `POST /v1/desk/turn`, kind `question` and `counts.questions` in `GET /v1/waiting`, MCP
  tools `question_open`, `question_answer` and `question_list`, and `shepherd question`.
- **Fenced code blocks in the web chat have a copy button.** Each block in a desk or agent
  message gets an icon button in its top-right corner that copies the block's text exactly,
  over plain http too, and shows a check for a moment. Inline code is unchanged.
- **The update notice can upgrade the daemon in one click, and the daemon can do it by
  itself.** The notice in the web UI gains an Upgrade button, `shepherd update upgrade
  [--now]` and `/update upgrade [now]` in `shepherd chat` do the same. The daemon downloads
  the release archive for its platform, checks it against the release's `SHA256SUMS`,
  proves the new binary runs, keeps the old one as `shepherd.previous`, swaps it in, and
  restarts onto it at the first moment no agent is running (or at once with `--now`,
  "Restart now", which interrupts them). A new binary that does not stay up is replaced
  by the previous one on the next start. Homebrew, `go install` and source installs are
  not replaced: the notice says what to run. New opt-in setting `update.auto` (default
  `false`) installs a newer release without a click. **Interface changes:** new route
  `POST /v1/update/upgrade`, an `upgrade` object in `GET /v1/update`, config key
  `update.auto`. See [docs/update-check.md](docs/update-check.md#upgrading).

- **Next-up items that wait on the person's approval are counted where decisions are.**
  An item the desk or an agent proposed, still planned and not started, no longer sits on
  the list unseen: the terminal dock's counts row shows "N Next-up to approve (/next)", the
  web header's Next up link carries a count badge, and the summary that seeds a new desk
  session says "N Next-up items wait on your approval" with each item, after the waiting
  decisions. `shepherd chat` gains `/next`, which lists them. Nothing is added to the API:
  the count comes from the existing `GET /v1/next`.

### Fixed

- **The web Backlog and board no longer hide a lane whose open merge request has no
  pipeline.** Only a queued or running pipeline counts as waiting on the forge. A merge
  request with no pipeline (a repo whose CI skips the branch), or with a canceled, skipped
  or unknown one, now shows under "To review" so the person sees it is waiting on them.

## [0.4.0-beta.1] - 2026-10-12

A beta of uBixShepherd that shows each lane's gate result in the web UI, files the
person's turns under topics, and lets the front desk widen a lane's scope once the person
has approved it. No interface is removed or changed incompatibly.

### Changed

- **The daemon files each of the person's turns under a topic before the front desk sees
  it.** In order: the topic the person wrote from in the web thread (the web sends it as
  `topic_id` on `POST /v1/desk/turn`); else the one open topic of a lane, run, decision,
  request or merge request the message names (`run 12`, `decision 4`, `!158`, a lane's
  name), and none when the things named are in different topics; else the topic of the
  person's last turn. Cleared topics are never chosen. The turn's header tells the desk
  which, and not to file it again. The desk can still re-file the turn with `topic_set`
  when the person clearly changes the subject.
- **A hosted OpenCode run's cost is shown as an estimate.** OpenCode prices a step from its
  own table, and OpenRouter's billed `usage.cost` never reaches its events, so the figure
  can be several times low. Runs and usage records gain `cost_estimated` (an appended
  migration, which also marks earlier hosted OpenCode runs), the run history and usage
  totals say how much of their dollars is an estimate (`estimated_usd`), and the web shows
  `~$0.05` with a tooltip, "cost unknown" for a hosted run that reported none and "local, no
  cost" for a local model. The figure itself is unchanged.
- **The front desk files its turns under topics without being told their ids.** `topic_set`
  with only an `id`, or a new `title`, files the turn in progress: the daemon knows the turn
  the desk's token was minted for. The desk may file only its own turn. A turn Shepherd
  starts for a run, decision or request already in a topic is filed under that topic, and
  the desk is told which; a lane with no link takes the topic of its next-up item or of a
  run tied to it. `topic_set` takes a lane by name (with `repo`) as well as by id, and `lane
  open` and `lane list` show the lane id. No schema change.

### Added

- **`lane_widen` and `shepherd lane widen`: the desk widens a lane's scope once the person
   has approved it in a decision.** The tool adds globs to an open lane on the strength of an
   answered decision raised by one of the lane's runs, only in a turn the person started (the
   rule `decision_answer` follows). A glob another open lane holds is refused, naming that
   lane, as for `lane_open`. Each change is logged and put in the feed. This adds the route
   `POST /v1/lanes/{id}/widen` and the MCP tool `lane_widen`; `lane scope` is unchanged.
- **Settings: the Web section with `editor_url`.** `web.editor_url` now appears in the
   Settings form as "Editor link", a URL template where `{path}` is the file's absolute path
   and `{line}` is optional. The default is `vscode://file/{path}`. Validated at load,
   following the same scheme rules the page already enforces.
- **A gate badge in the web UI, and a run-narrowed Changes section.** The lane page, the
  board and a project's open lanes show the lane's gate record (passed, failed, stale or
  not run), with its commit, time and command on hover. The run page gains a Changes
  section narrowed to the run with `run=<id>`; it shows the worktree's uncommitted work
  only for the lane's latest run and marks files on the run's outside list. This is the
  web step of `docs/lane-diff.md`; the daemon already served the badge.
- **The Help dialog lists `g t`,** the key for the timeline page.

### Fixed

- A lane with a running run is never judged finished. The review looked only at git state,
  so a clean lane with nothing unlanded read as finished while an agent run, such as a
  continuation of a stopped run, was still running in it. Such a lane is now judged live.
- OpenCode agents can run a repo's web tests and build: `npm *` is allowed. The repo's
  gate is allowed as written, with no wildcard, so `make check build` no longer matches
  `make check`.

### Interface changes

- New MCP tool `lane_widen` and command `shepherd lane widen`, on the new route
  `POST /v1/lanes/{id}/widen`. It is not open to an agent's worker token.
- `POST /v1/desk/turn` takes an optional `topic_id`. `topic_set` takes a lane by name (with
  `repo`) as well as by id, and files the turn in progress when given only an `id` or a
  new `title`. `lane open` and `lane list` show the lane id.
- Runs and usage records gain `cost_estimated`, and run history and usage totals gain
  `estimated_usd`. One appended store migration, which applies when the daemon starts; a
  binary refuses a store migrated by a newer one.
- `web.editor_url` is unchanged; it is now editable in the Settings form.

## [0.3.0-beta.1] - 2026-10-11

A beta of uBixShepherd that adds a Next up list for planned work, `lane diff` and a
Changes tab on the lane page, a timeline page in the web UI, and `shepherd repo
add/remove/list/scan`. No interface is removed or changed incompatibly.

### Added

- **A next-up list: `shepherd next` and the `next_list`, `next_add` and `next_update`
  tools.** Work that is planned but not being acted on now lives in the daemon's store
  (`next_items`, an appended migration), in the person's order: approved, then planned,
  then deferred. `shepherd next` lists, adds, shows, approves, defers, drops, finishes,
  moves and links items, and `shepherd next seed` adds the items `docs/backlog-next.md`
  lists. `lane open --item N` (and the `item` argument of `lane_open`) links an item to the
  lane it opens, or a lane named as the item's hint links it when exactly one matches;
  the item reads as started while the lane is open and done when its merge request
  merges. Approving, deferring, dropping, finishing and reordering are the person's: the
  daemon refuses them from the front desk outside a turn the person started, and an agent's
  token has no route. The API is `GET/POST /v1/next` and `GET/PATCH /v1/next/{id}`. The
  work-matrix read-through is a later merge request.
- **A Next up page and rail view in the web UI.** `#/next` (nav entry **Next up**, key
  `g n`) lists the items in order: approved, planned and deferred, with done and dropped
  behind a toggle. Each row shows its state, plan doc, repo, lane and merge request, and
  the first approved item with no lane carries the **next** marker. The person approves,
  defers, drops, restores, moves up or down, links to an open lane and adds items from
  the page; a refusal from the daemon shows as it came. **Start** puts "open a lane for
  item N" in the chat draft and starts nothing. The chat page's backlog rail gains a
  **Now / Next up** toggle, remembered in the browser: **Now** is the rail as it was, and
  **Next up** shows the top five approved items with Start and a link to the page.
- **`shepherd lane diff` and the `lane_diff` tool.** One read of what a lane holds against
  its base: the commits, the committed changes, uncommitted changes in the worktree and
  untracked files, each file marked inside or outside the lane's scope. `--stat` is the
  default, `--files` adds hunk headers, `--file <path>` reads one patch, `--json` prints the
  answer the daemon serves at `GET /v1/lanes/{id}/diff`. It is bounded (30 commits, 200
  files, 64 KiB per file, 256 KiB in all), redacted before it is cut, and open to the
  desk and browser sessions but not to an agent's worker token. The web Changes tab, the
  gate badge and the run view are later steps of `docs/lane-diff.md`.
- **A timeline in the web UI.** `#/timeline` (key `g t`, and a Timeline entry in the nav)
  shows the day's work at once: a track per lane grouped by repo, a bar per run coloured
  by its agent, running bars that grow with the clock and end in a pulsing cap, a lantern
  on every decision or request waiting on you, a tick for each merge, a notch on each
  failure, and a spend band against the daily budget. It has Today, Last 24 hours and Last
  7 days, an agent filter in the legend, hover and keyboard-focus details, click-through to
  the run, lane or decision, and a table view of the same runs. It is built from the live
  snapshot and the existing history endpoints, with no daemon change; the agent colours
  were checked with the dataviz validator, and the page follows the dark theme, reduced
  motion, forced colors and the narrow layout. The design is `docs/swarm-timeline.md`.
- **`shepherd repo add`, `remove`, `list` and `scan`, and `init --prune`.** Managing a repo
  no longer needs `init`. A removed repo is marked, not deleted, so its lanes, runs and
  history stay readable, and adding it again restores the same row. `init --only` now says
  it only adds and lists what it left managed; `--prune` unmanages the unlisted repos,
  refusing while a lane is open. The daemon serves them at `GET`/`POST /v1/repos` and
  `POST /v1/repos/{id}/unmanage`, with no restart.
- **A Changes tab on the web lane page.** It reads `GET /v1/lanes/{id}/diff` and opens
  each file's patch on demand. `web.editor_url` (default `vscode://file/{path}`) turns each
  file into an editor link; an empty value hides the links.
- **Gate results per commit.** Each Shepherd gate run (the end-of-run push and `lane ship`)
  is recorded with its commit, command, outcome and time (`gate_results`, an appended
  migration), and a run over uncommitted changes is skipped. Lane views and the lane diff
  carry a gate badge: passed, failed, stale or not run. The diff read takes a run, for
  that run's own start-to-end range (`lane diff --run`, `run diff <id>`, the `run`
  argument of `lane_diff`).
- **Web actions reach the front desk.** Answering a decision, stopping, continuing or
  starting a run, closing a lane, routing or closing a request, and taking or dismissing
  a fix from the web page append an action event. It shows in the thread, the history and
  other tabs, starts no turn, is redacted, and heads the desk's next turn.
- The web stub for screenshots (`web/shots`) serves `BUSY=1`, a heavy day, answers its
  history endpoints with its runs, and takes `SHOTS_PAGES` to choose pages. Shots now
  emulate reduced motion.

### Interface changes

- New commands: `shepherd next` (list, add, show, approve, defer, drop, finish, move,
  link, seed), `shepherd lane diff` (`--stat`, `--files`, `--file`, `--run`, `--json`),
  `shepherd run diff <id>`, `shepherd repo add`, `remove`, `list` and `scan`, and
  `lane open --item N`. `init --only` only adds; `--prune` unmanages the unlisted repos.
- New MCP tools: `next_list`, `next_add`, `next_update` and `lane_diff`; `lane_open`
  takes an `item` argument. `lane_diff` is not open to an agent's worker token.
- New endpoints: `GET`/`POST /v1/next`, `GET`/`PATCH /v1/next/{id}`,
  `GET /v1/lanes/{id}/diff`, `GET`/`POST /v1/repos` and `POST /v1/repos/{id}/unmanage`.
  Lane views gain a `gate` badge; the feed has action events.
- New config key `web.editor_url`, validated when the config loads.
- Two appended store migrations (`next_items`, `gate_results`); they apply when the
  daemon starts, and a binary refuses a store migrated by a newer one.

### Fixed

- OpenCode's hosted-model cost is labelled as OpenCode's own estimate, and a hosted model
  no longer gets the local endpoint's context window.
- Chat messages in the web UI use the thread column's width, and the backlog rail takes
  about 35% of the width, 18 to 30rem.

### Changed

- The docs explain the front desk and its Claude Code requirement.

## [0.2.0-beta.1] - 2026-10-11

A beta of uBixShepherd with a web UI served by the daemon, projects, standards packs,
usage stats, run diagnoses, an OpenCode agent and a front desk the daemon runs. This is
the first release published to GitHub. The `0.1.0-beta.1` section below was prepared but
never tagged, so everything it lists is part of this release too.

### Added

- **Projects.** `projects:` in `config.yaml` groups repos (a repo is in at most one
  project) with a daily or monthly budget marked `soft` or `hard`, local build caches
  and intake rules. Shepherd rolls spend up per project, holds or warns at the caps,
  and layers a project's approved brief and its caches into the text agents are given.
  A project's brief is drafted and then approved by the person. The daemon serves them
  at `/v1/projects`. A repo may be named in a project without its own `repos:` entry.
  See `docs/projects.md`.
- **The web UI grows pages.** The chat page follows the front desk with a composer,
  typed rows and decisions answered inline, and keeps a half-typed message across page
  changes. Beside it, a backlog rail lists what waits on the person or has gone quiet,
  most urgent first: needs you, broken, idle, working and to review, with today's spend
  against the budget, the forge poll's health and any quota hold in its header. Rows
  carry their age and MR and pipeline badge, dated from the lane's last activity and the
  forge's own times, and an "in thread" jump. The other pages are a Projects view (and
  a Board grouped by project, with a Work section from `docs/WORK.md`), Runs and
  history, a Usage page, a lane's whole Conversation, a Settings tab and a daemon status
  popover on the connection dot. See `docs/web-chat.md`, `docs/web-projects.md`,
  `docs/web-settings.md` and `docs/web-daemon-status.md`.
- **Settings in the web UI.** The Settings tab reads and edits `config.yaml` in place:
  per-repo overrides against the defaults, projects (add, edit, delete), a diff before
  every save, validation beside the field, a `config.yaml.bak.<time>` backup (the last
  10) and a reload of the daemon on save. It refuses to save over a file that changed
  on disk, and risky changes ask to be confirmed. A browser session may save only when
  `shepherd web --allow-settings` signed it in; otherwise the tab is read-only. See
  `docs/web-settings.md`.
- **Standards packs.** A pack is a folder of markdown rules, each with a level
  (`must` or `should`), an optional check command, `applies_to` globs and `when` tags.
  The rules that apply to a lane go into the one brief every agent gets, `plan_first`
  is part of it, and a `must` rule's check runs before a push. A repo's own standards
  win. Repo profiles gain `standards`, `standards_docs`, `standards_pack_dir` and
  `standards_when`; `shepherd standards list`, `show` and `init` inspect them, and an
  example house pack is written on first start. See `docs/standards-packs.md`.
- **Usage stats.** Each run and each front desk turn records its tokens, its largest
  context and how often the CLI compacted. `shepherd stats` (`--days`, `--from`, `--to`,
  `--json`), `GET /v1/usage`, the `usage_stats` tool, the web Usage page and a usage
  line in `shepherd run show` answer whether a larger context window is worth it.
  Claude Code and OpenCode report them; Copilot and Cursor record cost only. See
  `docs/usage-stats.md`.
- **Run diagnoses and suggested fixes.** A run that ends badly, or whose work cannot be
  pushed (gate, standards or commit rules failed, out of scope, push or merge refused,
  out of quota, hung, no change), gets a deterministic one-line diagnosis with its
  evidence and suggested fixes, shown in the feed, the CLI and the web UI. A fix runs
  an existing operation; the ones that widen what an agent may do are left to the
  person. The web UI and chat show what a running run is doing now. See
  `docs/run-failures.md`.
- **Dismiss a failed run.** `shepherd run dismiss` and `run undismiss` (and the
  `run_dismiss` and `run_undismiss` tools) take a failed or stopped run off the board
  without hiding it from history.
- **The OpenCode agent.** `shepherd lane run --agent opencode` runs OpenCode headless
  for small mechanical edits on a local model. `opencode.bin`, `opencode.endpoint`,
  `opencode.idle_timeout`, `opencode.context` and `opencode.output` configure it; a run
  that changes nothing or hangs fails with a diagnosis. Not for tests in a typed
  codebase. See `docs/opencode.md`.
- **Front desk session rotation.** The daemon's front desk ends its session past
  `desk.rotate_tokens` (default 150000) or `desk.rotate_cost`, and starts the next
  seeded with a summary Shepherd builds from its own records (`desk.summary_model`,
  `desk.summary_chars`, `desk.summary_turns`). What each tool returns to the desk is
  capped at `desk.tool_output_chars` (default 4000). A `desk_rotated` item appears in the
  feed. See `docs/desk.md`.
- **A daemon on another machine.** A named host in `~/.shepherd/hosts.yaml` is reached
  over an SSH tunnel with `--host NAME` (or `SHEPHERD_HOST`), with `--workspace` and
  `--repo` to choose what to act on; `shepherd host list` and `host check` list and
  test the hosts. The daemon stays loopback-only.
- **Chat conveniences.** Links in `shepherd chat` are clickable OSC 8 hyperlinks
  (`chat.hyperlinks`, also in Settings), and `!command` runs a shell command in the chat
  client, never by the front desk.
- **A minimum git.** The daemon refuses to start on git older than 2.31 and says so, and
  CI runs the tests on Ubuntu 22.04 (git 2.34).
- **OpenCode provider timeouts.** `opencode.request_timeout` (default 60 minutes, at
  least 1 minute, or `off`) and `opencode.chunk_timeout` (default 10 minutes, at least
  10 seconds) set the per-request and stream-chunk limits Shepherd writes when
  `opencode.endpoint` is set. These defaults raise the previous hard-coded 15 minute
  request limit and 2 minute chunk limit. `off` disables the request limit. The chunk
  limit cannot be turned off. See `docs/opencode.md`.
- **Scoped tokens.** Besides the operator token in `daemon.json`, the daemon mints a
  worker token for each agent run, passed to the agent as `SHEPHERD_TOKEN` with the API
  address in `SHEPHERD_URL`, and revoked when the run ends or the daemon restarts. A
  worker token may only report, ask the person, ask another lane, reserve and release
  its own lane's tags, read its own run and run the pre-push check. A desk role, for a
  front desk the daemon runs, gets the operator tools but may answer a decision only in
  a turn the person started.
- **The front desk in the daemon.** The daemon runs a front desk of its own, one
  conversation per workspace kept in the store, that clients follow instead of starting
  their own: `POST /v1/desk/turn`, `GET /v1/desk/stream` (server-sent events, resumable
  by sequence number), `GET /v1/desk/history`, `GET /v1/desk/status`,
  `POST /v1/desk/interrupt` and `POST /v1/desk/new`. Swarm events wake it on its own
  as the new `desk.wake` setting says (`attached`, the default, `always` or `never`),
  as one digest when nobody was attached. A turn it takes on its own cannot answer a
  decision. Each turn's own cost is recorded as `desk` spend. `shepherd chat` keeps its
  own desk for now.
- **A feed stream.** `GET /v1/feed/stream` sends the feed as server-sent events, each
  item with its event kind, resumable by id or `Last-Event-ID`, with a heartbeat every
  15 seconds and a limit on subscribers. `GET /v1/feed` is unchanged.
- **The web UI, served by the daemon.** The binary embeds the build of `web/` and the
  daemon serves it at its own address; `make build` builds it first when Node is
  installed, and a binary built without it serves a page saying how to. `shepherd web`
  signs a browser in: it asks the daemon for a link that works once, for a minute, and
  opens it (`--print` prints it instead; `--sign-out-all` ends every browser session).
  Opening the link sets a session cookie (HttpOnly, SameSite=Strict, for `/v1` only,
  12 hours, kept in the store so a daemon restart does not sign you out), so the
  browser never sees the operator token.
  A browser session has a new `web` role: the front desk's routes, the front desk
  conversation and answering decisions, but not the daemon's setup or minting links.
  Remote use is an SSH tunnel to the daemon's loopback port. See `web/README.md`.
- **A Work section on the project page.** Each repo may keep `docs/WORK.md`: a `## Work`
  heading and one table with the columns `ID | Area | Item | Status | Lane | PR | Notes`.
  The daemon reads it from the repo's base branch as of the last fetch (`origin/<base>`,
  not the working tree), caches it by blob hash, and serves it at
  `GET /v1/projects/{name}/work` (and `GET /v1/unassigned/work`). The web UI groups the
  rows by status, links each to its lane and PR or MR, and flags drift: an open lane no
  row names, a `doing` row whose lane is closed or unknown, and a repo with no file.
  Rows the parser cannot read are reported as warnings. See `docs/work-matrix.md`.
- **A model policy** in `config.yaml` (per-agent catalogue with use-for notes, default
  model and effort, project and repo deny and allow rules, shown by `shepherd models` and
  the `models_list` tool) and a per-run effort for `lane run`, `run continue`, `lane_run`
  and `run_continue`; the front desk chooses model and effort from the permitted list.
- **An install script** (`bin/install.sh`), `docs/INSTALL.md` with per-OS steps, and a
  release workflow that pins Node 22 and refuses to publish without the embedded web UI
  or any of the six archives.

### Interface changes

- New endpoints for projects (`GET /v1/projects`, `GET /v1/projects/{name}`, `GET
  /v1/unassigned`, a brief's draft and approve routes), config (`GET`/`PUT /v1/config`
  and `POST /v1/config/preview`), history (`GET /v1/history/lanes` and
  `/v1/history/runs`), `GET /v1/lanes/{id}/conversation`, `GET /v1/usage`, a run's
  diagnoses and fix (`/v1/runs/{id}/diagnoses`, `/v1/runs/{id}/fix`) and dismissal
  (`/v1/runs/{id}/dismiss`, `/undismiss`), and `GET /v1/feed/stream`.
  `GET /v1/status?detail=1` adds a `detail` object; lane views gain last activity and
  forge MR and pipeline times; the feed has new kinds (`desk_rotated`, run diagnoses).
- New commands: `shepherd standards`, `shepherd stats`, `shepherd host`, `shepherd
  models`, `run dismiss` and `run undismiss`, and the global `--host`, `--workspace`
  and `--repo`; `shepherd web` gains `--allow-settings`. New MCP tools: `usage_stats`,
  `models_list`, `run_dismiss`, `run_undismiss`.
- New config keys: `projects`, `standards`, `standards_docs`, `standards_pack_dir`,
  `standards_when`, `desk.rotate_tokens`, `desk.rotate_cost`, `desk.summary_model`,
  `desk.summary_chars`, `desk.summary_turns`, `desk.tool_output_chars`, `desk.wake`,
  `chat.hyperlinks`, and the `opencode` block (`bin`, `endpoint`, `idle_timeout`,
  `context`, `output`, `request_timeout`, `chunk_timeout`), with the model policy
  keys. Absent keys keep their defaults. The daemon refuses to start on git older than
  2.31.
- New endpoints `GET /v1/projects/{name}/work` and `GET /v1/unassigned/work`, and the
  client call `ProjectWork`.
- New optional config keys `opencode.request_timeout` and `opencode.chunk_timeout`.
  Absent keys use 60 minutes and 10 minutes, raising the previous hard-coded 15 minute
  request limit and 2 minute chunk limit.
- The HTTP API answers 403 to a scoped token calling an endpoint its role does not
  allow. The operator token is unchanged and may call every endpoint.
- `shepherd worker` commands and `shepherd mcp`, when started with `SHEPHERD_TOKEN`
  set, call the daemon at `SHEPHERD_URL` with that token and no longer read
  `daemon.json`. A worker command with no run token refuses instead of finding its run
  from the lane.
- New endpoints: `POST /v1/web/signin` (operator: a one-time sign-in link),
  `GET /v1/web/signin?code=` (the link: sets the session cookie and redirects to `/`),
  `GET /v1/web/session` (the caller's role, and a browser session's CSRF token and
  expiry), `POST /v1/web/signout` and `DELETE /v1/web/sessions` (operator). Paths
  outside `/v1` now serve the web UI. New command: `shepherd web`.
- An unknown path under `/v1` is answered with the API's JSON error (404), not Go's
  plain text; a request to a known path with the wrong method is now that 404 too,
  where it was a 405.
- Every request must name a loopback host (`localhost`, `127.0.0.0/8`, `::1`, any port)
  in its `Host` header; anything else is answered 421, against DNS rebinding. A request
  with a token that carries an `Origin` other than the daemon's own is answered 403.
- A request with the session cookie and no token must come from the daemon's own
  origin (`Origin` and `Sec-Fetch-Site` are checked) and, if it changes anything, carry
  `Origin` and the session's CSRF token in `X-Shepherd-CSRF`. The daemon sends no CORS
  headers.
- The front desk endpoints (`/v1/desk/*`) accept a browser session as well as the
  operator token.
- A request with no token and no session is answered 401 with "not signed in: run
  shepherd web for a sign-in link".

### Fixed

- **A rebased lane's push is judged on its own changes.** Scope was checked against the
  remote's old tip, so after a rebase and force push the base's own files were reported
  as outside the lane's scope; it is now checked from the merge base with the base
  branch.
- **A daemon start from inside an agent run is refused** (`--force-from-run` overrides),
  and per-run and push-block variables are scrubbed from the environment the daemon
  starts with, so a daemon started by an agent no longer carries that run's settings.
- **`daemon restart` and `install` check the config first,** so a broken file replaces
  nothing; a retired key loads with a warning instead of failing.
- **Chat records each turn's own cost,** not the session's running total.
- **The web UI shows a lost daemon connection and reconnects by itself.**
- **OpenCode no longer redirects a hosted provider to the local endpoint.** With
  `opencode.endpoint` set, a model such as `openrouter/qwen/qwen3-coder` used to make
  Shepherd define an `openrouter` provider at the local host. Now only a bare model name
  or `local/<model>` runs on the endpoint; any other `provider/model` is passed to
  OpenCode as written and uses its own provider and login. A prefix other than `local/`
  no longer names a local provider. The run's closing log line reports a priced
  provider's cost instead of always saying no cost. See `docs/opencode.md`.
- `shepherd daemon reload` no longer shuts the daemon down: the daemon treats SIGHUP only
  as a config reload, so a reload no longer interrupts runs or triggers a service restart.
- Agents no longer inherit `SHEPHERD_CLIENT` from the daemon. A daemon started from
  the front desk's tools passed `desk` on to every agent, so lanes an agent opened were
  recorded as opened by the desk.

### Known limits

- **GitLab is the only forge for lanes.** Merge request polling, merge closure, pipeline
  handoffs and merge arming use GitLab through `glab`. GitHub is the release mirror and
  publishing home; GitHub mirror and publication proofs are not built.
- **macOS and Linux are tested; Windows is untested and experimental.** It builds in CI
  and its archives are published, but it has no service-manager integration and does
  not support SIGHUP reload.
- **Not built yet:** workspace-wide leases (scopes are per repository), typed and
  cross-repo work orders, triage and `verified-on:<env>` proofs.
- **A beta.** Public interfaces may change between releases, and there has been no
  independent security review. See
  https://github.com/ubixsys/ubixshepherd/blob/main/docs/VERSIONING.md.
- **Scoped tokens are not isolation.** They stop accidents and tool misuse, but an
  agent running as the same OS user as the daemon can read `daemon.json` and obtain
  the operator token. Real isolation needs agents under a separate account or an OS
  sandbox, planned for later.
- **Web sign-in on a shared machine.** Opening the sign-in link passes it to the
  browser opener on its command line, where another local user could read it for that
  moment and race the browser to use it; `shepherd web --print` avoids that. Anyone
  with the operator token can mint a link. The daemon speaks plain HTTP on loopback, so
  the session cookie is not marked Secure; TLS for direct remote access is later work.

## [0.1.0-beta.1] - 2026-10-09

The first public beta of uBixShepherd: one local daemon coordinates agent work across a
workspace of Git repositories. Work happens in scoped lanes, agents run in those
worktrees, and the front desk brings decisions and activity into one terminal
conversation. This is a beta: public interfaces may change between releases. See
https://github.com/ubixsys/ubixshepherd/blob/main/docs/VERSIONING.md.

### Added

- **One binary and daemon.** `shepherd` provides the daemon, CLI, HTTP API and MCP
  server. It builds for Linux, macOS and Windows on amd64 and arm64. Commands start the
  daemon when needed. `shepherd daemon install` registers a login service with launchd
  on macOS or systemd on Linux. The daemon listens on loopback, stores state in SQLite
  under `~/.shepherd` or `$SHEPHERD_HOME`, and stops agents left behind after a crash.
  On systems with SIGHUP, `shepherd daemon reload` reloads configuration. The daemon
  writes and rotates its own log.
- **Workspaces and repo profiles.** `shepherd init` registers a directory of
  repositories and lets you choose which it manages. Profiles in
  `~/.shepherd/config.yaml` set the base branch, branch model, gate, shared paths,
  worktree setup, agent permission mode, and push, merge and tag autonomy. Defaults
  keep merge, tag and deploy decisions with the person.
- **Lanes and per-repo scope leases.** `shepherd lane open` creates a branch from a
  fresh fetch of the base branch, a worktree and a declared scope. Overlapping scopes
  in the same repository are refused, naming the lane and paths in conflict. Lane
  records retain who opened them and through which surface. `lane close`, `lane scope`,
  `lane review`, `fold gc` and `fold retire` support lane cleanup and review without
  removing unfinished work.
- **Pre-push and release-tag checks.** When it can safely install its hook,
  `lane open` adds the pre-push check; `shepherd hook` can install, remove or report on
  it, and does not replace another hook. For lane pushes the hook checks the branch,
  scope and configured commit-message rules. With `tags: reserved`, it also checks that
  pushed release tags were reserved and include the lane's merge.
- **Lane import and origin records.** `shepherd fold import` can turn a coordination
  file's lane table and matching worktrees into Shepherd lanes, reporting what it
  cannot import. Lane records capture their origin, such as the CLI, MCP, front desk,
  import or a followed release. With `coord_file` configured, Shepherd maintains a
  generated lane and tag-reservation view in that file.
- **Tag reservations.** `shepherd tag reserve` allocates release versions against
  remote tags and live reservations. With `tags: reserved`, the hook refuses an
  unreserved release tag and checks that a lane's tag contains its merge.
- **Agent runs.** `shepherd lane run` starts Claude Code, GitHub Copilot CLI or Cursor's
  `cursor-agent` headless in a lane, under daemon supervision. Prompts are sent on
  standard input, not exposed in process command lines. Each lane keeps its agent
  session for later runs; `run continue`, `run attach`, `run logs`, `run stop` and
  `run show` manage runs. Agent output is redacted before it is stored or shown, and
  `daemon.max_runs` limits concurrent runs per machine.
- **Permission modes and repo autonomy.** Agents use their repo's
  `agent.permission_mode`, defaulting to `auto`. Claude Code supports `auto`,
  `acceptEdits`, `default` and `bypassPermissions`; Copilot maps `auto` and
  `bypassPermissions` to allowing all tools and uses an allow list for the other modes.
  Cursor has no comparable permission modes and runs with `--force`. By default
  agents cannot push. `autonomy.push: agent` allows an agent to push its own lane to
  the repository's `origin`; `autonomy.merge: agent` lets it arm its own GitLab merge
  request to merge when the pipeline succeeds. With `autonomy.push: shepherd`,
  Shepherd checks scope and commit rules, runs the repo's gate itself, then pushes and
  opens or updates the merge request. `shepherd lane ship` does that for committed
  lane work on request. Shepherd never merges.
- **Decisions and requests.** Worker tools let agents use `ask_human` to hold a
  decision with options and a recommendation, `ask_shepherd` to ask a question, hand
  off work or request a review from another lane, and `report` to record progress,
  done or blocked. Answers and replies continue the asking agent's conversation.
  Deterministic rules route requests between lanes or explain why they cannot; reviews
  use a different provider in a fresh session. `shepherd decision` and
  `shepherd request` list and act on them.
- **Cost and quota tracking.** Each run records its attributable cost: dollars for
  Claude Code, Copilot credits converted at a configurable rate, and no reported cost
  for Cursor. `daemon.budget` holds runs Shepherd starts on its own after daily spend
  reaches the limit. An agent that reports a quota limit is held until its reset time,
  or retried after a pause when no reset time is available.
- **GitLab lane watching.** The daemon polls open lanes' merge requests through the
  local `glab` login and stores no forge credentials. A merge closes a lane using the
  forge's merge commit as proof, including squash merges. Failed pipeline job logs are
  sent back to the lane's agent to fix, at most twice per merge request. When a forge
  host is unreachable, the watcher backs off from one to fifteen minutes and resets
  the wait when the host responds.
- **Following dependency releases.** A repo profile's `follows` entry can open a lane
  and start an agent when a dependency publishes a release. It can wait for the
  release pipeline to pass, or follow tags directly.
- **Adopted conversations.** `shepherd session` imports Claude Code sessions run by
  hand in a managed repo, so they can be reopened or asked questions. Their answers
  are read-only, and a lane can route questions to an adopted conversation.
- **MCP tools.** `shepherd mcp` serves operator tools over stdio for Claude Code or
  another MCP client. Tools map to Shepherd CLI commands. Worker tools are for agents
  Shepherd starts.
- **The terminal front desk.** `shepherd chat` runs a Claude Code front desk with
  operator tools and read access to files, but no edit or shell tools. The conversation
  prints inline to terminal scrollback and replays recent history at startup. Ctrl-O
  opens the full searchable transcript in a pager, with markdown rendering in the
  thread. An attention-sorted dock shows lane and run state, merge-request badges and
  counts, and decisions can be answered in place.

### Interface changes

- Added `shepherd request close <id> [--why TEXT]` and the `request_close` MCP tool,
  also available to the front desk. They close a stale request without a reply and
  retain the reason. Requests already replied to, failed or closed are refused.
- Added `/model` in chat to show, set or reset the desk's model. The
  `shepherd chat --model` flag overrides it for that chat, `desk.model` configures the
  front desk, and per-repo `agent.model` selects a model per agent when a run does not
  specify one.
- Added `daemon.log_level` to select `debug`, `info`, `warn` or `error`; a configuration
  reload applies it immediately.

### Changed

- A `go install` build reports the module version recorded by Go instead of `dev`.
- `autonomy.push: agent` now permits a running agent to push only its own lane branch
  to the repo's `origin`; the hook checks the target remote as well as the branch,
  scope and configured commit rules.

### Known limits

- **GitLab is the only forge for lanes.** Merge request polling, merge closure,
  pipeline handoffs and merge arming use GitLab through `glab`. GitHub is the release
  mirror and publishing home; GitHub mirror and publication proofs are not built.
- **macOS and Linux are tested; Windows builds in CI but is untested.** Windows has
  no service-manager integration, and does not support SIGHUP reload.
- **Not built yet:** workspace-wide leases (scopes are per repository), typed and
  cross-repo work orders, triage, the OpenCode adapter, `verified-on:<env>` proofs,
  and the proposed merge-discipline checks.
- **The web UI is a preview.** Its sources are in `web/`; it runs from a development
  server against a running daemon, which does not serve it.
- **`autonomy.push: agent` uses a cooperative pre-push guard, not a security boundary.**
  An agent can bypass it with `git push --no-verify` or by editing the hook. Forge
  branch protection and merge rules remain the enforcement boundary.
- **No independent security review.** See
  https://github.com/ubixsys/ubixshepherd/blob/main/docs/VERSIONING.md#what-the-version-does-not-communicate.
