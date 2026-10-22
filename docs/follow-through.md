# Follow-through

Shepherd follows a lane's merge request through its failures: a failed pipeline and
unresolved review threads go back to the lane's agent, up to `watch.fix_tries` rounds
(see [config.md](config.md)). This page covers which agent takes each round.

## Escalation to Claude

A round continues the lane's last conversation, so by default it runs on whatever agent
and model took the work before. When that was a cheaper one and the round did not close
what it was sent to close, running it there again rarely helps. So the next round runs on
Claude Code instead.

A round counts as not closing when the watcher finds the same kind of item still open and
starts another one. The first round is never escalated: it gives the lane's own agent a
chance.

| The previous round ran on | The next round runs on |
|---|---|
| Copilot, Cursor, OpenCode, or any agent but Claude Code | Claude Code, on the repo's `agent.model` for `claude` (else `models.claude.default`, else Claude's own default) |
| Claude Code on a model other than that default | The same Claude conversation, on the default model |
| Claude Code on that default (or on any model when none is named) | Unchanged: there is nowhere further to go |

Details:

- Moving off another agent resumes the lane's Claude conversation if it has one, else
  starts one with the usual brief. The prompt carries the open threads or job logs as
  before, with a line saying the earlier round did not close them.
- The round is still one of Shepherd's own starts: the daily budget, run caps, quota holds
  and model policy apply. If Claude Code is not installed or is out of quota,
  the round stays with the previous agent rather than failing.
- Rounds still spend the `watch.fix_tries` budget.
- Set `watch.escalate: false` to keep every round on the same agent.
