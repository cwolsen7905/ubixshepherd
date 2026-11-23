# Chat replies: asks the person can answer where they read them

Status: **proposed, not built.** For the maintainer's review. The problem comes from the
person's own report; everything under "The design" is a recommendation. The person asked
for the direction now and not the whole of it at once: the phases at the end are ordered so
each is useful alone, and later ones can wait.

## Where this lives

A new page, linked from [planning-room.md](planning-room.md), and not a section of it. Rooms
need answerable asks (a room asks one question at a time), but the main thread has the
problem today, with no room involved, and the fix is built on the thread, the Waiting list
and the tools. A section in the rooms doc would make a small, useful fix wait on a large,
unbuilt feature. The rooms doc gains a short pointer instead.

## The problem

The web chat shows the front desk's reply ending in a "Needs you:" numbered list, for example
"Approve or change decision 519 ... my recommendation is to approve". Each item is plain
markdown. There is no button, no choice and no reply field on it. Under the composer, the
"5 waiting on you" strip ([waiting-on-you.md](../waiting-on-you.md)) shows Q509, Q515 and
Q517 as truncated links, a "+2" and a "next" control. The decision was in fact answered on a
separate page.

What goes wrong:

- **The ask is visible but not answerable.** The person reads it in the thread and must
  leave the thread to act on it. The thread and the answer controls are in different places.
- **Prose hides the shape.** Many asks are yes/no or a short choice, and the desk already
  knows the options and its recommendation. Prose throws that away, so the person types a
  sentence the desk then has to parse.
- **Repeats.** The desk restates the same open item at the end of several turns, so the
  thread fills with stale copies of one ask. Only the last is current, and nothing says so.
- **Two lists that disagree.** The numbered list in the reply and the waiting strip are
  written separately. The strip is derived from the store; the list is whatever the desk
  typed, so they drift, and the strip's truncated links tell the person little.

## What exists today

| Tool | What it carries | Gap |
|---|---|---|
| `ask_human` (agents) | `question`, `options` (strings), `recommendation` (free text naming one), `why`, `artifacts`. Stored as a decision | The recommendation is not tied to an option; an option is a bare string |
| `decision_answer` (desk) | `id`, `answer` as the person's words or an option number | The web decision page can pick an option; the thread has no control that calls it |
| `question_open` (desk) | `text`, optional `topic`, `lane`, `item` | No options at all: a question is text only. [inbox-context.md](inbox-context.md) lists this as left out |
| `question_answer` (desk) | `id`, only when the person's message plainly answers it | Answering is a guess by the desk, not an action by the person |
| `next_update` (desk) | `state` and `move` only in a turn the person started, in their words | A proposed item's approval is a button on the Next up page, not in the thread |
| Merge request approval | The lane page's approve control; listed in Waiting as `mr` | Not reachable from the thread |

The Waiting list already has stable ids (`D410`, `Q271`, `N255`, `app!175`), computed from
the record. They are the identity this design reuses: a card is a view of one waiting item.

## The design

### 1. A structured ask

The desk and agents stop writing "Needs you" items as prose. Each ask is one typed object,
stored with the record it belongs to and rendered by the client.

| Field | Meaning |
|---|---|
| `kind` | `decision`, `question`, `mr_approval` or `next_approval` |
| `ref` | The waiting item id (`D519`, `Q509`, `app!175`, `N255`): the identity |
| `summary` | One line, what is being asked |
| `answer_type` | `yes_no`, `choice` or `text` |
| `options` | For `choice`: each with a short `label`, an optional one-line `detail`, and a `recommended` flag on at most one |
| `why` | One line, why it is the person's call |

Rules for the types:

- **Decision.** Already has options and a recommendation. Change the options from strings to
  `{label, detail}` and make the recommendation name an option by position, keeping the
  free-text reason. An old decision with string options reads as options with no detail, and
  its recommendation text as the reason.
- **Question.** Gains an `answer_type` and options. The default stays `text`, so every
  existing call works.
- **MR approval.** `yes_no`: approve, or open the lane. It is the lane page's approve control
  surfaced, with the same checks; the card never adds a way to approve that the page lacks.
