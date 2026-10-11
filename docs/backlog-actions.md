# Web chat: acting on the backlog

**Status: proposed.** Nothing here is built. It extends the backlog rail described in
[web-chat.md](web-chat.md) and needs the maintainer's approval before any code is written.

## The problem

In the chat page's backlog rail, a row's name is a link. For a decision it leads to the
Decisions page, for a request to the Decisions page's list, and for everything else to the
lane's page. The lane's page shows runs, a timeline and facts, and offers almost nothing to
do: no way to continue a run, stop one, nudge an idle lane, route a request or close a lane.
The person leaves the conversation, finds a read-only page, and has to go back to the
terminal or ask the front desk in words.

The goal is the opposite: **the person stays on the chat page and acts on the backlog from
there**, with the lane's page offering the same actions rather than being a dead end.

## The flow

1. Click a row in the rail. The page stays where it is.
2. An **action card** is added to the thread. It says what the item is, why it is waiting
   and for how long, then offers the likely actions as buttons, the recommended one marked
   with the word "recommended". It also has a reply box and a "details" link to the lane's
   page.
3. A button calls the daemon's API directly. Nothing goes through the front desk, so a
   card is instant and costs nothing.
4. When the action lands, the card collapses to a one-line receipt ("You stopped run 8").
   When the item is gone (resolved in the terminal, say), the card says so and its buttons
   are disabled.

The card is built in the browser from the live snapshot the page already holds (lanes, runs,
decisions, requests, the feed). Opening one starts no desk turn.

## What exists today

| Piece | Where | Note |
|---|---|---|
| Backlog items and groups | `web/src/model/backlog.ts` | `needs`, `broken`, `idle`, `working`, `review`; each item carries its lane, run, decision or request |
| The rail | `web/src/components/Backlog.tsx` | name is a link; "in thread" jumps to the latest row about the work |
| An in-thread decision form | `InlineDecision` in `web/src/pages/Chat.tsx` | options with "recommended", own words, calls `Live.answer` |
| A run's diagnosis and fixes | `web/src/components/RunFailure.tsx` | takes a fix through `POST /v1/runs/{id}/fix`, with a confirmation for those marked `needs_person` |
| Dismiss and undismiss a run | `Live.dismiss`, used by the board | `POST /v1/runs/{id}/dismiss` |
| The thread | `DeskThread`, `timeline()` in `web/src/state/desk.ts` | desk events merged with typed feed rows in time order |

## Cards by item kind

"Recommended" is a rule in a pure function (`actionsFor`, below), never a guess made at
render time. Where no rule applies, nothing is marked.

| Item | The card says | Buttons (recommended first) | Calls |
|---|---|---|---|
| **Decision** (needs you) | the question, the agent's reason, who asked, waiting how long | each option, **Answer** after picking one; **Ask the desk about this**; details | `POST /v1/decisions/{id}/answer` |
| **Request needing routing** (needs you) | kind, from which lane and agent, the message, the note saying why no rule routed it, waiting how long | **Route to lane** (a lane picker, and an optional agent), recommended when the request names an open lane; **Close request** with a reason; ask the desk; details | `POST /v1/requests/{id}/route`, `POST /v1/requests/{id}/close` |
| **Failed or interrupted run** (broken) | run, agent, how it ended (exit code, error), the diagnosis summary, ended how long ago | the diagnosis's first fix that Shepherd can carry out (recommended), **Continue** with an editable prompt, **Dismiss run**, open log, ask the desk, details | `POST /v1/runs/{id}/fix`, `POST /v1/runs` with `continue`, `POST /v1/runs/{id}/dismiss` |
| **Failed pipeline** (broken, run fine) | the merge request, the pipeline, failed how long ago, the diagnosis when the lane has one | **Continue the agent** with a prompt naming the failed pipeline (recommended), open pipeline, open MR, ask the desk, details | `POST /v1/runs` with `continue` |
| **Idle lane** (idle) | what it last did and when ("last: commit, 3 days"), whether it has an open MR | **Nudge** (recommended when the last run succeeded with commits and nothing was shipped), **Close lane** (recommended when its MR is merged or closed), ask the desk, details | `POST /v1/runs` with `lane_id` or `continue`, `POST /v1/lanes/{id}/close` |
| **Running** (working) | the progress line, how long it has run, which agent | **Stop run** (never recommended: nothing is needed), open log, ask the desk, details | `POST /v1/runs/{id}/stop` |
| **To review** (review) | the MR, the passed pipeline, waiting for a merge how long | **Open MR** (recommended), open pipeline, ask the desk, details | none: links only |

