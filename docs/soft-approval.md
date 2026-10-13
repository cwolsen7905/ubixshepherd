# Soft approval: an agent review that lets a merge keep moving

Status: **proposed**. The outline was approved by the maintainer (2026-10-11) with three
changes, all folded in below: the review runs in the repo's pipeline (not in Claude Code),
on a cheap model through OpenCode and OpenRouter; a finding becomes a resolvable thread, an
issue or both; and a finding must start another agent so the loop closes. Nothing is
built. Implementation is six merge requests, each its own lane (section 12).

## The problem

Every merge waits for the person. With `autonomy.merge: agent` an agent can arm
merge-when-pipeline-succeeds on its own lane's merge request, but nothing has looked at the
change except the agent that wrote it and the repo's gate. So a repo is either wide open
(the author merges its own work) or it queues behind one human, who is the bottleneck.

The person asked for the middle: let agents approve merges, with a catch. A review must be
done, a comment must be posted saying it is a **soft** approval, and every one must be
logged somewhere so a human can audit them later in a batch.

This keeps "agents never approve their own work" true: the reviewer is **never the author**
(section 1). What changes is that "never approve" becomes "never approve *for the person*":
a soft approval is a recorded agent opinion that lets a merge proceed, and the person
keeps an after-the-fact audit with a way to undo it. It is a change to
[design.md](design.md) §3.7 and [vision.md](vision.md); section 13 lists the lines.

## Decisions

| # | Choice | Options | Recommendation |
|---|---|---|---|
| 1 | Reviewer independence | the prompt / the daemon | The daemon: the author's run never sees the review tool, and a review is valid only if its agent or model differs from the author's |
| 2 | Where the review runs | a daemon-started run / a pipeline job in the repo | A pipeline job, shipped as a CI include for every repo: it is outside the author's process, blocks merge-when-pipeline-succeeds natively and also reviews people's merge requests |
| 3 | Reviewer runtime | Claude Code / OpenCode on a cheap hosted model | OpenCode with an OpenRouter model, read-only, from the models allow-list ([opencode.md](opencode.md), [models.md](models.md)) |
| 4 | Verdicts | approve/reject / three | `soft_approve`, `request_changes`, `escalate` |
| 5 | A finding becomes | thread / issue / both | Both, by severity: a blocking finding is a resolvable thread on the diff, a follow-up is an issue; the daemon starts the fixing agent from either (section 5) |
| 6 | Forge approval | comment and checks only / the person's real Approve / a bot account's Approve | Comment, threads and a required job, posted by a project bot. Never the person's Approve, because `glab` is the person's account |
| 7 | Audit log | forge comments only / the daemon's store | The store, an appended migration, with a human-audit state per row |
| 8 | Config | per repo / per workspace | Per repo, `autonomy.approve: human \| shadow \| agent`, beside push and merge; default `human` |
| 9 | Rollout | straight to `agent` / `shadow` first | `shadow` first: the review runs and is logged, but never gates or merges |
| 10 | What a review can never decide | prompt rules / daemon rules | Daemon rules (section 9): code lists the stops, a model can add a stop and never remove one |
| 11 | Limits | none / caps | Per-review and daily spend, a request-changes cycle cap, a daily count (section 10) |

## 1. The reviewer is never the author

Rules, all checked in Go, none by the prompt:

- **A separate process.** The review is a pipeline job on a runner, a new session with no
  memory of the author's. The author's run is a local CLI process; it has no tool that
  starts, edits or answers a review. `mr_review` tools exist only on the person's and the
  desk's side, and the worker tool set (`shepherd mcp --worker`) has none.
- **A recorded difference.** The author's agent and model are known to the daemon (the
  run record). The job writes its own agent and model into its result. The daemon marks a
  row `independent` only when the reviewer's model, and by default its agent too, differ
  from the author's. `approve.reviewer.require: agent | model | none` sets how strict:
  `model` is the default and `none` is for a repo with one provider, and is shown in the
  log as "same model as author".
