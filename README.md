<img src="docs/brand/logo.svg" alt="uBixShepherd" width="228">

# uBixShepherd

**One voice to direct a whole swarm of AI agents.**

uBixShepherd is a master control agent: a single point of communication between one human
and many AI agents (Claude, Gemini, and whatever comes next). You talk to Shepherd; Shepherd
hands out the work, keeps the agents from stepping on each other, holds the decisions that
are yours for you, and reports back in one thread.

> Status: **early build**. v1's scope and stack are decided ([docs/v1.md](docs/v1.md)): a Go
> core, the Fold and dispatch, GitLab and GitHub, useful on any repo and aimed at uBixCore.
> In so far: the daemon, its API, the store, config and repo profiles, workspaces (M1),
> and from M2 lanes, scope leases and the pre-push hook, plus first slices of the MCP
> server (M4) and of dispatch (M5): starting Claude Code, GitHub Copilot or Cursor in a
> lane. Tag reservations, forge proofs, work orders and routing are still design.

Part of the **uBix** family of open-source systems tooling (uBixCore, uBixVault, uBixOps,
Replikate, UbixOS), published under [uBixSys](https://ubixsys.com).

## Build and run

Needs Go (see `go.mod` for the version) and git.

```sh
make build                  # bin/shepherd for this machine
make check                  # gofmt, vet, tests, and the core boundary check
make cross                  # dist/ for Linux, macOS and Windows on amd64 and arm64
make install                # copy to ~/.local/bin/shepherd and restart a running daemon

bin/shepherd init ~/git     # finds the repos below ~/git; you choose which Shepherd manages
bin/shepherd status         # the daemon, its workspaces, and where you are
bin/shepherd where          # the workspace, repo and lane for this directory, with its profile
```

### Lanes

A lane is one stream of work in a repo: its own branch, cut from a fresh fetch of the
repo's base branch, its own worktree, and a declared scope.

```sh
cd ~/git/myrepo
shepherd lane open feat/login --scope 'src/auth/**' --scope docs/auth.md
cd ~/git/myrepo-worktrees/feat-login      # work here, or start an agent here
shepherd lane list                          # this repo's lanes; --all, or run at ~/git, for every repo
shepherd lane close                         # from inside the worktree, or: shepherd lane close feat/login
shepherd fold gc                            # worktrees that look finished, across the workspace
shepherd hook status                        # is the pre-push hook installed in this repo
```

A lane's scope is its lease: `lane open` refuses a scope that overlaps an open lane's,
naming the lane and the paths, judged against the repo's files plus globs for paths not
created yet. It reports any of the repo profile's `shared_paths` the lane takes. It
also refuses a name, branch or worktree path already in use.

The **pre-push hook** enforces the scope where every agent has to pass: a push from a
lane may only update the lane's branch, with changes inside its scope. `lane open`
installs the hook when that is safe (no `pre-push` hook yet, hooks inside `.git`);
otherwise it says what to do. `shepherd hook install | uninstall | status` manage it by
hand; it never replaces another hook, and prints the line to add to one instead. Pushes
from outside a lane are left alone, and `git push --no-verify` skips the check once.

 `lane close` refuses a
worktree with uncommitted changes, and a branch git cannot see merged into the base. A
squash merge looks unmerged to git, so after one, `lane close --force` closes the lane and
keeps the branch. `fold gc` only lists; it removes nothing.

### Agent runs

Shepherd can start an agent headless in a lane's worktree:

```sh
shepherd lane run feat/login --agent claude "Add rate limiting to the login handler, with tests"
shepherd lane run --agent copilot "..."     # from inside the lane's worktree
shepherd run list                            # this lane's runs; --all for every lane
shepherd run show 7                          # state, time, exit code, commits, files outside the scope
shepherd run logs -f 7                       # follow its output
shepherd run continue 7 "the lint job failed: fix it"   # same agent, same conversation
shepherd run attach 7                        # the agent's own CLI, resumed, with you at the keyboard
shepherd run stop 7
```

**A lane keeps its conversation.** Shepherd records each agent's session (Claude Code's
session id, Copilot's resume id, Cursor's chat), so the next `lane run` in a lane with the
same agent continues where it left off, with everything it already knows; `--new`
starts fresh. `run continue` sends a follow-up to a finished run's session, and
`run attach` opens that session interactively in the lane's worktree, where you work
with your own permissions and the push hook still checks the scope.

Agents: `claude` (Claude Code), `copilot` (GitHub Copilot CLI), `cursor` (Cursor's
`cursor-agent`), each with its own login on this machine. The daemon owns the run, so
closing the terminal does not stop it; `lane run` follows the output, and Ctrl-C only
detaches.

