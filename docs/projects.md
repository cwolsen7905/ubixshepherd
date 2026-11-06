# Projects: groups of repos with a shared brief and budget

A **project** is a named group of repos in one workspace that share a brief, a
budget, and a view of their work. It is configuration plus a little state in
Shepherd's store. It is not a directory, a pack, or a product.

- A repo is in at most one project. Sharing goes through requests between lanes
  and `follows`, never dual membership.
- A project adds no new way to act. Lanes, leases, runs, decisions, and autonomy
  stay per repo. A project layers context on top, rolls costs up, and groups work
  in the project views.
- Core stays product-free. Naming a project after a product is the person's
  config, not code.

## Config

Projects sit beside `repos:` in the config file. A member needs no entry of its
own under `repos:`; without one it uses the defaults profile. A repo in no
project behaves as before, and a repo listed in two projects is a config error.

```yaml
projects:
  shop:
    repos: [shop-api, shop-web]
    brief_max_age: 30d            # show the brief as stale past this; default 30d
    budget:
      amount: 20.00               # dollars per period; default: daemon.budget, 0 is no cap
      period: day                 # day (default) or month
      cap: hard                   # soft | hard (default hard)
    caches:                       # optional: local build caches that are not repos
      - path: ~/build/cache
        made_by: shop-api:tools/build.sh   # <repo>:<recipe path>, a real repo's recipe
        note: "Build objects; rebuild, do not hand-patch."

repos:
  shop-api:
    follows:                      # unchanged: a release of shop-api opens a lane here
      - repo: framework
        min_bump: minor
        task: "Move the pin, run the gate, fix what breaks."
```

The brief is not in this file. It is text with an author and an approval, so it
lives in the store and is drafted through the front desk.

## The brief

Each repo keeps its `brief` string, added to every agent's brief in that repo. A
project brief is the layer above it: the goal and the current focus, a few lines
to a page.

Layering, outermost first: workspace rules, then the project brief, then the
repo's `brief`. Every agent Shepherd starts in a member repo, and the front desk,
gets the project brief in its standing instruction. A repo's brief wins on a
conflict; a conflict is a note for the person, not something an agent resolves.

Who writes it: the front desk drafts the brief, and drafts updates when the work
has drifted from it. A draft is only a draft. The person approves it, and only
the approved text is given to agents. This is the same rule as decisions: agents
never approve, and a brief approval takes the person's words. An agent can suggest
a change through `ask_shepherd`; it cannot edit the brief.

Staleness is visible. Each approved brief records `approved_at`. Shepherd shows its
age in the project page and in the brief itself as it is given to agents, so a
stale focus is a stated fact to the agent too. Past `brief_max_age` the front desk
drafts a refresh and the project page marks it; nothing is blocked.

## Spend

Run costs are recorded per run, and a run belongs to a lane and a lane to a repo,
so a project's spend is a sum with no new measurement. The model is the existing
budget machinery: a dollar figure per period, a warning at 80%, a hold on runs
Shepherd would start on its own, and never a stop on a run the person started.

- Each project gets the daemon's budget (`daemon.budget`) as its own unless the
  project sets one, with the same warning at 80%. With no config at all, a project
  behaves like the workspace does now.
- The workspace budget caps the sum of the project budgets. A configuration whose
  project budgets sum above the workspace figure is allowed, and the workspace
  ceiling holds first.
- The front desk's spend is its own line against the workspace ceiling. It is not
  charged to any project.
- A run in a repo outside any project counts toward the workspace total only.
- Every cap is `soft` or `hard` (default `hard` when unmarked):

| `cap` | At 80% | At 100% |
|---|---|---|
| `soft` | Warns. | Warns again. Never stops work: it only reports. |
| `hard` | Warns. | Holds only runs Shepherd starts itself. Never holds a run the person starts. |

A held run says which budget held it and what lifts it, like a quota hold does.
Rollups (per project, per repo, the desk, per day or month) go into the dock's
spend line and the project page.

## Caches

A project may declare local build caches that are not repos: a toolchain build,
a vendored object store, anything slow to make and shared by the project's repos.
A cache is information, not a member.

| Field | Meaning |
|---|---|
| `path` | Where the cache lives on this machine. |
| `made_by` | `<repo>:<recipe path>`: the recipe in a real repo that makes it, so the cache is reproducible. |
| `note` | A line for agents. |

Shepherd tells every agent in the project where each cache is and how to rebuild
it (the recipe path and its repo), and may show its size and age in the project
page. It never opens lanes for a cache, commits to it, or counts it as a member.
Agents rebuild a cache from its recipe rather than hand-patching it. A temporary
cache is removed from config when it is no longer needed.

## Feeds between repos

Two repos relate in one of two ways.

A release feeds a consumer: `follows`. A repo that depends on another's tagged
release already says so in its profile; the followed repo's release opens a lane
in the consumer and hands it to an agent. This stays exactly as it is, inside a
project or across projects. It is a pin upgrade the consumer configures.

Everything else is a request, not a release. When one repo's work affects
another's and there is no tag, the agent sends a request to the other repo's
lane, as it does today with `ask_shepherd`. Inside a project that is the existing
routing: deterministic rules start the target agent or explain why they cannot,
anything needing judgment waits for the front desk, a request addressed to the
person becomes a decision, and chains are capped at `MaxDepth`. A request
addressed to a repo with no open lane is `needs_routing` for the desk, as an
unrouteable request is today.

## How it maps onto what exists

| Piece | Today | With projects |
|---|---|---|
| Repo `brief` | A string in the repo's profile, added to each agent's brief. | Unchanged; the project brief sits above it. |
| `follows` | A release of a followed repo opens a lane and starts an agent. | Unchanged; the way a released dependency feeds a consumer, in or across projects. |
| Requests | `question`, `handoff`, `review`, `person`, addressed to a lane; depth capped. | Unchanged. Requests stay between lanes. |
| Decisions | `ask_human`, routed `person` requests, quota holds. | A brief approval is a decision too, under the same rule as answers. |
| Autonomy | Per repo: `push`, `merge`, `tag`, `deploy`. | Unchanged. A project grants no autonomy. |
| Budget | `daemon.budget`, daily, holds Shepherd's own runs. | Per-project budgets under it, a separate desk line, each cap `soft` or `hard` (default `hard`, today's semantics). |
| Leases and scope | Per repo. | Unchanged. |
| Config | `daemon`, `desk`, `defaults`, `repos`. | Adds `projects` (with `repos`, `budget`, `caches`), validated like the rest, reloaded on SIGHUP. |

See [web-projects.md](web-projects.md) for the Projects tab and the board
grouping, [chat.md](chat.md) for the terminal dock, and
[web-settings.md](web-settings.md#projects) for editing a project in Settings.
