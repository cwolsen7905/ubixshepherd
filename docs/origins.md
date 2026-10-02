# Origins: what already exists

Shepherd is not starting from a blank page. Its ideas are practices already running, by
hand or as scripts, in the uBix repos (uBixCore and the products built on it). Shepherd's
job is to lift them out of files and conventions into one tool. Read these before designing
anything; they are the working prototype.

## The lane protocol (`AGENTS-COORD.md`)

Used in `ubixcore`, `ubixvault`, `UbixOS` and the host products built on uBixCore. Several agent sessions work
concurrently; each one:

- registers a **lane** (a row with a distinct branch prefix) in an untracked
  `AGENTS-COORD.md`, seeded from a tracked `AGENTS-COORD.template.md`;
- works in its own git worktree (`../<repo>-worktrees/<lane>`);
- **claims** shared paths in an append-only log before editing them (root docs,
  `CLAUDE.md`, per-app `Routes.php`/`Dependencies.php`, CI config, lock files);
- checks `git status` and never stashes, resets or switches branches over someone else's
  uncommitted work.

The rules live in `ubixcore/docs/standards/branching-and-git-workflow.md` § Concurrent
Agent Sessions. **This is the Fold.** Its weak points are what Shepherd should fix: it is a
file each agent must remember to read, claims are honour-system, and nothing tells the human
when two lanes collide.

## Scale today

uBixCore's live `AGENTS-COORD.md` on 2026-10-01, the main reason v1 starts with the Fold:

- **20 lanes** in the table and **307 log entries** in 541 lines; many lanes are sessions
  started from a host product that do framework work in ubixcore.
- **32 git worktrees** under `../ubixcore-worktrees/`, some long since landed. The
  `code:worktree` bootstrap was never ported, so each is created and removed by hand.
- **Two tag races in two days**: v0.39.0/v0.40.0 and v0.43.0/v0.44.0, each sorted out in
  the log after the fact.
- **Lessons for the next lane written as log paragraphs** (a test needing `#[Depends]`
  because `phpunit.xml` orders by `depends,defects`; a behaviour change in
  `AbstractTestCase`'s test-database mapping). An agent sees them only by reading the log.
- **Owner steps relayed by hand**: approve, then retry `require-approval`; "LANDED !205 at
  a7b522c5, claims released" typed by the agent.

Note that ubixcore uses the *framework profile*: `main` is the only long-lived branch and a
release is a `v*` tag. Host products keep `dev` → `staging` → `main`. Shepherd's repo
config has to carry the difference.

## Merge sign-off

Every merge in `ubixcore` and its hosts needs the owner's Approve (or 👍), enforced by a
`require-approval` CI job (uBixCore `ci:requireApproval`) plus "Pipelines must succeed".
**Agents never approve or 👍 an MR, their own included**, even though they run under the
owner's account and the API would allow it. This is the model for Shepherd's gatekeeping: the
human's click must keep its meaning.

## uBixOps

A small uBixCore framework app (`php/Ubix/Bootstrap/apps/UbixOpsApi`, published image, Helm
chart) that receives GitLab webhooks; when an MR is
approved, it re-runs the sign-off job. It is the first piece of "infrastructure that reacts
to the human on the agents' behalf", and a possible home or neighbour for parts of Shepherd.
Architecture: `ubixcore/docs/architecture/ubix-ops.md`.

## AI review in CI

uBixCore `ci:aiReview` runs a Gemini review of each MR on the CI runner and posts findings
as GitLab threads. Findings, or a failure to review, open a thread that **must be resolved by
a human** before merge; a clean review resolves itself. Agents never resolve these threads.
Standard: `ubixcore/docs/standards/ai-review-in-ci.md`. This is an example of a non-Claude
sheep already in the flock.

## Working agreements with agents

The agreements that work in practice, from the hosts' `CLAUDE.md` files:

- **Keep going.** Chain whole areas of work; don't stop to ask "shall I continue?".
- **Stop only for what is genuinely the human's:** money, pricing, published promises, choosing
  between materially different products, destructive production actions, design calls with
  no defensible default.
- **Tell the human what is true**, including about your own work; verify state rather than assume
  it (a push is not a merge; a green local gate is not a green pipeline).

These are the policy Shepherd should enforce and route by: what flows on automatically, and
what lands in the human's "this is yours" queue.

## Hand-offs

Long sessions run out of context and are continued from a written summary. Today that
summary is generated per session and per-repo notes live in `docs/.../journal/` files.
Shepherd holding and passing on hand-off packets is a direct improvement.
