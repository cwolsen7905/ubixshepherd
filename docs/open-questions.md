# Open questions

These are the maintainer's calls; an agent should bring each one with the trade-offs and a
recommendation, not a bare menu. Numbers are stable: a decided question keeps its number
and records the decision, so links to it stay right.

1. **Scope of a first version.** *Decided 2026-10-01:* the Fold and dispatch together,
   proven on ubixcore, then a host product built on it. See [v1.md](v1.md). (design.md §6 had proposed the
   standards pack first; it now comes after v1.)
2. **Where the human talks to it.** *Decided 2026-10-01:* the terminal first, a web page
   later, and a hosted `shepherd.ubixsys.com` with logins as a future enterprise feature.
   Discord and Slack remain candidates after that.
3. **How agents talk to it.** MCP server exposing Shepherd's tools to each agent is the
   obvious fit for Claude and increasingly for others; a REST API for CI jobs. v1 proposes
   both (operator and worker MCP tool sets over one HTTP API); not yet confirmed.
4. **Language and stack.** *Decided 2026-10-01:* a Go core (one binary per OS: daemon,
   CLI, MCP server, HTTP API) and a TypeScript/React web UI later. Chosen for Windows,
   macOS and Linux from one build and a path to a GUI. Contracts in JSON Schema; webhook
   intake stays in uBixOps.
5. **Who runs the agents.** *Decided 2026-10-01:* Shepherd starts them (dispatch is in
   v1), on the machine it runs on. Containers on the k3s cluster and hosted agents are
   still open.
6. **Secrets.** Provider API keys and tokens belong in uBixVault, never in this repo or in
   prompts. How does Shepherd get scoped credentials to each agent? v1 uses the provider
   CLIs' own logins; Vault leasing is later.
7. **Relationship to uBixOps.** A separate service, or a later uBixOps capability? v1
   proposes a separate service that uBixOps forwards GitLab webhooks to once Shepherd runs
   somewhere reachable.
8. **Open source and licence.** Like the rest of uBixSys, presumably public; confirm
   licence and when it gets a page on `ubixsys-web` (the family convention). Now tied to
   question 12.
9. **uBixFlock.** Whether and when an in-house agent runtime is worth building. Not before
   Shepherd works with third-party agents.
10. **Standards pack format.** Markdown with structured front matter, or schema-first,
    rendered to `CLAUDE.md` / `AGENTS.md` / `GEMINI.md`. And where the uBix pack lives:
    in uBixCore beside the standards it summarises, or here.
11. **Routing policy.** The first routing table ([design.md §3.11](design.md#311-routing-the-right-agent-for-the-work)):
    which task kinds exist, which provider and model each starts on, how many gate failures
    before escalating, and whether a cost budget per task is a hard stop or a warning.
12. **Open core and the enterprise edition.** "Framework-grade, product-free, open
    source" and "an enterprise edition people unlock at `shepherd.ubixsys.com`" can both
    hold under an open-core model (the core free; hosted, multi-user and team features
    paid). Which features sit on which side, and the licence that allows it. Not needed
    for v1.
13. **Adapter protocol.** Drive each provider's CLI directly (JSONL streams, version-pinned)
    or act as an ACP client so one adapter covers many agents. The roadmap starts with the
    CLIs and spikes ACP later ([roadmap.md §5](roadmap.md#5-nice-to-haves)).
14. **Repo autonomy profiles.** The research found merge, tag and deploy rights differ per
    repo (who merges, who tags, who deploys, what needs a plan first). The first profiles
    need the maintainer's confirmation, since they encode what agents may do unasked.
15. **Positioning against GitLab Duo Agent Platform**, which already runs Claude Code and
    Codex inside GitLab: complement it, ignore it, or adapt to it as one more provider.
16. **Which generic packs ship with v1.** The roadmap proposes Go (for the non-uBixCore
    pilot) alongside the uBixCore pack; Node/TypeScript is the next most useful to outside
    users.
17. **The terminal.** What the one terminal the human works from looks like: one thread
    of the front-desk conversation plus typed events from the swarm, with drill-down into
    and attach to any agent ([design.md §3.15](design.md#315-the-terminal-one-thread-many-feeds)),
    or another shape. And when: `shepherd watch` could come early; the embedded front desk
    after held decisions exist.
