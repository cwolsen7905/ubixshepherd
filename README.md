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

bin/shepherd daemon         # runs in the foreground; Ctrl-C stops it
bin/shepherd init ~/git     # finds the repos below ~/git; you choose which Shepherd manages
bin/shepherd status         # the daemon, its workspaces, and where you are
bin/shepherd where          # the workspace, repo and lane for this directory, with its profile
```

Shepherd keeps its files in `$SHEPHERD_HOME`, or `shepherd/` under the OS user config
directory: `config.yaml` (optional), the SQLite store, and the running daemon's address and
access token. The daemon listens on loopback only. A repo's profile comes from
`config.yaml`, over cautious defaults (a human merges, tags and deploys; agents plan first):

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
