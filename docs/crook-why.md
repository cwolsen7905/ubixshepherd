# Crook's verdict log: `shepherd crook why`

Crook answers "may Shepherd do this now?" with one of four verdicts: proceed, ask, hold
or refuse, and the id of the rule that decided. Every answer a call site acts on is
written to the daemon's store, so you can ask afterwards why Shepherd did, held or
refused something. The log is read only: nothing in it changes what Crook decides.

## What is logged

One row per verdict:

| Field | Meaning |
|---|---|
| id, time | Row id (what `crook why <id>` takes) and when it was decided |
| action | What was asked: `run.start`, `push.agent`, `push.check`, `lane.ship`, `tag.push`, `merge.arm`, `keep-going/...`, the person-only actions (`decision.answer`, `next.choose`, `lane.widen`, `keep-going.on`, `brief.approve`) and `route <pattern>` |
| actor | Who asked: `person`, `desk`, `daemon` or `worker` (an agent Shepherd started) |
| verdict | `proceed`, `ask`, `hold` or `refuse` |
| rule, version | The rule that decided, such as `builtin/push-autonomy`, and its version. Built-in rules are version 1 |
| stage | The stage that decided: floor, limits, guardrails, classify, policy or default |
| reason | The text a person would be told, redacted |
| repo, lane, run | What the request named, when it named them |
| facts | The facts that mattered, as JSON with the empty ones left out, redacted |

A check that only classifies the caller or probes what a repo allows is not logged, and a
start held by a limit is logged once, not again each time the queue retries it. A route
that proceeds is not logged: it is a request answered, not an act, and the web
polls constantly. A route Crook refuses is logged.

## Reading it

```
shepherd crook why <id>            one verdict, its rule and its path through the stages
shepherd crook why run <id>        the verdicts for one run, newest first
shepherd crook why lane <name>     the verdicts for one lane, newest first
shepherd crook log                 the latest verdicts
shepherd crook rules               per rule, what it allowed, asked, held and refused
```

`log` takes `--rule`, `--action`, `--kind` and `--since`; `why run` and `why lane` take
`--kind` and `--since`; `--limit` sets how many are listed (default 50). `--since` is a
time in RFC 3339 or a number of days such as `7d`. `rules` takes `--since` (default
`7d`). Every form takes `--json`.

`crook why <id>` prints the stages in the order Crook runs them, up to the one that
decided. A stage with nothing to say reads "no opinion":

```
verdict #12: refuse
  when:    Mon 2026-10-19 14:02:11
  action:  push.agent, asked by worker
  for:     myrepo lane feat-x run 42
  rule:    builtin/push-autonomy (version 1)
  reason:  agent run 42 (claude) is going in lane feat-x; agents Shepherd starts never push. ...
  path:
    1. floor      no opinion
    2. limits     no opinion
    3. guardrails no opinion
    4. classify   no opinion
    5. policy     decided
```

The front desk has the same read as the `crook_why` tool.

## Keeping it

Holds, asks and refusals are kept, but an identical one (same action, actor, rule, reason
and subject) is logged once a minute, so a caller that keeps hitting the same answer does
not fill the log. Proceeds are kept for 90 days: the daemon prunes older
ones when it starts and once a day after. A failure to write a row is logged by the
daemon and never changes a verdict.

The route is `GET /v1/crook/verdicts`, `GET /v1/crook/verdicts/{id}` and
`GET /v1/crook/rules`.
