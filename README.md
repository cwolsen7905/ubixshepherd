# uBixShepherd

**One voice to direct a whole swarm of AI agents.**

uBixShepherd is a master control agent: a single point of communication between one human
and many AI agents (Claude, Gemini, and whatever comes next). You talk to Shepherd; Shepherd
hands out the work, keeps the agents from stepping on each other, holds the decisions that
are yours for you, and reports back in one thread.

> Status: **early build**. v1's scope and stack are decided ([docs/v1.md](docs/v1.md)): a Go
> core, the Fold and dispatch, GitLab and GitHub, useful on any repo and aimed at uBixCore.
> The first milestone (M1, the skeleton) is in: the daemon, its API, the store, config and
> repo profiles, and workspaces. Lanes and everything after them are still design.

Part of the **uBix** family of open-source systems tooling (uBixCore, uBixVault, uBixOps,
Replikate, UbixOS), published under [uBixSys](https://ubixsys.com).

## Build and run

Needs Go (see `go.mod` for the version) and git.

```sh
make build                  # bin/shepherd for this machine
make check                  # gofmt, vet, tests, and the core boundary check
make cross                  # dist/ for Linux, macOS and Windows on amd64 and arm64

bin/shepherd init ~/git     # finds the repos below ~/git; you choose which Shepherd manages
bin/shepherd status         # the daemon, its workspaces, and where you are
bin/shepherd where          # the workspace, repo and lane for this directory, with its profile
```

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
