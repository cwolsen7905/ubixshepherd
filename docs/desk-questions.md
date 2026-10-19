# Desk questions: the front desk's questions to you cannot be missed

Status: **built** (Next-up item 59, approved by the maintainer 2026-10-13).

## The problem

When the front desk asked the person something in chat, new swarm events pushed the
question up the thread and it was missed. Nothing tracked it: a question was just text.

## The design

Questions are marked **explicitly** by the desk, never detected from a "?". A marked
question is an open question until the person answers or skips it, and it waits in the one
[Waiting on you](waiting-on-you.md) list as a new kind, `question`. There is one list and
one counter.

### Tools (the desk's only; agents never get them)

| Tool | CLI | What it does |
|---|---|---|
| `question_open` | `shepherd question open "text" [--topic N]` | Records the question the reply asks, under the desk turn in progress. Text is redacted and cut at 2,000 bytes. |
| `question_answer` | `shepherd question answer <id>` | Marks it answered by the person's message in this turn. Refused unless the turn is one the person started; the desk cannot name another turn. |
| `question_list` | `shepherd question list [--all]` | The open questions. |

Skipping has a CLI form (`shepherd question skip <id>`) and a route, but it is the
person's: the daemon refuses it from the desk outside a turn the person started.

The desk's instructions (`DeskBrief` in `internal/chat`, `Brief` in `internal/desk`) tell it
to open a question when a reply asks one, and to mark it answered only when a main-chat
reply plainly covers it. The main input never clears a question by itself.

### Data

Migration 29 adds `desk_questions` (id, workspace_id, turn, topic_id, text, state, answered_turn,
created, closed). `state` is `open`, `answered` or `skipped`. A question is per workspace,
like the desk. It stays open across a new conversation (`/v1/desk/new`) until answered or
skipped; resetting the desk does not clear what the person was asked.

### API

| Route | Who | |
|---|---|---|
| `GET /v1/questions?state=&workspace_id=` | desk, operator, browser | `{questions: [...]}` |
| `POST /v1/questions` | desk | open one: `{text, turn?, topic_id?, workspace_id?}` |
| `POST /v1/questions/{id}/answer` | desk (person's turn), operator | mark answered |
| `POST /v1/questions/{id}/skip` | the person | clear without an answer; recorded as a thread action and told to the desk at its next turn |
| `POST /v1/desk/turn` with `reply_to` | the person | a normal person turn linked to the question; accepting it answers the question, whatever the words say |

`GET /v1/waiting` lists each open question with kind `question`, action `reply`,
`question_id` and the desk `turn` it was asked in, and counts them in `counts.questions`.
The `[Waiting on you (N)]` line the desk sees and repeats includes them as
`question 12: ...`. The dock's "N waiting on you" count includes them.

A reply from the thread is sent as `[Replying to your question N: <text>]` followed by the
person's words, so the desk knows what is being answered. Any reply closes the question it
names, not only a plain yes or no: the person has engaged with it, and the desk can ask again
in the thread if the answer left something open. A reply the desk refuses to take (empty,
queue full) closes nothing.

### Web

- **Counter arrows.** The header badge gains ▲ and ▼. They step through every waiting
  item (questions and the other kinds, oldest first, wrapping), open the chat page when
  another page is showing, scroll to the item's row and highlight it. A question goes to
  its desk message, a decision to its "asked" row or card, a merge request or ship item to
  its lane's latest row. An item the loaded thread holds no row for (a Next-up proposal,
  or work older than the loaded window) is named in a note with a link to act on it.
  Read but unanswered still counts.
- **Reply here and Skip.** The desk message that asked an open question carries a
  "Waiting on you" mark with buttons. Reply here opens an input under that message;
  sending posts the linked turn and clears the question. Skip clears it unanswered. Once
  cleared the buttons go away.

## Testing

Store (including the migration upgrade test), the daemon routes (open, answer, skip,
permissions, `reply_to`), the desk briefs, and the web (counter stepping, step anchors,
the reply and skip buttons, the thread placement).

## Not in this change: follow-ups

- The browser tab-title count of what waits.
- Desktop notifications for a new question.
- Re-nudges: the desk reminding the person of a question left open past some time.