Notes on the table:

- **To review has no merge button.** Shepherd never merges and agents never approve their
  own work; the person merges in the forge. The card is a shortcut to it.
- **A lane that has never run** has no session to continue, so its idle card offers only
  close lane, ask the desk and details. Starting a first run needs an agent and a task and
  is left for later.
- **A run that is not continuable** (still running, or with no session recorded) shows
  Continue disabled, with the reason as text beside it, not hidden.
- **Quota holds** read from the same daemon status the rail already polls: an agent held
  for quota shows the hold and the reset time on its cards, since Continue would wait.
- **Default prompts** for Continue and Nudge are plain sentences the person can edit before
  sending ("Continue where you left off.", "Where does this stand? Carry on with the task,
  or report done or blocked."). They are sent as the person's words, so they are shown in
  the box, never added invisibly.

## The API

Every endpoint the cards need already exists and is open to a browser session (the guard
lets a web session call every route the front desk may). The gap is on the client.

| Action | Endpoint | Web client today |
|---|---|---|
| Answer a decision | `POST /v1/decisions/{id}/answer` `{answer}` | `api.answer`, `Live.answer` |
| Dismiss or undismiss a run | `POST /v1/runs/{id}/dismiss`, `.../undismiss` | `api.dismissRun`, `Live.dismiss` |
| Take a diagnosed fix | `POST /v1/runs/{id}/fix` | `runFailures.fix` |
| Stop a run | `POST /v1/runs/{id}/stop` (202) | **missing** |
| Continue a run, or run in a lane | `POST /v1/runs` `{continue \| lane_id, prompt}` | **missing** |
| Close a lane | `POST /v1/lanes/{id}/close` `{force}` | **missing** |
| Route a request | `POST /v1/requests/{id}/route` `{lane, agent?}` | **missing** |
| Close a request | `POST /v1/requests/{id}/close` `{why?}` | **missing** |
| Message the desk | `POST /v1/desk/turn` | `DeskThread.send` |

The new wrappers go in their own exported object beside `runFailures` and `daemonInfo` in
`web/src/api/client.ts`, so the poller's fakes need not carry them, as that file already
does for the others. After a successful call the card asks `Live` to refresh, as
`Live.answer` and `Live.dismiss` do.

What the daemon cannot say today, and what the cards do instead:

| Missing | Effect | Handling |
|---|---|---|
| Whether a lane's worktree is dirty or its branch unmerged | the card cannot preview a close | try without force; the daemon's refusal is the preview (see confirmations) |
| How many fix retries Shepherd has spent on a lane's pipeline (`fix_tries` is stored but not in the lane view) | the failed-pipeline card cannot say "Shepherd has tried 2 of 3" | say only that Shepherd retries on its own up to a cap; adding the field to the lane view is a small, separate change |
| Whether an answer reached the agent at once or waits for its run to end | the card could mislead | the decision's `answer_run` is 0 while held: say "recorded; the agent gets it when its run ends" |
| Any record of who did a thing, or that the desk should know | the desk is not told (see below) | phase 3 |

## Attribution: the person's own words

How an answer is attributed today, from the code:

- A browser session is a principal with the web role and `Human: true`. The answer route is
  open to it, and `personsAnswer` refuses only a front-desk token that is not for a turn
  the person started (the wake-ups Shepherd starts itself).
- So a button click or typed words on a card, sent with the session's CSRF token to
  `POST /v1/decisions/{id}/answer`, is recorded as the person's answer, by capability.
- The store keeps the answer text and the time. It keeps no "answered by". The agent is
  told "The person answered your question" with those words.
- The front desk can also record an answer in a turn the person started
  (`decision_answer`), and the store cannot tell that from a click.

Rules for the new cards, each of which gets a test:

1. **A decision is answered only from the person's click or Enter on the card**, through
   `Live.answer`, with the option's exact text or the typed words, trimmed and nothing else.
   No code path from a card, a prefill or the desk's reply answers a decision.
2. **Typed words on a decision card are the answer**, verbatim. They are not sent to the
   desk. "Ask the desk about this" is a separate button.
3. **Ask the desk never fills in an answer.** Its prefill is context phrased as a question
   ("About decision 5 from lane x: ..."), and it goes into the composer for the person to
   edit and send. It is not sent on click. The desk's own brief already says to record an
   answer only with the person's own words.
4. **Choosing an option does not send it.** Picking selects, **Answer** sends, as the
   in-thread form works today. One click to answer an agent is too easy to do by accident.

### Telling the desk

Today the desk is not told when the person acts on the page. It wakes for runs ending, a
decision asked and a request needing routing, but not for a decision answered, a lane
closed or a request routed or closed. Stopping a run does wake it (the run is interrupted).
So after a card action, the desk would learn only by reading state through its tools.

The plan to fix that is phase 3, done in the daemon so a client cannot forget or forge it:

- Add a desk event kind `action`, origin human, to the closed set. The handlers for answer,
  stop, continue, close lane, route, close request and dismiss append one when the caller
  is a browser session, with a short redacted text ("answered decision 5: Option B").
- It shows in the thread as a typed row, in history, in other tabs and in `shepherd chat`.
- It does **not** start a turn and costs nothing. The desk holds it and puts it at the head
  of its next turn's prompt: "[Shepherd] Since your last turn the person did this from the
  web page: ...". (Today the desk's pending events drain only into wake-up digests, so this
  needs its own queue.)
