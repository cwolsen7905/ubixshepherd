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

## An external owner and retries

When an external owner follows a repo (`watch.follow_through`, see [config.md](config.md)),
it has its patience to act on a failed pipeline before Shepherd takes the failure over.
What the owner does counts as an attempt:

- A newer pipeline that fails is one attempt, and the wait starts over.
- A retried job that fails again in the same pipeline is one attempt too, and the wait
  starts over from that failure. A retry that fails again is not the owner giving up.
- Once the owner has made `watch.fix_tries` attempts and the pipeline still fails,
  Shepherd takes it over at once rather than waiting out the patience again.

A pipeline whose only failed job is `require-approval` is the sign-off gate, not a
defect. Shepherd says in the feed that the request is awaiting sign-off, asks no agent and
does not start the owner's wait, with or without an external owner. The request stays in
the waiting list as an MR to approve; once a person approves, Shepherd retries the job
(see [merge-rules.md](merge-rules.md)). A new or re-run pipeline replaces the mark. A
failure of any other job, even beside `require-approval`, is handled as above, and the
agent's prompt leaves `require-approval` out.

The feed says when a retry failed again, which attempt it was, and why Shepherd took over.

## An approved request held only by the owner's threads

In a repo set to `autonomy.merge: after_approval` (see [merging.md](merging.md)), an
approved request can wait on review threads that nobody is going to resolve: the external
owner's triage summaries, or findings it answered with a proposal or a dismissal. Shepherd
does not merge past an open thread, and the agent is not asked to resolve what the owner
wrote, so without a signal the merge would wait for ever.

The watcher raises it to the person when all of these hold on one poll:

- a person other than the author (and not a bot) approved the request's current head;
- the head pipeline is finished;
- the request has open review threads, and every one was opened by an account listed in
  `watch.follow_through.author` or has a note from one (authorship is read from the notes,
  never from their text).

A thread of anyone else's keeps the request out of this: those go back to the agent as
usual, and the person already knows about their own. With no `author` listed, nothing is
raised.

Raising does two things:

- one feed item naming the request and linking each blocking thread's first note;
- one entry in the waiting list ([waiting-on-you.md](waiting-on-you.md)), kept in the lane's
  forge record, so it survives a restart.

It is not repeated every poll. It is raised again only when the set of blocking threads
changes (a new one arrives, or one is resolved but others remain). When the threads are
resolved, the approval no longer holds, or the request is merged or closed, the entry
clears. A poll that could not read the forge, or that found the head pipeline still
running, leaves the entry as it was.
