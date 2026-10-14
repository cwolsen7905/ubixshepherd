# The front desk's session

The daemon's front desk is one Claude Code session per workspace, resumed headless on
every turn: the person's messages, and Shepherd's wake-ups for the swarm's events. Each
turn rereads the whole session, so a long day of use grows slower and dearer turn by turn,
mostly from long tool results (run logs, request lists) that stay in the context for good.
Two things keep it small: the session is **rotated** once it grows past a threshold, and
what each tool returns to the desk is **capped**.

## Rotation

At the end of a turn, Shepherd checks two measures:

| Measure | From | Setting | Default |
|---|---|---|---|
| Context size | The input and cache tokens of the turn's last model call, as Claude Code reports them | `desk.rotate_tokens` | 150000; 0 never rotates on size |
| Session cost | The session's total cost, as Claude Code reports it | `desk.rotate_cost` (dollars) | unset: never rotates on cost |

When either is passed, the daemon ends the session and the feed gets one `desk_rotated`
item saying why. The next turn starts a new session seeded with a summary. Nothing else
changes for the person: the thread, its history and the streams go on as they were, and
the turns already queued, and the swarm's events waiting for the desk, run in the new
session in their order.

A rotation never happens mid-turn: a turn that was interrupted, failed, or cut short by a
new conversation or the daemon stopping is not measured, and the session is checked
again after the next turn.

Starting a new conversation on purpose (`POST /v1/desk/new`, which a client offers as a
new conversation) takes the same path: its first turn is a new session seeded with the
same summary. Only the very first session of a workspace, with no earlier turn to
continue, starts without one.

Claude Code's own prompt, the brief and the tools come to over 20000 tokens before
anything is said, so `desk.rotate_tokens` is at least 50000.

## The summary

The summary is built by Shepherd from its own records when the new session starts, not
written by a model, and every part of it passes through the same redaction as agent
output. It holds, in this order:

1. Decisions waiting for the person, with options and recommendation.
2. Requests between lanes not yet replied to.
3. Runs in flight, with each one's task and its latest report line.
4. Open lanes, with scope, state and how they were opened.
5. Standing rules: the desk's model, `desk.wake`, and for each repo its base branch,
   autonomy (push, merge, tag, deploy, plan first), permission mode, gate and brief.
6. Decisions the person already answered (the newest five).
7. The last finished runs (five), with outcome, commits and cost.
8. Spend today, by source, against the daily budget.
9. Optionally, a notes paragraph: with `desk.summary_model` set, that model is asked once,
   with no tools and no saved session, for a short paragraph on what the person is after
   and what is still open, from the summary itself. Its cost counts as the desk's. If
   it fails or takes too long, the summary goes without it.
10. The last turns of the thread, verbatim: the message that started each and the desk's
    reply (`desk.summary_turns`, default 6), newest first while they fit.

Each item is cut to a line, each list to twelve items, and the whole summary to
`desk.summary_chars` (default 6000): the records first, then the quoted turns in what room
is left. The summary goes to the new session on standard input before the turn's own
message, never on the command line, and is never stored in the thread. The new session's
brief says that it continues an earlier conversation, what the summary is, and not to
re-derive the facts in it.

What the summary never holds: credentials or tokens (the desk's own token is in its
environment, never in its messages, and anything shaped like a credential in the records
is replaced by `[REDACTED]`), agent output beyond a run's latest report line, tool
results, and the old session's transcript.

## What tools return to the desk

The desk's operator tools run `shepherd mcp --scoped --max-output N`, with N from
`desk.tool_output_chars` (default 4000, at least 500). A tool's output longer than that
keeps its start and its end, with a line in between saying how much was cut and how to
read more: for `run_status`, the run's whole log, redacted, is a file the desk can read
in parts with its Read tool.
MCP clients other than the desk get no cap of their own (only the general one that keeps
the start and end of a very long log).

## Reading a lane's changes

`lane_diff` reads what a lane holds against its base, and changes nothing: the commits, the
committed changes, the uncommitted changes in the worktree and the untracked files, each
file marked inside or `OUT` of the lane's scope. By default it returns the stat, a few
hundred characters for a small lane. `files` adds each file's hunk headers, and `file` one
file's patch as the lane would ship it, so the desk reads the stat first and then only the
files that matter. It is bounded (30 commits, 200 files, 64 KiB per file) and secrets are
masked before it is cut. Use it to see whether a lane's work is inside its scope or ready
to ship before telling the person, and to answer "what did that agent change". It is read
only: the desk still cannot edit, and a file marked `OUT` is the person's to rescope or
revert, not the desk's.

## What waits on the person

Each `[Shepherd] Since your last turn` message ends with a tagged
`[Waiting on you (N)]` line listing the decisions, held merge requests, lanes to ship and
proposed Next-up items that need the person ([waiting-on-you.md](waiting-on-you.md)). The
desk repeats it at the end of its reply, so the person sees it without digging through
the thread.