- **Next-up approval.** `choice`: approve, defer, drop. These are `next_update`'s states.
- Every `choice` and `text` ask offers **Other**, a free-text answer, so the options never
  box the person in. `yes_no` offers it too ("Yes, but ...").
- A yes/no is a choice of two options, so one renderer serves all three.

### 2. Cards in the thread and the strip

In the web thread, a typed ask renders as a **card** where the prose list was, and not as
markdown.

- A card shows the summary, the options as buttons with the recommended one marked
  ("Recommended" in words, not colour alone), and an **Other...** button that opens a text
  field. Keys `1` to `9` pick options and Cmd+Enter sends, as the UI rules already say.
- Choosing sends the answer and the card settles: it shows what was chosen, by whom and
  when, and its controls go away. A failed send keeps the card open with the error.
- Cards use the existing parts (`ListRow`, `Button`, `Chip`) and tokens; lantern marks them
  as waiting on the person, lichen the selection and Send. A new class goes beside its
  siblings in `components.css`, with a `data-component` name.
- The same answer control appears in the waiting strip. Selecting a strip item opens its card
  in the side panel (the answering place, per the UI rules) with "Open as page". The strip's
  truncated link text becomes the ask's `summary` in the row, with its id.
- The terminal chat gets the same asks as a numbered list under the reply; typing `D519 2` or
  `D519 other: ...` answers, matching the ids the strip already prints. Later, not first.

### 3. One live card per ask

The desk restating an item must not add a copy. A card is keyed by `ref`.

- The first turn that raises an ask renders the card in place. A later turn that raises the
  same `ref` does not render a second card: it updates the first (new summary, changed
  options) and adds a short line in the thread, "Still waiting: D519", linking to it.
- A card whose item is no longer waiting is settled wherever it shows. Answering on the
  decisions page settles the thread's card too, because the card reads the live item and not
  its own copy. This is the case in the screenshot.
- The desk's closing format stops listing items the waiting strip already shows. A turn's
  "Needs you" part holds only asks new to that turn; the rest is the strip's job.

### 4. Keeping the rules

The standing rules do not change, and the card is built so they cannot be bent.

