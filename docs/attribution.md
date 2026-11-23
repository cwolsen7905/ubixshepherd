# Attribution

Work an agent does in a Shepherd run is marked, so a reader of the history or of a merge
request can tell which agent and model made it. Shepherd adds the marks itself, not the agent.

## Commit trailers

When a lane opens, Shepherd installs a `prepare-commit-msg` hook in the repo's private hooks
directory (never over another hook, and never in a tracked `core.hooksPath`). In an agent's
run, the hook adds:

| Trailer | Value |
| --- | --- |
| `Co-Authored-By` | the agent's name and a noreply address, e.g. `Claude <noreply@anthropic.com>` |
| `Shepherd-Run` | the run id |
| `Shepherd-Model` | the model, when known |

It uses `git interpret-trailers`, so amending or re-running never adds a trailer twice. A commit
made by hand, with no run, gets none. If the daemon cannot be reached, only `Shepherd-Run` is
added; the commit never fails because of it.

## Merge request footer

A merge request Shepherd opens ends with one line: `Made by <agent> (<model>) in Shepherd run <id>.`
After a run ships its own lane that is the run; for `shepherd lane ship` it is the lane's newest run.

## House standard

The `no-ai-attribution` house standard no longer forbids these trailers. It now requires that a
commit carrying `Shepherd-Run` also carries `Co-Authored-By`.