## The next-up list

`next_list`, `next_add` and `next_update` are the desk's tools for the list of work that is
planned but not being acted on ([backlog-next.md](backlog-next.md)).

| Tool | What it does | Who decides |
|---|---|---|
| `next_list` | Reads the list in the person's order: approved, then planned, then deferred, each with its plan doc and, once a lane is open for it, the lane and merge request. The item marked next is the first approved one with no lane. `all` adds done and dropped items. A `matrix` list follows: the repos' `docs/WORK.md` rows with status `todo` or `blocked`, read-only and with no id. | Read only |
| `next_add` | Adds an item as `planned` when the person says "put X on the list", and, without being asked, for concrete work the person and desk agreed in chat that is not being started now. The desk calls `next_list` first, quotes the person in the note and says in one line that it added it. | The desk may add; the item shows as proposed until approved |
| `next_update` | Changes an item's title, note, plan, repo or lane hint; with `state` or `move`, approves, defers, drops, finishes or reorders it. | `state` and `move` are the person's |

Approval follows the rule `decision_answer` does. The daemon refuses `state` and `move`
(and `next_add` with `approved`) from the desk outside a turn the person started, so a
`[Shepherd]` event can never be the reason an item moves, and an agent's token reaches none
of it. `started` and `done` are not set: they follow the linked lane, which opens with
`lane_open` and the item's `item` id (or `shepherd lane open --item N`), and the item is done
when that lane's merge request merges. The desk does not add items from its own
ideas: it puts on the list what the person asked for or agreed to in the thread (never
approved, so it is not lost when the session rolls over), and says which items agents or it
proposed. Work it only suggested and the person did not take up is not agreed.

## Widening a lane's scope

An agent that needs a file outside its lane's scope asks the person in a decision. Once the
person has answered, `lane_widen` adds the globs they approved to the open lane, so the lane
does not have to be closed and reopened. The tool maps onto `shepherd lane widen <lane>
--decision N --add GLOB...`, which calls `POST /v1/lanes/{id}/widen`.

| Check | Refusal |
|---|---|
| The desk's token is for a turn the person started (the rule `decision_answer` and `next_update` follow) | 403: the desk may not widen a scope in a turn Shepherd started; an agent's token never reaches the route |
| `decision` is an answered decision raised by a run of this lane | 409: it is open, or belongs to another lane |
| No other open lane's scope overlaps the globs (the Fold's lease rule, as for `lane_open`) | The reason names the lane that holds them; nothing changes |

The tool only adds globs; narrowing a scope stays `shepherd lane scope --remove`, the
operator's. Each widening is logged by the daemon and put in the feed as "lane X scope
widened by desk: +globs (decision N); scope is now ...". The desk should pass only the
paths the person's answer approved, and the person can check them against the feed.

## Keep going

[keep-going.md](keep-going.md) describes a mode that lets the desk carry on with what is
already approved. The desk's part:

- **The tool.** `keep_going` takes `on`, `off` or `status` (and, for `on`, `cap`, `runs`
  and `idle`) and maps onto `shepherd keep-going`, which calls `GET/POST /v1/keep-going`.
  `on` is refused in a turn Shepherd started and for an agent's token, the rule
  `decision_answer` follows; `off` is always allowed.
- **The text.** The sentence a `[Shepherd]` event turn ends with comes from one function,
  `EventInstruction`, which reads the flag from the store: "Do not start new work they have
  not asked for" when off, and the keep-going sentence when on. The standing `Brief` gets a
  paragraph on the mode and what it never covers, so a session started before the flag
  changed still knows the rules.
- **The limits are the daemon's.** The brief says the mode; spend, runs, idle time, end of
  day and starts per lane are checked in Go, and a refusal comes back as a tool result with
  its reason, which the desk relays.

## Filing turns under topics

The desk's token is minted for the turn it serves, so the daemon knows which turn is in
progress. `topic_set` with only an `id`, or a new `title`, files that turn under the topic;
the desk never needs the turn's number, and may file only its own turn. A turn Shepherd
starts for a run, decision or request that is already in a topic is filed under that topic
before the desk runs, and the head of the turn says which; when the events fall in several
topics the turn goes to the one most of them belong to and the head names the rest, and
when none is in a topic the head says the turn is unfiled. A person's turn is never filed
by Shepherd. When there are no open topics the head of a turn is left out, as before. See
[topics.md](topics.md) for the rules.

## Settings

```yaml
desk:
  rotate_tokens: 150000   # 0: never rotate on size; else at least 50000
  # rotate_cost: 5        # dollars for one session; unset: never rotate on cost
  summary_model: ""       # a model for the notes paragraph; empty leaves it out
  summary_chars: 6000     # the summary's cap; at least 1000
  summary_turns: 6        # turns quoted; 0 to 50
  tool_output_chars: 4000 # each tool's output to the desk; at least 500
```

A change applies from the next turn after the daemon reloads its config.