- **A review of a stale commit counts for nothing.** The result names the head SHA it
  reviewed. A new commit invalidates it, so every push is re-reviewed.
- **The author cannot edit the reviewer away.** The job comes from an `include` of a
  template that is not in the lane's scope. A change to CI configuration, to the review
  settings or to a standards pack is an always-human stop (section 9), and the push hook
  already refuses files outside the lane's scope.

## 2. Where it runs, and on what

**The job.** `shepherd-review` is a job in the repo's pipeline, in its own stage after the
gate's jobs, `needs` them, and it runs for every merge request pipeline. It does not
replace the repo's gate: the gate result is an input.

| Why a pipeline job | What it gives |
|---|---|
| It sits where merge-when-pipeline-succeeds already waits | An author that armed the merge before the review ran cannot beat it: a failing review job holds the merge (section 5) |
| It is not the author's process | Independence is structural, not a promise |
| It needs no running daemon | The review happens when the person's machine is off |
| It reviews anyone's merge request | A human's change, a dependency bot's, a lane's: one path, one log |

The cost is that a job has less context than the daemon. It sees the diff, the repo and
the pipeline; it does not see `~/.shepherd` packs, the lane's scope or its plan. The daemon
supplies none of that and does not need to: scope is already enforced at push by the hook,
mechanical rule checks already run at ship, and what a pack lists in the repo's own
`.shepherd/standards`, `AGENTS.md` and `CLAUDE.md` is in the checkout. Rules that live only
in a home pack ([standards-packs.md](standards-packs.md)) are not seen by the job: put the
ones that matter for review in the repo's pack, or in a pack checked out by the template.
The plan comes from the merge request's description, where the lane's brief and plan are
written when the lane opens a merge request, and it can only ever add a finding.

