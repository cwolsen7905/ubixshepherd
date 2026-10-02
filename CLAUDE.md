# CLAUDE.md

Guidance for Claude Code (or any AI session) working in this repository.

## Where things stand

uBixShepherd is in **early build**: M1 (the skeleton) is in, everything after it is design. v1's
scope and stack were decided on 2026-10-01: a **Go** core (daemon, CLI, MCP server, HTTP
API in one binary for Windows, macOS and Linux), the Fold and dispatch together, GitLab and
GitHub, running over a workspace of repos, terminal first with a TypeScript web UI later.
It must work on anyone's repos and is **aimed at uBixCore**: uBixCore support lives in a
pack, never in the core (see `design.md` §3.12). The docs:

- [README.md](README.md): what it is, in one screen.
- [docs/vision.md](docs/vision.md): purpose, capabilities, and what it is not.
- [docs/design.md](docs/design.md): **the core design** (proposed). The two rules that
  shape everything: "the shepherd is not a sheep" (a deterministic control plane, LLMs only
  at the edges) and "enforce at the boundaries every provider must cross".
- [docs/v1.md](docs/v1.md): **what v1 builds**: the decided calls, then the proposed
  shape, Fold, dispatch, cutover from `AGENTS-COORD.md`, and milestones M1 to M6.
- [docs/roadmap.md](docs/roadmap.md): the MVP feature list per milestone, then next steps,
  nice-to-haves, the landscape and risks, from the 2026-10-01 research (proposal).
- [docs/naming.md](docs/naming.md): the name, the family vocabulary (uBixFlock, Fold,
  Crook, Pasture), and rejected names. Use this vocabulary consistently.
- [docs/origins.md](docs/origins.md): the practices around uBixCore that Shepherd
  formalises. **Read before designing anything**, and read the source docs it points to.
- [docs/open-questions.md](docs/open-questions.md): the calls, decided and open (numbers
  are stable; a decided question keeps its number).
- [docs/pitch.md](docs/pitch.md): elevator pitch, one-liner, tagline (the README quotes
  the elevator pitch; keep the two identical).

## Code

Go, one binary (`cmd/shepherd`), packages under `internal/`. `make check` is the gate
(gofmt, vet, tests, `core-boundary`); `make build` gives `bin/shepherd`, `make cross` the six
release targets. CI runs `public-boundary`, `go-check` and `go-cross`.

- The daemon (`internal/daemon`) owns the store; the CLI is a client of the HTTP API
  (`internal/api`, `internal/client`) like every other client. Don't let a command open the
  store directly.
- `internal/store` is an interface; `store/sqlite` uses a pure-Go driver so `CGO_ENABLED=0`
  cross-compiles. Schema changes are appended migrations, never edits.
- Text that stores or shows agent output goes through `internal/redact`.
- `internal/fold` is lanes (and next, leases and reservations). It drives the git CLI
  through `internal/git`, never a git library, so hooks and config behave as for people.
  Its tests build real repos with a bare origin; keep them that way.
- `shepherd mcp` (`internal/cli/mcp.go`) maps each MCP tool onto a CLI command and runs
  it with output captured. Add a tool by adding a command first, then its mapping.
- `internal/dispatch` starts agents (`lane run`). An adapter per CLI builds its headless
  command line; the runner briefs the agent, blocks pushing for the whole run, redacts
  its output into `~/.shepherd/runs/`, and records the outcome. Its tests use a fake agent
  script; a real run of each CLI is a manual check before changing an adapter. Each
  adapter also says how its CLI names a session (chosen up front, created first, or
  printed in the output), how to resume it headless, and how to attach to it.
- Worker tools (`shepherd mcp --worker`, `internal/cli/decision.go`) are for agents
  Shepherd starts: the run comes from `SHEPHERD_RUN`. A decision's answer is delivered by
  continuing the asking run's session (`dispatch.Runner.Answer`), at once or when the run
  ends. Never let an agent answer a decision: `decision_answer` takes the person's words.
- Routing between lanes is `internal/dispatch/route.go`: deterministic rules only (a
  named open lane, its last agent, another provider for reviews); anything needing
  judgment becomes `needs_routing` for the front desk. `Route` runs whenever a run ends.
- Commands autostart the daemon (`internal/cli/daemon.go`); `internal/service` registers it
  with launchd or systemd. Tests leave `Env.Autostart` false; set `SHEPHERD_NO_AUTOSTART=1`
  and `SHEPHERD_HOME` to a temp dir when running the binary by hand.
- `core-boundary` fails if `cmd/` or `internal/` names a product. Product knowledge goes in
  a pack.
- Keep dependencies few: the standard library first (the CLI is `flag`, not a framework).

## This repo is public

It is meant to be mirrored to GitHub, and mirroring copies whole branches with their
history, so **anything committed here is public**. Write every doc and commit message for a
stranger:

- No private product names, internal hostnames, private MR numbers, incident details, or
  anyone's personal working notes. State the lesson in general terms instead.
- Maintainers keep the private evidence and research behind these docs in a separate
  private repository that is never mirrored. If you have access to it, that is where
  specifics go.
- The `public-boundary` CI job fails if the tree matches the pattern in the
  `PUBLIC_BOUNDARY_PATTERN` CI variable (set in the project settings, deliberately not in
  the repo). Public uBix projects (uBixCore, uBixVault, Replikate, UbixOS, ubixsys.com) are
  fine to name.

## Ground rules

- **The maintainer decides scope, stack and naming.** Bring open questions with the
  trade-offs and a recommendation, then a choice. Don't pick a stack and start building
  unasked.
- **Framework-grade, product-free.** Like all uBix tools, Shepherd must be useful verbatim
  to an unrelated company. No product's logic in the core.
- **No secrets in the repo.** Credentials live in uBixVault.
- **Agents never approve their own work**, here or in any repo Shepherd coordinates.
- Default branch `dev`. Work on a branch (e.g. `docs/<topic>`) and land it on `dev` by MR,
  with Conventional Commit style messages (`docs: ...`). Check for `AGENTS-COORD.md` before
  branching; none exists yet.
- Related public repos: `ubixcore` (the framework, uBixOps, CI tooling), `ubixsys-web`
  (where uBix projects get a docs page once released).

## Keeping the docs coherent

- The docs cross-reference each other: the README's "Read next" table indexes `docs/`,
  `design.md` §7 feeds the numbered list in `open-questions.md`, and that list links
  back into design sections by anchor (e.g. `design.md#6-a-phased-path-proposal`). When
  you add, renumber or retitle something, update the links that point at it.
- `design.md` is marked **Proposed**. Don't present anything in it as decided; a decision
  moves out of `open-questions.md` only when the maintainer makes it.
- House prose style: plain sentences, colons and commas instead of em dashes (the docs
  contain none), tables for comparisons.

## Good first step for a new session

Read all of `docs/` (design.md, then v1.md last), then the uBixCore standards that
`origins.md` cites. M1 is in; the next step is M2 (the Fold) in v1.md. The
milestones and everything below v1.md's "Decided" table are proposals: confirm them with
the maintainer before writing code.
