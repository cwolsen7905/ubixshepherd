# Naming

## Why "Shepherd"

The brief: a master control agent that is **one point of communication** for a swarm of AI
agents. A shepherd is exactly that: the flock knows one voice, and the shepherd keeps track
of every sheep and brings back the ones that stray. *"My sheep hear my voice"* (John 10:27).

It fits the uBix family (`uBix` + a plain word, like uBixCore, uBixVault, uBixOps), and the
reference is there for those who know it without being forced. Someone who misses the reference still
reads it as "the thing that herds the agents".

Spelling: **uBixShepherd** in prose; `ubixshepherd` for repos, packages and images;
`shepherd` for the CLI command.

## The family vocabulary

Reserved for future use, so the pieces have names that agree with each other:

| Name | Meaning |
|---|---|
| **uBixShepherd** | The control point. The human talks to it; it directs the agents. *This repo.* |
| **uBixFlock** | A possible in-house agent runtime: our own sheep. A separate product. Shepherd must work without it; Claude, Gemini and others are outside sheep it herds as well. |
| **Fold** | The shared coordination state (lanes, claims, hand-offs). What `AGENTS-COORD.md` is today. |
| **Crook** | The policy/guardrail layer: what an agent may do unasked, and how a stray agent is pulled back. |
| **Pasture** | The sandbox/workspace an agent is let loose in (a git worktree, a container). |
| **Sheep** | One agent session. |
| **Strayed** | An agent that ignored a claim, a policy, or its brief. |

Fold, Crook and Pasture are likely components or concepts inside Shepherd, not separate
products. uBixFlock is the only one intended to be its own product.

Shepherd comes first. Building Flock first would risk designing the protocol around one
agent we happen to own.

## Names considered and rejected

| Name | Why not |
|---|---|
| **uBixMCP** | Tron's Master Control Program, the most fun option. Rejected because it collides with the **Model Context Protocol (MCP)**, which Shepherd will almost certainly speak to its agents. Every conversation would need to say which MCP. |
| **uBixHive** | A strong runner-up: a swarm with a centre, and it reads as "swarm controller" to anyone. Lost on the "one voice" fit. |
| **uBixBridge** | A starship's bridge: one room to command the ship. Also reads as bridging AI systems. Good, but generic. |
| **uBixMaestro** | One conductor, many instruments. Suggests directing more than communicating. |
| **uBixHelm** | Clashes with Kubernetes Helm, which the uBix stack already uses. |
| **uBixBabel** | Many models, many languages, one tower. Funny, but that story ends badly. |
| **uBixLoom** | Weaving many threads. Pleasant, but says little about control. |

Before any public release, check that the GitHub org/repo, package names and container image
names are free.