**The runtime.** Claude Code is the wrong tool for this job: it is a full agent with a
subscription and a permission model built for writing. The job runs OpenCode headless
with an explicit permission block (read, `git diff` and `git log` only; no edit, no
network but the model's) and a model chosen for cost per review:

```yaml
approve:
  reviewer:
    agent: opencode
    model: openrouter/deepseek/deepseek-v4-flash   # example; check ids and prices first
    require: model
```

The model must be in the `models:` catalogue's allow-list for the repo
([models.md](models.md)), and an OpenRouter model's cache-read price matters because a
review reads the same repo context many times ([opencode.md](opencode.md)). The job uses a
dedicated OpenRouter key with a credit limit set at OpenRouter: that, not the job, is the
hard ceiling on spend. The key is a masked CI variable; section 11 states the risk.

**The alternative, a daemon-started review run**, is the same record contract with a
different executor: the daemon runs OpenCode in a fresh checkout and has full context. It
is for a repo with no runners and is not in the six merge requests; the result rows are
identical, so it can be added later.

## 3. What the review covers

| Input | From | What the reviewer judges |
|---|---|---|
| The diff | `git diff <base>...HEAD` on the merge request | Correctness, regressions, tests for changed behaviour, secrets, unrelated churn |
| The gate | the pipeline's own jobs for this SHA | A failed or missing gate is not reviewed: it fails the job at once (section 9) |
| Standards | the repo's pack and docs; each rule's `applies_to` and `level` | Each applicable `must` and `should` rule, one finding per breach, with its id. Closes the roadmap gap "a post-lane review run that judges them is not built" ([roadmap.md](roadmap.md)) |
| Plan vs built | the merge request description (brief, plan, scope) | Work the plan did not name, and plan items the diff does not do |
| Scope | the diff's paths against the described scope | A path outside it is a stop, not a finding |

`lane_review` today only classifies worktrees as finished, live or unclear
(`internal/cli/lane.go`); it is not a code review and is untouched. The new surface is
`shepherd review` and the `mr_review_*` tools.

## 4. Verdicts and the comment

| Verdict | Meaning | Effect |
|---|---|---|
| `soft_approve` | No blocking finding, no stop | The job passes; the merge may proceed under the repo's merge rule |
| `request_changes` | At least one blocking finding | The job fails with threads on the diff; the author's lane is continued with them (section 5) |
| `escalate` | A stop applies, or the reviewer cannot judge | The job fails; the daemon raises a decision for the person |

The reviewer posts one summary note per run. The first line is fixed so the daemon and
people can find every one by a search:

```text
Soft approval (agent review, pending human audit)

Verdict: soft_approve | request_changes | escalate
Commit: <head sha>
Reviewer: opencode, openrouter/<model>, job <id>, pipeline <id>
Author: <agent>, <model>, run <id>   (independent: yes | no, why)
Findings: 0 blocking, 2 advisory, 1 follow-up

<findings, each with id, severity, file:line, one-line statement, rule id if any>

Audit: unreviewed. A human can accept or flag this in `shepherd review`.
<!-- shepherd-review: {"v":1,"sha":"...","verdict":"...","fingerprints":[...]} -->
```

`request_changes` and `escalate` use the same header but say so on the verdict line; the
word "Soft approval" is kept in the header on all three so one query finds every review.
The machine-readable block is a convenience: the daemon's ingest reads the job's result
artifact and its status from the forge, never the note's text (section 7), because the
comment is posted through an account an author could also use.

## 5. Findings: threads, issues and the loop

| Severity | Becomes | Blocks the merge | Starts |
|---|---|---|---|
| `blocking` | an inline, resolvable discussion on the diff | Yes: the job fails, and the forge's "all threads must be resolved" setting holds it too | the author's lane, continued with the thread ids |
| `advisory` | a line in the summary note only | No | nothing |
| `follow_up` | an issue labelled `shepherd:follow-up`, linked to the merge request, deduped by fingerprint | No | a Next-up item (`planned`) the person approves |

**Why both a thread and an issue.** A thread is where the author already looks and it
holds the merge; it dies with the merge request. An issue outlives it and is the right
home for "this is wrong but not in this change". Using one for both loses either the block
or the memory.

**The loop.**

1. The author's lane arms merge-when-pipeline-succeeds (`autonomy.merge: agent`). The
   pipeline includes the review job, so the merge waits for it.
2. The job finds a blocking problem, posts threads, fails. The merge does not happen.
3. The watcher sees a failed review job as it sees a failed pipeline
   (`internal/watch`) and continues the author's session with the findings, the same
   handoff, counted separately by `approve.max_cycles` (section 10).
4. The author fixes and pushes: a new SHA, so a new pipeline and a new review.
5. The reviewer re-reads its own threads (matched by fingerprint), resolves the ones the
   change fixed and leaves the rest. A pass ends the loop; the merge proceeds.

Two rules keep this honest. **The author never resolves a review thread**: that stays the
rule in [design.md](design.md) §3.7. Only the reviewer resolves its own threads, on a new
SHA, and each resolution is in the log. And **a new commit disarms trust**: before the
daemon arms or re-arms a merge it checks the review's SHA is the merge request's head,
and passes that SHA to `glab mr merge --sha`, so the forge refuses a merge of anything
else.

A `follow_up` issue is turned into a Next-up item by the daemon with the issue linked
([backlog-next.md](backlog-next.md)). It is `planned`, never started unasked, as for any
item the desk thinks of; keep-going starts it only after the person approves it.

## 6. Forge approval

| Option | What it is | Verdict |
|---|---|---|
| Comment, threads, required job; no Approve | The merge is allowed by a job passing and no open thread. The forge's approval count is untouched | **Recommended.** Honest, needs no extra identity, and works with merge-when-pipeline-succeeds |
| The person's real Approve | `glab mr approve` as the person | Rejected. It would be the person's name on an agent's judgment, and defeats the audit |
| A bot account's Approve | A project access token user approves | Later, for repos whose rules require an approval. Counts only if the forge accepts a bot as an approver, which must be checked per forge and plan; it is a separate account so the record stays honest |

**A consequence to state plainly.** A repo whose rules require one or more human
approvals still needs a human's click: a soft approval does not satisfy that rule and is
not meant to. Soft approval is for repos where merging is gated by pipelines and threads,
as `autonomy.merge: agent` already assumes.

Comments and threads are posted by the job's project access token, a bot, so they do not
look like the person wrote them. The daemon's own comments (a handoff, an issue link) use
the person's `glab` as every Shepherd write does today and say "Shepherd" in their text.

## 7. The audit log

One table in the daemon's store (an appended migration), one row per review of a SHA,
whatever its verdict: the person audits requests for changes too, because a reviewer that
is wrong in that direction wastes time.

| Column | Meaning |
|---|---|
| `id`, `repo`, `mr_iid`, `lane`, `head_sha` | what was reviewed |
| `pipeline_id`, `job_id`, `reviewed_at` | where it ran |
| `author_run`, `author_agent`, `author_model` | the author, from the run record |
| `reviewer_agent`, `reviewer_model`, `independent`, `independence_note` | the reviewer and whether it was valid |
| `verdict`, `findings` (JSON), `n_blocking`, `n_advisory`, `n_follow_up` | the result |
| `stops` | the always-human rules that applied, if any |
| `cost_usd`, `cycle` | spend, and the request-changes cycle number |
| `merged`, `merged_at`, `merge_sha` | what came of it, filled in by the watcher |
| `audit_state`, `audited_by`, `audited_at`, `audit_note` | `unreviewed`, `accepted` or `flagged` |

**How a row gets there.** The watcher polls a lane's merge request pipelines and, for a
`shepherd-review` job that has finished on the head SHA, reads the job's status and its
result artifact from the forge. It does not trust the note: the status and artifact come
from the runner, and the job is protected (section 1). It then validates in Go: the SHA is
the head, the model is in the allow-list and equals the profile's, no stop applies, the
result is independent as the repo requires. A result that fails validation is recorded
with `verdict: invalid` and is never a soft approval.

| Surface | What |
|---|---|
| CLI | `shepherd review list [--state unreviewed] [--repo r] [--verdict v] [--json]`, `show <id>`, `accept <id> [--note]`, `flag <id> --why "..." [--lane \| --next]`. A client of `GET/POST /v1/reviews`; no command opens the store |
| Web | A Reviews page ([web-chat.md](web-chat.md) pages are the model): a table with state filters, the diff link, findings, whether it merged, and per-row accept and flag. A reviewer-by-flag-rate summary shows which model's approvals the person later flags |
| Desk and MCP | `review_list` and `review_show` read; `review_accept` and `review_flag` are refused outside a turn the person started, the rule `decision_answer` follows. The desk can say "three unreviewed, one merged with a blocking advisory" and never audit them |

**Flag** records why and, by the person's choice, opens a follow-up lane (a revert or a
fix, against the merged SHA) or a Next-up item. **Accept** closes the row. A bulk accept
exists for rows with no findings and `independent: yes`, and never for one with a stop.

