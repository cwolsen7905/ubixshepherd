# Merge request thread rules: fix and resolve, each a switch

Status: **proposed**, a plan. The maintainer asked for it in outline: an agent's merge
request should have its open review threads fixed and resolved, as a rule that can be
turned off for repos where something else does the fixes, and enabled now. Two switches,
fix and resolve. This note is the design; the code lands in separate lanes (section 7).

## The problem

Today the watcher hands unresolved threads on an open lane merge request back to the lane's
agent once (`reviewThreads` in `internal/watch/threads.go`, on the `watch.review_threads`
switch and the shared `watch.fix_tries` budget). The prompt says "do not resolve the
threads", so every thread stays open after the fix, and the rule is only half a rule:

| Gap | Effect |
|---|---|
| Nothing marks a thread as handled | A thread the agent fixed and replied to is still unresolved, so it counts as open and can go back to the agent again (it also waits on the person for no reason) |
| The forge reader is read-only | `internal/forge` lists threads but cannot reply or resolve |
| One switch, tied to the retry budget | A repo whose pipeline already fixes threads cannot turn the hand-back off without turning off pipeline fixes |
| Agents differ in what they may run | OpenCode's bash rules allow `glab mr view` and `glab mr merge` only when the repo lets it merge, so a reply or a resolve through `glab` is denied |

The rule that shapes the fix: **an agent may answer and resolve a thread on its own merge
request; approving and merging stay the person's, with either switch on or off.**

## 1. The two switches

| Setting | Label (Settings page) | Default | Meaning |
|---|---|---|---|
| `review.fix_threads` | Fix review threads | on | Shepherd continues the lane's agent to fix each unresolved thread on the merge request it opened, or to reply with why not |
| `review.resolve_threads` | Resolve fixed threads | on | After a fix is pushed, the agent replies naming the commit and resolves the thread. Off: it replies and leaves the thread for a reviewer |

`resolve_threads` does nothing when `fix_threads` is off (the Settings page greys it out
with "needs Fix review threads"). Both are set under `defaults.review` for the workspace
and under a repo to override, the same layering as `watch`. Unset is on, as with
`watch.review_threads` today.

`watch.review_threads` is read as `review.fix_threads` when the new key is unset, with a
warning, so existing configs keep their meaning (`internal/config/retired.go` only ignores
retired keys, so this is a small mapping in the loader). `watch.fix_tries` keeps capping tries per
request, now for both pipeline and thread hand-backs. Neither switch can hand an agent a
different permission: they change what Shepherd asks, not what the repo's `autonomy` allows.

## 2. What "handled" means

A thread is **handled** when it is resolved, or when Shepherd recorded a reply to it that
is newer than every other note on it. Reading it back needs no author check, because the
agent and the person may post as the same forge user:

- Every reply an agent posts through Shepherd (section 4) starts with a `[Shepherd]` line,
  and Shepherd records the thread's id and its last note id in the lane's forge state
  (replacing the id list in `ThreadsSeen`, which holds ids only).
- On each poll a thread is **fresh** only if it is unresolved and either unseen, or has a
  note after the recorded one that does not start with `[Shepherd]`. A reviewer's new
  comment makes it fresh again; the agent's own reply never does.
- Handled and unresolved with `resolve_threads` off is the normal resting state: the watcher
  neither re-sends it nor counts it against the fix budget. It shows on the lane as
  "answered, waiting for a reviewer", not as work for the agent.

## 3. The agent's order of work

For each fresh thread, in the prompt and enforced in Go where it can be:

1. Fix what it asks, run the gate, commit.
2. Push, if the repo lets the agent push; otherwise Shepherd ships the lane (`push:
   shepherd`) or the person pushes. **Resolve only after the commit is on the remote
   branch**; a fix that is only local is replied to as "fixed in `<sha>`, not pushed yet" and
   left open.
3. Reply on the thread naming the commit (or why no change was made).
4. Resolve, only if `resolve_threads` is on and step 2 is done. A thread the agent declined
   to fix is never resolved: the reply stands and the reviewer decides.

Replying always comes before resolving. A resolve with no reply from this run is refused.

## 4. How dispatch does it

Prompt wording alone does not hold across four agents, so the writes go through Shepherd
rather than the agent's shell:

