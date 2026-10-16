# Keep-going stall alert

Status: **approved** (Next-up item 81). Built in `internal/daemon/keepgoing_stall.go`.

## The problem

With keep-going on, the person expects work to go on while they are away. When nothing
is running and nothing can be started, the mode sits on and says nothing: the idle
limit eventually turns it off, and the person comes back to find hours were lost to a
decision, a question or a merge only they could settle.

## Behaviour

A workspace is **stalled** when all hold:

- keep-going is on for it;
- no run is running in its lanes;
- at least one approved Next-up item has no lane yet (its effective state is still
  `approved`), and it is for a repo in the workspace or for none.

When a workspace has been stalled for a short grace period (two minutes, so the gap
between one run ending and the desk starting the next never alerts), the daemon posts
**one** `keep_going` feed item. The feed is what the thread, the desk's next event and
the web already carry, so no new kind, route or config is needed. The text names each
approved item and what blocks it:

| Blocker | Source | Says |
|---|---|---|
| A held decision | open decision on a lane in the item's repo | decision N, answer it |
| A question waiting on the person | open desk question (no repo, so it applies to every item) | question N, reply |
| A blocked lane | last run reported blocked | lane X is blocked, open it |
| An unapplied scope widening | answered scope decision not yet applied | lane X, widen or look |
| A request nobody can route | request needing routing | request N, route it |
| A merge held for the person | MR held at the approval job, or failed | MR !N on lane X, approve |
| Unpushed work | lane with commits and `push: human` or `shepherd` pending | lane X, ship it |
| Quota | an agent is held out of quota | until time |
| Repo unknown | item names a repo not in the workspace | register it |
| Nothing known | none of the above | the desk has not started it, ask it |

These are the items `GET /v1/waiting` already derives, filtered to the item's repo, so
the alert and the Inbox never disagree. A limit that turns the mode off (spend cap,
budget, idle, day end) already posts its own off line and is not repeated.

## Debounce

State is in memory, per workspace: when the stall was first seen and whether it was
told. It is checked on the existing triggers (the one-minute timer, run end, start).
The alert fires once when the grace passes; it fires again only after the workspace has
stopped stalling (a run started, no approved item left, the mode went off) and then
stalled anew. A daemon restart forgets the state, so a stall that lasts across a restart
is told once more; that is acceptable and cheaper than a new store column.

## Not done

- **Scope overlap with another lane.** An approved item carries no scope until a lane is
  opened for it, so the daemon cannot say which lane would overlap. An unapplied scope
  widening and a blocked lane are named instead.
- **Config, web and `internal/waiting` changes.** None were needed.