- **Only the person's action answers.** A card's buttons call the same HTTP routes the
  Decisions and Next up pages call, from the web client, as the person. There is no desk
  tool that clicks a card, and `decision_answer`, `question_answer` and the `next_update`
  state change keep their existing guards (the person's own words, in a turn they started).
  An agent never answers a decision.
- **Recorded as their words.** A choice is stored as the option's label as the person saw
  it, with the option number, and "Other" as exactly what they typed, plus who, when and
  that it came from a card. The agent continued by the answer receives the same text it
  would have from a typed reply: "option 2: Approve ...", or their sentence. A recommended
  option is a suggestion; nothing is pre-selected and nothing is answered by default or
  by timeout.
- **Agents never approve their own work.** An MR approval card is shown only to the person.
  A lane's own agent has no route to it.
- **No decision from a typed ask alone.** A desk that emits a typed ask of kind `decision`
  does not create a decision. Decisions come from an agent's `ask_human`; the desk's own asks
  are questions, MR approvals or next-up approvals.

## What has to change

| Area | Change |
|---|---|
| Schemas | `ask_human` options become objects with `label`, `detail`; a `recommended` index. `question_open` gains `answer_type` and `options`. Both still accept the old string forms |
| Store | Decisions keep their options column and read old rows unchanged. Questions get appended migrations for `answer_type` and `options`. Answers keep who, when and `via` (`card`, `page`, `message`) |
| Answer routes | One route for a question answered by a person from a card, recording `via`. Decision and next-up answers already have routes; the card calls them. A question answered from a card marks `answered_turn` to the turn that answered it, so the existing reply behaviour holds |
| Waiting list | Items carry `answer_type`, `options` and the recommended flag so the strip and the thread render from one source. `GET /v1/waiting` is the card's data; nothing is stored for it twice |
| Feed | Desk turns carry the refs they raised, so the web can key cards and collapse repeats. The feed's closed set of kinds is unchanged |
| Desk prompt (`DeskBrief`) | Replace the "Needs you" list rule: raise each ask by `question_open` with options, do not restate an item already waiting, name it by id when referring to it. Keep "never answer for the person" |
| Worker prompts | `ask_human` guidance: give options with a short detail and mark the recommended one, as the person picks a button |
| Web | A `AskCard` component in `web/src/ui/` used by the thread, the side panel and the Inbox; the strip's rows from `summary`; style lint and both themes |
| Docs | `docs/web-chat.md` and `docs/waiting-on-you.md` as each phase ships |

`inbox-context.md`'s "A question's choices" leftover is closed by phase 2.

## Phased plan

Each phase is one merge request, green alone, and updates the user docs for what it ships.

### 1. Answer a decision from the thread

Scope: `web/` only. The thread shows a card for a decision it names by id, built from the
live decision list: its options as buttons, "Recommended" marked, and "Other...", calling the
existing decision answer. No schema or desk change: the card is attached where the reply
mentions a waiting `D` id.

Done when: a decision named in a desk reply can be answered in the thread with a click; the
decisions page shows the same answer; answering on the page settles the thread's card on its
next read; the recorded answer is the option text; a test fails if a card renders a control
for an item that is no longer waiting.

### 2. Questions with options

Scope: schema, store, desk prompt, API, web. `question_open` takes `answer_type` and options;
the card answers a question with a click or "Other...", recorded with `via`.

Done when: the desk can open a yes/no and a multiple-choice question; the person answers
each from the thread and from the strip; the answer is stored as their words; old
text-only questions still work; the desk prompt test checks the new rule.

### 3. One live card, no repeats

Scope: feed (refs on turns), web, desk prompt. Cards keyed by `ref`; a restated ask updates
the first card and leaves a "Still waiting" line.

Done when: a desk that raises the same item in three turns leaves one card and two short
lines in the thread; an item answered elsewhere settles its card; the closing format no
longer lists items the strip shows.

### 4. MR and next-up cards

Scope: web, API. Approve cards for MRs and approve, defer, drop for proposed Next-up items,
calling the existing routes.

Done when: the same actions available on the lane and Next up pages work from a card with the
same checks and refusals; nothing is possible from a card that is refused on the page.

### 5. Structured asks from agents

Scope: `ask_human` schema, worker prompt, decision page. Options with details and a marked
recommended one.

Done when: an agent's decision shows labelled options with detail and one marked
recommended, and an old decision renders as before.

### 6. The terminal chat

Scope: `internal/chat`. Numbered asks and `D519 2` replies.

Done when: the terminal answers a card by id and option, tested with fakes.

## Out of scope

- A desk that answers for the person, or any default-on-timeout. Never.
- Rooms ([planning-room.md](planning-room.md)). They use these cards unchanged and add
  nothing here; a room's "one question at a time" is one live card.
- A free-form form builder. Four kinds and three answer types are all there is.
- Editing a sent answer. A wrong answer is corrected by a new message, as now.

## Open questions

Each has options and a recommendation. None blocks phase 1.

1. **Typed asks from the desk: tool or inline block?** Options: extend `question_open` and
   friends so the structure is stored; have the desk emit a fenced block the client parses.
   Recommendation: the tools, because the store then holds the ask and the waiting list can
   draw the strip from it. A parsed block cannot be checked against the records.
2. **Should the desk's prose "Needs you" list go away?** Options: remove it, since cards
   carry it; keep a one-line pointer. Recommendation: keep one line naming the ids, so the
   terminal and logs stay readable.
3. **Does Other... for a decision go to the agent verbatim?** Options: verbatim; verbatim
   with the options listed for context. Recommendation: verbatim, matching a typed answer
   today.
4. **Multi-select.** Some asks are "which of these". Options: not in v1; a `multi` answer
   type. Recommendation: not now: ask them as separate yes/no questions until a real case
   needs it.
5. **Does a one-click choice need a confirm for risky asks?** Options: never; for decisions
   flagged risky (money, deletion, production); always. Recommendation: for risky ones only,
   consistent with the settings rule that confirms risky saves, and the confirm names the
   chosen option.
6. **Where the recommended option sits.** Options: first; its position as given, marked.
   Recommendation: position as given, marked in words, so the order does not nudge.
7. **Mobile and narrow widths.** Options: buttons wrap; options collapse into a list.
   Recommendation: wrap up to four options, a list beyond, tested at narrow width in both
   themes.