- Optional and separable: an appended migration giving a decision an `answered_via`
  ("web", "cli", "desk"), so a receipt and a test can say how it was answered.

An older web build against a newer daemon shows an unknown event kind as a note (the
thread already does); a newer one against an older daemon keeps its receipts local.

## How a card lives in the thread

- **Placement.** A card is a thread item stamped with the time of the click, merged by
  `timeline()` with desk events and feed rows. It appears at the bottom, the page follows
  to it (the thread's follow-bottom behaviour), and focus moves to its heading.
- **Keyed by subject, not by row.** A card is about a decision, a request or a lane, and
  shows whichever backlog item currently matches. If an idle lane gets a run, the same card
  becomes the running card and says "now working" in place; its buttons are always derived
  from current state, never cached, so a stale button cannot fire the wrong action.
- **One card per subject.** Clicking the row again scrolls to and flashes the existing
  card (the rail's jump and flash), expanding it if collapsed. At most five open cards;
  older ones collapse to their receipt line.
- **Decisions reuse the form that exists.** A decision's card is `InlineDecision` with the
  waiting time added and "Ask the desk" and "details" beside Answer. There is one answer
  form and one attribution path, not two. When the decision's own row is loaded in the
  thread, the row click jumps to that form instead of adding a second one; otherwise (and
  it is already so for unplaced decisions) the form sits at the bottom.
- **Saved?** In phases 1 and 2 a card is local: its subject and receipt are kept in
  `sessionStorage`, so a reload of the tab keeps them, and nothing is written to the
  daemon's history. From phase 3, what was done is a daemon-recorded `action` row and
  survives everywhere; the card itself stays local, since it is a view of live state.
- **Resolved elsewhere.** The snapshot refreshes every few seconds and on any feed item.
  When the subject has left the backlog, the card shows "No longer waiting" with what the
  data says now ("decision answered", "run 9 ended", "lane closed") and disables its
  buttons. A call that races it comes back refused (the daemon says "request 3 is closed
  already", for instance): the card shows that text, refreshes, and re-renders.
- **After acting.** The card collapses to a receipt, with **Undo** only where an inverse
  exists (dismiss has one; answer, stop and close do not). A failed call leaves the card
  open with the daemon's message in an alert, and the typed words kept.

## Confirmations

| Action | Confirmation |
|---|---|
| Answer, route, dismiss, continue, nudge | none beyond the click itself; answer already needs select then send. Continue and nudge show their prompt in an editable box first |
| Stop run | one inline confirm: "Stop run 8? The agent is asked to end and is killed after ten seconds. Work in the worktree stays; you can continue the session later." |
| Close request | a reason box, optional, then one confirm |
| Close lane | one confirm that states what happens (removes the worktree, keeps the branch unless git sees it merged), sent without force. If the daemon refuses (uncommitted changes, branch not seen merged), its message is shown verbatim and a second, separate confirm appears: "Close anyway: discards uncommitted changes in the worktree" with focus on Cancel |

Confirms are inline in the card, not a modal, so the thread stays readable and the
existing "Start fresh?" pattern in the page header is the model. **Escape** closes the
open confirm first and the card second.

## Keyboard and accessibility

- Rail rows become buttons (name as the accessible label, with the group's glyph and its
  word, as now). Enter or Space opens the card. The **details** link beside it carries the
  lane link the name used to be, so it still opens in a new tab.
- On open, focus moves to the card's heading (`tabIndex=-1`); tab order inside is natural
  document order. **Escape**, or the card's close button, removes the card and returns
  focus to the row that opened it when it still exists.
- Digits 1 to 9 pick a decision's options only while focus is inside that card and not in a
  text field. There are no global shortcuts on the chat page, where the composer takes
  every key.
- "Recommended" is a word, never colour alone, as the option badge is today. Groups keep
  their glyph plus label. Disabled buttons carry their reason as visible text.
- Receipts and errors use `role="status"` and `role="alert"`, as the in-thread form does.
- **Live-region risk.** The thread is a polite live region, so a card added to it is
  announced whole. Moving focus to its heading is the intended announcement; this needs a
  manual check with VoiceOver before it ships. The fallback, if it is too noisy, is to
  render cards in a labelled non-live region directly above the composer, giving up
  chronological placement.
- Targets are at least 44px tall on touch screens. `prefers-reduced-motion` turns off the
  flash.

## Narrow screens

On a narrow screen the rail sits above the thread, so a clicked row is far from its card.
The page therefore scrolls to the card on open, as it does for a desk reply, and the
"Jump to latest" pill still works. The card is full width, its buttons wrap and stack,
the reply box is full width, and the confirm panel sits under the button that asked for it.
The rail's open or closed state is left alone.

## The lane's page

The lane's page gets the same panel at the top, built from the same component the card
uses: the lane's current backlog item, with its actions. It replaces the paragraph that
only links to waiting decisions. For a closed lane there are no actions and the page says
so. The lane's page also links back, `Open in chat`, to `#/chat?card=lane:12` (decision and
request subjects likewise), which opens that card on load; this is the one router change,
a query parameter on the existing chat route, no new route.

## Files

New:

- `web/src/model/backlogActions.ts`: the pure `actionsFor(item, context)`: title, why,
  age, actions with their recommended flag, confirm text, disabled reason, the prefill for
  "Ask the desk". All rules live here.
- `web/src/components/BacklogCard.tsx`: the card, and `ItemActions`, the body shared with
  the lane's page.
- `web/src/state/cards.ts`: open cards by subject, in `sessionStorage`, with receipts.
- Tests beside each (below).

Changed:

- `web/src/pages/Chat.tsx`: render card items, row click handler, deep link, focus.
- `web/src/components/Backlog.tsx`: row becomes a button, **details** link.
- `web/src/state/desk.ts`: `timeline()` merges card items (and its test).
- `web/src/pages/Lane.tsx`: the panel.
- `web/src/api/client.ts` and `types.ts`: the wrappers and the close and start bodies.
- `web/src/router.ts` and its test: the chat route takes an optional `card` parameter.
- `web/src/styles/chat.css` (or a new `card.css`): card, confirm and receipt styles.
- `web/shots/stub-daemon.mjs` and `shoot.mjs`: see below.
- `docs/web-chat.md` (a "Rows and links" rewrite and the review table), `web/README.md`,
  `CHANGELOG.md`, and an entry for this page in the README's "Read next" table.
- Phase 3 only: `internal/store` (event kind, optional migration), `internal/desk`,
  `internal/daemon` (handlers), `internal/api` (the kind), and the web `DeskRow`.

Coordination: other work touches the chat page's styles and a separate swarm timeline page.
Check `web/src/styles/**`, `router.ts`, `App.tsx` and `api/types.ts` for open changes
before starting, rebase often, and keep the new code in new files so conflicts stay small.

## Tests and screenshots

Unit, `backlogActions.test.ts`: each kind's actions and which is recommended; Continue
disabled for a running run and for one with no session; no merge action on review; the
Ask-the-desk prefill is never worded as an answer.

Component, in the style of `Chat.test.tsx`, with the fake daemon API and fake desk:

- clicking a row adds a card and **sends no desk turn** (the `turn` fake is never called);
- a decision card answers with exactly the option's text, or the typed words, via
  `answer`, only after the Answer click; Ask the desk fills the composer and sends nothing;
- stop and close lane confirmations, and the force step after a refusal;
- a card whose subject leaves the lists shows "No longer waiting" and disables buttons;
- a second click on the same row focuses the first card;
- a refused call shows the daemon's text and keeps typed words;
- keyboard: open with Enter, focus on the heading, Escape order, focus restored.

`Lane.test.tsx` covers the panel per group. In phase 3, Go tests cover the daemon: a
browser-session answer appends an `action` event and starts no turn, a wake-up desk token
still gets 403, the next turn's prompt carries the held text, and the migration applies.

Screenshots, from the stub daemon with made-up data (`web/shots`, nothing committed):
the stub learns the new POSTs and mutates its state so a card visibly resolves. Shots per
item kind (decision, request, failed run, idle, running with the stop confirm open, review,
close-lane refusal with the force step), in light and dark, and the same at a narrow
viewport (390 by 844), which needs a per-shot size in `shoot.mjs`. Reviewers can then look
at every card without a live daemon.

## Phases

| Phase | Delivers | Size | Needs |
|---|---|---|---|
| 1 | model, card, client wrappers, all seven kinds, confirmations, local receipts, tests, stub and screenshots, docs | one web MR, the largest | web scope only |
| 2 | the panel on the lane's page, the `?card=` deep link | small web MR | phase 1 |
| 3 | `action` events: the desk is told, the thread and history show what was done, optional `answered_via` | one Go MR, then a small web one | daemon scope; phase 1 for the receipts to show |

Until phase 3 lands, the desk is not told of card actions, and this page should not claim
otherwise. Later and not planned here: shipping a lane from a card (repos set to
`push: shepherd`), starting the first run of a lane that never ran, a per-lane fix-try
count, and acting on several items at once.

## Choices, with recommendations

| Choice | Recommendation | Why |
|---|---|---|
| Answer a decision with one click on an option, or select then Answer | select then Answer | an answer steers an agent; a stray click should not |
| Typed words on a decision card: the answer itself, or a message to the desk | the answer, verbatim; Ask the desk is a separate button | the words are provably the person's; no turn, no cost |
| Where the card lives | in the thread, local, until phase 3 | matches the agreed design; the daemon's history stays the desk's |
| What a row click does | opens the card; a **details** link goes to the lane | the person asked to stay on the page |
| Close lane | try without force, show the refusal, force only on a second confirm | the daemon's refusal is the only preview it can give |
| Tell the desk | phase 3, daemon side, no turn | a client cannot forget; no cost per click |
| Default Continue and Nudge prompts | the two plain sentences above, editable | the person's words, visible before sending |

## What this does not change

- No agent gains a way to answer, approve or merge. The routes and their guards are as
  they are; phases 1 and 2 touch no Go.
- Decisions, requests and runs keep their existing pages; the board's Dismiss stays.
- The terminal chat has its own dock and is not touched.