Every agent gets the same brief (its lane, branch, scope, the repo's gate) and the same
autonomy: **edit and commit inside the worktree, run the gate, never push.** Claude Code
and Copilot are started with `git push` denied; for every agent, git's push URL for
`origin` is replaced with one that cannot connect for the length of the run, and the
pre-push hook refuses pushes from a lane while an agent runs in it. You review the
lane's commits and push. One agent runs per lane; `daemon.max_runs` (default 4) caps
them per machine. Output is redacted and kept in `~/.shepherd/runs/`.

### Agents ask, you answer

Agents Shepherd starts get three tools of their own, and their brief says when to use
each: `ask_human` for anything that is yours to decide (money, published text, deleting
data, production, a scope or design call), `ask_shepherd` when they need another lane,
and `report` for progress, done or blocked. An agent that asks ends its turn; your
answer goes back into its conversation and it carries on.

```sh
shepherd decision list                 # what agents are waiting on you for, with options and a recommendation
shepherd decision answer 3 2           # pick option 2, or answer in words: "keep the old price"
shepherd run show 12                   # its reports and decisions, with the run's outcome
```

Claude Code and Copilot get the tools by flag; Claude Code sees only Shepherd's worker
tools, so an operator server you registered for yourself never reaches an agent. Cursor
reads MCP servers only from its config file, so it needs one step, once:
`shepherd agents setup cursor` adds a `shepherd-worker` entry to `~/.cursor/mcp.json` and
leaves the rest alone.

**Agents talk to each other through Shepherd.** `ask_shepherd` sends a question, a
hand-off or a review request to another lane; the asking agent ends its turn, Shepherd
continues the target lane's conversation with it, and the reply (the target's `report`)
comes back into the asker's conversation. A review goes to a different provider than
the author, in a fresh session, and may not change files. A request Shepherd cannot
route by rule (no lane named, or a lane with no agent yet) waits for the front desk or
you; chains are capped at three requests.

```sh
shepherd request list                  # requests between lanes, and their replies
shepherd request route 4 --lane docs/api --agent cursor   # route one Shepherd could not
```

### From Claude Code (MCP)

`shepherd mcp` serves Shepherd's operator tools over MCP on stdio: `shepherd_status`,
`shepherd_where`, `lane_list`, `lane_open`, `lane_close`, `lane_run`, `run_list`,
`run_status`, `run_continue`, `run_stop`, `decision_list`, `decision_answer`,
`request_list`, `request_route` and `fold_gc`. (`shepherd mcp --worker` is the agents' own set, described above.) Each runs the CLI
command of the same name. Register it once, then start Claude Code at the workspace root
and ask in plain words ("open a lane in myrepo for the login fix, scoped to src/auth"):

```sh
claude mcp add --scope user shepherd -- ~/.local/bin/shepherd mcp   # or wherever the binary is
cd ~/git && claude
```

Any MCP client that can start a stdio server works the same way.

### The daemon

Any command starts the daemon in the background if it is not running, and says so. To
have it start at login and restart if it crashes, register it with the OS service manager
(launchd on macOS, systemd on Linux; Windows to come):

```sh
bin/shepherd daemon install     # writes and loads the LaunchAgent or user unit, with your PATH
bin/shepherd daemon status      # running or not, and whether it starts at login
bin/shepherd daemon stop        # stays stopped until the next login or `daemon start`
bin/shepherd daemon start | restart | uninstall
bin/shepherd daemon             # in the foreground, for debugging; Ctrl-C stops it
```

Register the copy `make install` puts in `~/.local/bin`, not `bin/shepherd`: `make clean`
removes the latter, and `make install` restarts the daemon onto each new build.
`install` copies your current PATH into the service, because launchd and systemd start
programs with a bare one that would hide git and the agent CLIs. Set
`SHEPHERD_NO_AUTOSTART=1` to stop commands starting a daemon (scripts, CI).

Shepherd keeps its files in `~/.shepherd` on every OS (or `$SHEPHERD_HOME`): the config,
the SQLite store, the daemon's log, and the running daemon's address and access token. The daemon listens on
loopback only. On its first start it writes `~/.shepherd/config.yaml` with every setting
commented out, so the defaults apply until you change one; it never touches the file
again. A repo's profile comes from that file, over cautious defaults (a human merges, tags
and deploys; agents plan first):

```yaml
defaults:
  gate: make check
repos:
  ubixcore:                 # the repo's path relative to the workspace
    shared_paths: [README.md, .gitlab-ci.yml]
    autonomy: { tag: agent }
  my-app:
    base_branch: dev
    branch_model: promotion
    promotion: [dev, staging, main]
```

## The pitch

> **uBixShepherd** is one place to run all my AI agents. Instead of juggling separate
> sessions of Claude, Gemini and whatever else, I talk to Shepherd, and it hands out the
> work, keeps the agents from stepping on each other, and reports back in one thread. It's
> mission control for an AI swarm: I give the direction, it keeps the flock together.

More versions (one-liner, technical) are in [docs/pitch.md](docs/pitch.md).

## Read next

| Doc | What it holds |
|---|---|
| [docs/vision.md](docs/vision.md) | What Shepherd is for, what it does, what it deliberately is not |
| [docs/design.md](docs/design.md) | **The core design**: how Shepherd applies uBixCore's typed-boundary philosophy to make multi-provider agent work deterministic |
| [docs/v1.md](docs/v1.md) | **What v1 builds**: decided scope and stack, the Fold and dispatch around uBixCore, milestones |
| [docs/roadmap.md](docs/roadmap.md) | The MVP feature list by milestone, what follows, nice-to-haves, and the research behind them |
| [docs/naming.md](docs/naming.md) | Why "Shepherd", the names we rejected, and the family vocabulary (Flock, Fold, Crook, Pasture) |
| [docs/origins.md](docs/origins.md) | The practices Shepherd grows out of, already running around uBixCore |
| [docs/pitch.md](docs/pitch.md) | Elevator pitches, ready to paste |
| [docs/open-questions.md](docs/open-questions.md) | Decisions not yet made |
| [CLAUDE.md](CLAUDE.md) | Hand-off notes for an AI session picking this up |