## 8. Config

Per repo, beside push and merge, with the rest in a block:

```yaml
repos:
  api:
    autonomy:
      push: agent
      merge: agent
      approve: shadow          # human (default) | shadow | agent
    approve:
      reviewer: {agent: opencode, model: openrouter/<model>, require: model}
      sensitive: ["migrations/**", "**/auth/**", ".gitlab-ci.yml", ".shepherd/**"]
      max_diff_lines: 800
      max_cycles: 2
      cost_cap: 1.00           # USD per review
      daily_max: 20            # soft approvals per repo per day
```

| `autonomy.approve` | Review job | Gates the merge | Soft approval counts |
|---|---|---|---|
| `human` (default) | not required | no | no; nothing is logged |
| `shadow` | runs, `allow_failure` | no | no; every review is logged so the person can judge its quality |
| `agent` | required | yes | yes |

How the three autonomy settings combine:

| push | merge | approve | Result |
|---|---|---|---|
| agent | agent | agent | The lane pushes, arms merge, the review gates it, the log records it |
| agent | agent | human | Today's behaviour: the author may arm; the review, if any, is advisory |
| any | human | agent | The review posts and is logged, and a human merges: soft approval informs, it does not merge |
| human | any | agent | Refused at validation: nothing opens a merge request |

`internal/config` validates the block, including that `approve: agent` needs
`autonomy.merge` set and a reviewer in the models allow-list. `shepherd review init`
(a person's step) prints the CI include and the variables to set; `repo add` and `doctor`
check the job exists in the repo's pipeline and that the project requires resolved
threads, and `approve: agent` is refused until both hold. The job reads its model and
caps from CI variables, and the daemon compares them with the profile on every ingest, so
a CI edit cannot quietly widen them.

## 9. What always stays with the person

A stop is computed by Go, twice: by the job, so the merge is physically held, and by the
daemon on ingest, so it is recorded and raised as a decision. A stop is evaluated before
the model is asked, and the model may add a stop but never remove one
([design.md](design.md) §3.7).

| Stop | Why |
|---|---|
| Tags, releases and deploys | `autonomy.tag`, `autonomy.deploy`, "agents never tag or publish unasked" |
| A change to a protected branch's rules, CI configuration, a standards pack or `approve:` settings | It is how the guard would be weakened |
| A path in `approve.sensitive`, or one the repo marks protected | The person's list of what is not for an agent's opinion |
| A failed, missing or stale gate | There is nothing to soft-approve |
| A file outside the lane's declared scope | The push hook refuses it; a stop here covers a merge request not from a lane |
| A diff over `max_diff_lines` | A cheap model's judgment on a large change is not worth the name |
| Reserved decisions: money, published promises, destructive migrations | [design.md](design.md) §3.7: detected by code |
| A reviewer that is not independent as the repo requires | Section 1 |
| A review of a SHA that is no longer the head | Section 5 |
| Anything an unanswered decision on the lane still holds | The decision is the person's |

A stop is not a failure: the review may still post findings, and the merge request waits
for a person, who sees it in the dock and the decision list as today.

## 10. Safety limits

| Limit | Default | Setting | Effect |
|---|---|---|---|
| Spend per review | $1.00 | `approve.cost_cap` | The input is bounded by `max_diff_lines`; a job over its cap fails as `escalate`. The OpenRouter key's own credit limit is the ceiling |
| Request-changes cycles per merge request | 2 | `approve.max_cycles` | The third becomes `escalate`: a decision, not another loop |
| Soft approvals per repo per day | 20 | `approve.daily_max` | Past it, results are logged but not counted, and the repo falls back to `human` until the next day |
| Review spend per day | the daemon budget | the existing `daemon.budget` | Crossing it turns the repo to `shadow` for the day and says why in the feed |
| Fix starts from reviews | with the keep-going loop guard | `keep_going.lane_starts` | A lane that keeps failing review is handed back |

**Keep-going.** A review is a pipeline job, so it runs with or without keep-going. What
keep-going adds is the desk starting the follow-up work: approved Next-up items from
`follow_up` issues, under the same caps ([keep-going.md](keep-going.md)). It cannot
raise a cap, turn `approve` on, accept or flag a row, or answer an `escalate`. The
request-changes handoff is the watcher's, like a failed pipeline's, and is capped by
`approve.max_cycles`, not by keep-going's flag.

## 11. How it shows

| Where | What |
|---|---|
| Merge request | The header note, inline threads and the `shepherd:follow-up` issues, all from the bot |
| Dock and web lane view | A review badge next to the gate and pipeline badges: `soft-approved`, `changes requested`, `escalated`, `shadow`, with the cycle number |
| Feed | A closed kind, `review`: reviewed (verdict, cost), handed back to the author, escalated, merged on a soft approval, audited (accepted or flagged) |
| Reviews page and `shepherd review list` | The audit backlog, unreviewed first; the count is a badge so it cannot be forgotten |
| `shepherd status` | Unreviewed count, soft approvals merged today, spend |

## 12. Sequencing

Six merge requests, each green alone and each its own lane. 1 and 2 are independent.

1. **Forge reads and writes and the record contract**: MR notes, discussions (create,
   resolve), issues, pipeline jobs and artifacts in `internal/forge`; the result schema
   and the stop and independence checks as pure functions in a new `internal/review`,
   tested with fixtures. Scope: `internal/forge`, `internal/review`.
2. **Store and config**: the appended migration and store methods; `autonomy.approve` and
   the `approve:` block with their validation and the allow-list check. Scope:
   `internal/store`, `internal/config`.
3. **Daemon ingest and the loop**: watcher ingest and validation, the author handoff on
   `request_changes`, the `escalate` decision, follow-up issues to Next-up items, merge
   arming with `--sha`, the cost and daily caps, `GET/POST /v1/reviews`. Depends on 1 and 2.
   Scope: `internal/watch`, `internal/daemon`, `internal/dispatch`, `internal/api`.
4. **The pipeline job**: the CI include, the reviewer prompt, the OpenCode permission
   block, the stops and the artifact writer, with a fixture-repo test. Depends on 1.
   Scope: `ci/` (new), `docs/`.
5. **CLI, MCP and the desk**: `shepherd review`, `review init`, the MCP tools with the
   origin rule, the `review` feed kind, the `DeskBrief` paragraph, `repo add` and `doctor`
   checks. Depends on 3. Scope: `internal/cli`, `internal/client`, `internal/desk`,
   `internal/chat`.
6. **Web**: the Reviews page, the lane badge and `docs/web-chat.md`. Depends on 3.
   Scope: `web/`.

Rollout: land all six, set one repo to `shadow` for a few weeks, read the log, compare
flagged rates, then set `agent` there. No repo is `agent` by default.

## 13. Docs to change when this is accepted

These say the opposite today, so they change in the same release as MR 3 of this plan
(section 6: the daemon ingest and the loop; not the third merge request of the Next-up
list in [backlog-next.md](backlog-next.md)), not before:

| File | Line | Change |
|---|---|---|
| `docs/design.md` | §3.7 table, "merge sign-off: always; agents never approve" | Add: a soft approval is not sign-off; the person's audit is |
| `docs/design.md` | "No setting lets an agent approve" under the autonomy table | Add the `autonomy.approve` row and keep "never the person's approval" |
| `docs/vision.md` | "Not a replacement for review", last sentence | "never gives that sign-off itself" stays true; add soft approval and its audit |
| `docs/roadmap.md` | standards-packs row, "a post-lane review run ... is not built" | Point to this document |
| `CLAUDE.md` | "Agents never approve their own work" | Unchanged in meaning; add "a soft approval is by a different reviewer and is audited" |
| `docs/open-questions.md` | a new numbered question | The decision log for the table above |

## Out of scope

- A real forge Approve, by the person or by a bot; section 6 names the later option.
- A daemon-started review run for repos without runners (the same record contract).
- GitHub: the forge reads in MR 1 are GitLab's; GitHub follows the mirror-proof work.
- Letting any agent tag, release, deploy or answer a decision on a soft approval.
- Auto-approving `follow_up` items into lanes; they are `planned` until the person says.
- Training or tuning a reviewer from the audit; the log only reports flag rates.

## Risks

- **A reviewer's blind spots.** A cheap model misses things, and a soft approval can merge
  a bug. The log, `shadow` first, per-model flag rates and the stops are the defence;
  `human` is one setting away.
- **Rubber-stamping the audit.** The person bulk-accepts and the log becomes a ritual. The
  bulk action excludes stops and findings, and flag rates are shown per reviewer.
- **A secret in CI.** The OpenRouter key is a CI variable and a pipeline of a branch with
  a CI change could read it. A lane's scope excludes CI files, CI changes are a stop, and
  the key has a credit limit; those bound it, they do not remove it.
- **Same model, same blind spot.** An author and a reviewer from one vendor share failure
  modes. `require: agent` is available, and the log records `independent` plainly.
- **Cost surprises on a large diff.** `max_diff_lines`, the per-review cap and the key's
  limit exist because a hosted model's real bill can differ from OpenCode's estimate
  ([opencode.md](opencode.md)).
- **A comment is not proof.** Anyone with note access can post the header. The daemon
  trusts only the job's status and artifact, and says so, so a pasted header changes
  nothing.
