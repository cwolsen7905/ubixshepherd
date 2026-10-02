# Vision

## The problem

A developer running several AI agent sessions at once across their projects (Claude Code
sessions in different worktrees, a Gemini reviewer in CI, more to come) becomes the
switchboard. They relay context from one session to another, keep track of which agent
claimed which files, notice when two lanes are about to collide, and answer the same
"shall I continue?" question in five terminals.

The agents are capable. What is missing is **one place to talk to all of them**.

## What uBixShepherd is

A **master control agent** that sits between one human and many agents:

- **One point of communication.** You give direction in one thread. Shepherd turns it into
  work for the right agents and brings the results back, summarised, in that same thread.
- **Dispatch.** Shepherd decides which agent (and which kind of agent: Claude, Gemini, a
  local model, a CI job) gets which task, and starts it in a suitable workspace.
- **Coordination.** Shepherd owns the shared state that keeps agents out of each other's
  way: who is working on what, which files and branches are claimed, what is blocked on
  whom. Agents ask Shepherd instead of reading each other's notes.
- **Gatekeeping.** Some decisions belong to the human: money, pricing, published promises,
  destructive actions on production, approving a merge. Shepherd recognises those, holds
  the work, and asks once, clearly. Everything else keeps moving.
- **Memory.** Shepherd remembers what the swarm has done and decided across sessions, so a
  new agent starts from the current state rather than from zero.
- **Model-agnostic.** Claude, Gemini and others are interchangeable sheep. Shepherd should
  be useful with only third-party agents on day one.

In one sentence: **you give the direction, Shepherd keeps the flock together.**

## What it is not

- **Not an agent runtime.** Shepherd directs agents; it does not try to be the best coder.
  An in-house agent, if one is ever built, is a separate product: **uBixFlock** (see
  [naming.md](naming.md)).
- **Not a replacement for review.** Agents never approve their own work. Shepherd can route
  a merge to the human for sign-off; it never gives that sign-off itself.
- **Not tied to one product.** Like everything in the uBix family, it must pass the
  uBixCore boundary test: a completely unrelated company should want it verbatim. Nothing
  in it may know about any one product.

## Rough capabilities (unordered, unscoped)

These are ideas, not commitments. Scope is an open question.

- Start, stop, and list agent sessions across machines and providers.
- A single inbox/thread for the human (terminal, Slack, Discord, web: undecided).
- Lane registry: the lane protocol from `AGENTS-COORD.md` as a service instead of a file
  (see [origins.md](origins.md)).
- Path and branch claims, with conflict warnings before work starts.
- A "this is yours" queue: decisions held for the human, each with the trade-offs and a
  recommendation.
- Status roll-up: what is running, blocked, waiting on a pipeline, waiting on approval.
- Hand-off packets: when a session runs out of context, Shepherd holds the summary and
  briefs its successor.
- Policy (the "Crook"): what each agent may do without asking, per project.
