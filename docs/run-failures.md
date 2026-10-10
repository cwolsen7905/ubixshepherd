# Run failures: diagnosis and fixes

When a run ends badly, or Shepherd cannot push what a run produced, Shepherd records a
**diagnosis**: why, in one plain line, with the output that shows it and a few suggested
fixes. It is worked out deterministically from what Shepherd saw (exit codes, its own
timeouts, the agent CLIs' usage-limit words, the output of the gate). No model is asked,
so the same failure always gets the same diagnosis.

A run can have several diagnoses: the agent's own failure when it exits, and later what
Shepherd found when it checked a run that had succeeded (the gate failed, a commit
message is not allowed). The newest is the headline. A diagnosis never changes the run's
state: a run whose gate failed is still `succeeded`, since routing and shipping depend on
that.

## What is kept

| Field | Meaning |
|---|---|
| `kind` | One of the kinds below. |
| `summary` | One line, plain words. The feed line for a failed run carries it. |
| `evidence` | The relevant output, redacted and capped at 40 lines and 4 KB: the end of the agent's output, or the failing command's tail. |
| `command`, `exit_code` | The failing command, for a gate or standards check. |
| `files` | Files involved: out-of-scope files, or files the failing output points at. |
| `log` | Where the whole output is (`~/.shepherd/runs/<id>.log`, or the gate's own log). |
| `reset_at` | When an out-of-quota agent's hold lifts. |
| `fixes` | The suggested actions (below). |

Everything passes through `internal/redact` before it is stored.

## Kinds

| Kind | Detected by | Evidence |
|---|---|---|
| `start_failed` | The agent's process could not be started, or the run's log or credentials could not be set up (`Runner.fail`). A binary missing from the daemon's PATH is refused before a run exists, with that message. | The error. |
| `agent_error` | The run failed with a non-zero exit or a process error, and nothing below matched. | End of the agent's output. |
| `hung` | The adapter's `Idle` timeout passed with no output, so Shepherd stopped it. | End of the agent's output. |
| `quota` | The adapter's `Limit` reader saw a usage limit as the run failed. `reset_at` is the CLI's reset time, or the default pause. | End of the output. |
| `no_change` | The adapter's `NoChangeFails` is set, and the run exited 0 with no commit, no file change and nothing asked or reported. | End of the output. |
| `gate_failed` | The repo's gate, run by Shepherd before a push (after a run, or `lane ship`), failed or timed out. | The gate's last 60 lines, the command and exit code, the gate log. |
| `standards_failed` | A standards rule at level `must` failed its check before a push. | The rule id, level and check command, and its output. |
| `commits_forbidden` | A commit message in the lane matches a repo `forbid` pattern. | The commit and the pattern. |
| `outside_scope` | The run's commits change files outside the lane's scope, found when the run ends, before a push, or in the pre-push hook's refusal. | The files. |
| `push_refused` | The push failed (the remote, or the pre-push hook for a reason not covered above), in the agent's output or in Shepherd's own push. | The refusal's words. |
| `merge_refused` | The push worked but the forge refused the merge request. | The forge's error. |
| `stopped` | The run was stopped through `Runner.Stop` (the person, or the desk). | End of the output. |
| `interrupted` | The daemon stopped (`Shutdown`) or crashed (`Recover` found the run still running at the next start). | The end of the run's log. |
| `budget_cap` | The agent said it hit its turn, spend or context limit (`error_max_turns`, `error_max_budget_usd`, "prompt is too long"). A hard spend cap held by Shepherd refuses a start before a run exists and is reported by that refusal. | End of the output. |
| `unknown` | The run failed and nothing above says why. | End of the output. |

## Fixes

Each fix is `{id, label, op, args, needs_person, desk_ok}` and maps onto an existing
operation, so a client can offer it as a button. `needs_person` marks a fix only the
person chooses (it widens what an agent may touch, spends beyond a cap, changes rules or
touches push and merge). `desk_ok` marks a fix the front desk may take on work the person
asked for. A fix with neither is the person's to choose too, but nothing about it is risky.

| Op | Does | Who |
|---|---|---|
| `continue` | Continues the run's session with a generated follow-up that includes the summary, the command and the evidence. | Desk may, except after `stopped`. |
| `retry` | Starts a new session in the lane with the run's prompt. | Desk may, except after `stopped` or `budget_cap`. |
| `retry_other` | A new session with another agent or model (`args.agent`, `args.model`). | The person. |
| `wait_quota` | Nothing to do: runs resume when the quota resets (`reset_at`). | Anyone. |
| `ship` | Runs the gate and pushes again (`lane ship`). | The person. |
| `widen_scope` | Adds `args.paths` to the lane's scope. Carried out by the Fold, which owns scope leases. | The person. |
| `raise_budget`, `open_mr`, `fix_config_text` | The person's own change. | The person. |
| `open_log` | Reads the log (`args.path`). | Anyone. |

Offered per kind:

| Kind | Fixes |
|---|---|
| `start_failed` | retry, retry_other, open_log |
| `agent_error`, `hung`, `no_change`, `unknown` | continue, retry, retry_other, open_log |
| `quota` | wait_quota, retry_other, open_log |
| `gate_failed`, `standards_failed` | continue, ship, open_log |
| `commits_forbidden`, `push_refused` | continue, ship, open_log |
| `outside_scope` | continue, widen_scope, open_log |
| `merge_refused` | ship, open_mr, open_log |
| `stopped` | continue (person), retry (person), open_log |
| `interrupted` | continue, retry, open_log |
| `budget_cap` | raise_budget, retry (person), retry_other, open_log |

`continue` is offered only when the run has a session to continue.

## Taking a fix

`Runner.ApplyFix(ctx, runID, fixID, by, overrides)` is the one path every client uses.
`by` is `person` or `desk`. The desk is refused any fix that needs the person or is not
marked for it, and may not edit a fix's arguments; the person may (an edited prompt, the
agent and model for `retry_other`). `widen_scope`, `raise_budget`, `open_mr` and
`fix_config_text` are not run by dispatch: the result says they are the person's change
(`done: false`) and the caller carries them out with the fix's op and args.

Taking a fix never approves anything: a run it starts is checked, gated and ended like
any other.

## Seeing and taking them

The same diagnosis and fixes are offered in the API, the CLI, the MCP tools and the web UI.

| Surface | What it does |
|---|---|
| API | `GET /v1/runs/{id}` and `GET /v1/lanes` carry the newest diagnosis (`diagnosis`) on a run or on a lane's latest run. A list leaves `evidence` out and sets `evidence_omitted`. `GET /v1/runs/{id}/diagnoses` returns all of a run's, newest first, with evidence. `POST /v1/runs/{id}/fix` takes `{action, overrides}` and returns the `FixResult`. |
| CLI | `shepherd run why <id>` prints the reason, the evidence and the fixes. `shepherd run fix <id> <action> [--agent A] [--model M] [--prompt P]` takes one; the flags replace the fix's arguments, such as the agent of a `retry_other`. |
| MCP | `run_why` and `run_fix`, mapped onto the two commands. |
| Web | The Lane page, the Run page and the Board show a failed run's reason, its evidence in a disclosure and the fixes as buttons. A fix marked `needs_person` asks for a confirmation that says what it will do. |

Who may take a fix follows the token that asks:

| Caller | May take |
|---|---|
| The operator token and a signed-in browser session (the person) | Any fix, with its arguments edited. |
| The front desk's token | Only a fix marked `desk_ok` and not `needs_person`, as suggested: an edited fix is refused. |
| An agent's worker token | None. The routes are not an agent's, so an agent cannot fix its own run, and it cannot read diagnoses either. |

`widen_scope` is carried out by the daemon through the Fold when the person takes it, and
says so in its result. `raise_budget`, `open_mr` and `fix_config_text` are the person's own
edits: the web UI shows them as a note, not a button, and the API answers that it is theirs.