- A worker tool pair, `thread_reply` and `thread_resolve` (`shepherd mcp --worker`, command
  first, then mapping, like the decision tools). The run comes from `SHEPHERD_RUN`; the
  thread must belong to that run's lane merge request.
- The daemon performs the forge call. `thread_reply` adds the `[Shepherd]` marker and
  records the note. `thread_resolve` checks the order: a reply from this run exists, the
  recorded commit is an ancestor of the remote branch head, and `review.resolve_threads`
  is on. Otherwise it answers with the reason and changes nothing.
- `internal/forge` gains `ReplyThread` and `ResolveThread` behind a new optional
  `ThreadWriter` interface, next to `ThreadReader`, using `glab api` like the rest. GitLab
  only for now; GitHub waits with the mirror proofs.

This is why **OpenCode needs no `glab` permission**: its permission block already allows
`shepherd_*` worker tools, and the daemon holds the forge access. No bash rule is loosened
and the push and merge rules are untouched. Claude Code, Copilot and Cursor use the same
tools. An agent that has no worker tools in a run gets no thread prompt: the watcher says
so on the feed and leaves the threads to the person, as it does when there is no
conversation to continue.

## 5. Keep-going

Keep-going (`docs/keep-going.md`) is the desk's switch for starting work between approvals.
Thread fixes are not desk actions: the watcher continues the lane's agent on its own, as it
does for a failed pipeline, and keep-going being off does not stop it. Keep-going's spend
cap and run limits do apply, because a continued run costs like any run; a hand-back held
by a limit is not counted as a try, and goes out when the limit lifts. The keep-going
panel gets one read-only line, "Review threads: fixing and resolving" (or "fixing, not
resolving", or "off"), linking to the repo's Settings, so the person sees what runs without
asking. It has no toggle of its own: one setting, one place.

## 6. Kelpie

Kelpie is a separate uBixCore tool that triages merge request threads with AI. It must not
overlap with this rule, and Shepherd's core names no product (`core-boundary`), so the seam
is the setting, not code:

- A repo with Kelpie (or any pipeline bot) in its pipeline sets `review.fix_threads: false`.
  Shepherd then neither fixes nor resolves threads there. `resolve_threads` is moot.
- A repo that wants Kelpie to triage but Shepherd to fix leaves `fix_threads` on and
  `resolve_threads` off. Kelpie's replies are not `[Shepherd]` notes, so they make a
  handled thread fresh again; that is the intended way for a triage to send it back.
- The shipped default stays on, per the maintainer. A uBixCore pack may set it off for repos
  that run Kelpie; that default lives in the pack, never in the core.

## 7. Implementation lanes

Three lanes keep the overlap small. `feat/next-up-guards` currently holds
`internal/watch/**`, `internal/forge/**` and `internal/dispatch/**`, so lane B and the
dispatch part of C wait for it to merge (lane A touches none of those).

| Lane | Scope | Globs |
|---|---|---|
| A. Config and docs | The `review` block, alias, validation, template, Settings fields | `internal/config/**`, `internal/api/config.go`, `web/src/pages/Settings.tsx`, `web/src/pages/Settings.test.tsx`, `docs/config.md`, `docs/web-settings.md` |
| B. Forge and watcher | `ThreadWriter`, handled state, fresh-thread rule, switches honoured | `internal/forge/**`, `internal/watch/**`, `internal/store/**` (an appended migration for the handled record) |
| C. Worker tools | `thread_reply`, `thread_resolve`, prompt text, OpenCode check | `internal/cli/decision.go` (or a new `internal/cli/thread.go`), `internal/cli/mcp.go`, `internal/cli/mcp_test.go`, `internal/dispatch/**`, `web/src/components/KeepGoing.tsx` and its test |

Order: A, then B once `feat/next-up-guards` lands, then C. Tests use the fake forge and
fake agent script; a real read of one open merge request's threads is a manual check, and
the first real reply and resolve should be on a throwaway merge request.

## Decisions for the maintainer

| # | Choice | Recommendation |
|---|---|---|
| 1 | Writes through worker tools, or the agent's own `glab` | Worker tools: one path for all four agents, with the order enforced |
| 2 | Keep `watch.review_threads` | Retire it as an alias of `review.fix_threads` |
| 3 | GitHub threads | Later, with the mirror proofs |
