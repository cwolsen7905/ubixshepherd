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
a later run amends keeps the first run's `Shepherd-Run` and `Shepherd-Model` and gains the later
run's, next to both agents' `Co-Authored-By`: every run that touched it is on record. A commit
made by hand, with no run, gets none. If the daemon cannot be reached, or the run's agent is not known, only `Shepherd-Run` is
added (no made-up co-author); the commit never fails because of it. A co-author line with the
same address is never added twice.

## Merge request footer

A merge request Shepherd opens ends with one line: `Made by <agent> (<model>) in Shepherd run <id>.`
After a run ships its own lane that is the run. For `shepherd lane ship` it is the run named by
the newest `Shepherd-Run` trailer among the shipped commits; with none, there is no footer.

## Forbid patterns

A repo's `forbid` patterns (its profile's rules for commit messages) are matched with Shepherd's
own attribution lines taken out: `Shepherd-*` trailers, and `Co-Authored-By` lines carrying a
known agent's address (Claude, Copilot, Cursor, OpenCode). Without that, a pattern written to
keep AI credit out would refuse every push from an agent's run, since those commits must carry
the trailers. The exemption covers those lines only, in every commit, whoever wrote them; the
rest of the message is still matched. A repo that must keep AI credit out entirely should not
run agents through Shepherd.

## House standard

The `no-ai-attribution` house standard no longer forbids these trailers. It now requires that a
commit carrying `Shepherd-Model` also carries `Co-Authored-By`.
