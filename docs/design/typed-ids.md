# Typed IDs in the web UI

Status: **plan**, for next-up item 80 (approved). No code is written yet.

Chat, the terminal and the agents refer to things by kind and number: "decision 275",
"item 82", "question 43", "run 311". The web UI shows some of these and not others, so a
reference cannot always be found on a page. This plan makes every list show the typed ID the
same way, and lets the person search and filter by it.

## What dev shows today

Checked against `dev` at `33ebd0e`. It does not show IDs everywhere.

| Kind | Where an ID shows today | Where it does not |
| --- | --- | --- |
| decision | `DecisionCard` meta line ("decision 275"), the answered list in `pages/Inbox.tsx` | Inbox row for an unanswered one other than via the card, the inspector header, the lane page's decision links (`pages/Lane.tsx`), the run inspector's Asked tab (`inspector/runPane.tsx`) |
| item (next-up) | `pages/Next.tsx` row as `#82`, the planned row as `row_id` | `components/Backlog.tsx` rail, lane page item list (`pages/Lane.tsx`), board rows for next items (`pages/Board.tsx`) |
| request | Only as `shepherd request route <id>` text in `pages/Inbox.tsx` | Everywhere else, including the lane page's request list |
| question | Only in `aria-label`s in `chat/questions.tsx` | Visible nowhere: Inbox rows for questions, `inspector/questionPane.tsx` |
| run | Bare number in `pages/Runs.tsx`, `components/TimelineTable.tsx` and the lane runs table, "run N" in `DecisionCard` | Inbox, board rows (only in dismiss button text); the log page title says "Run N" but as a heading, not a chip |
| topic | Nowhere visible; `pages/Artifacts.tsx` shows "topic N" only as a fallback option | `components/Topics.tsx`, `TopicBar.tsx`, `TopicPill.tsx` |

Three spellings are in use already (`#82`, bare `82`, "decision 275"), so the first job is one
format. `components/Work.tsx` also prints a bare `row.id`; that one is a plan-file row id
(for example `M2-3`), not a typed ID, and stays as it is.

## The chip: one component, one format

**Format: the kind word and the number, `decision 275`**, in a small monospace, faint chip.
Why:

- It is what the chat, the CLI and the agents already say, so the text on a page matches the
  words the person is trying to find, and the browser's find-in-page works on a pasted
  reference.
- Letter codes (`D275`, `R12`) collide: run and request both want `R`, and they must be
  learned. A bare `#82` loses the kind, which is the whole point of typing the ID.
- Width is the cost. Where a column or heading already names the kind (the Runs table's "Run"
  column, the Next-up page), the chip has a `bare` form that shows only the number (`311`),
  with the kind kept in its accessible name and `title`.

Shape, in `web/src/ui/IdChip.tsx`, exported from `ui/index.ts`:

```tsx
type IdKind = 'item' | 'request' | 'decision' | 'question' | 'run' | 'topic'
<IdChip kind="decision" id={275} />                  // "decision 275"
<IdChip kind="run" id={311} bare />                  // "311", aria-label "run 311"
<IdChip kind="decision" id={275} href={href.decisions(275)} />  // a link when given a target
```

- Plain text span by default (so it can be selected and copied whole); an anchor when `href`
  is given. It does not reuse `Chip`, which is for filters and removable tags, but shares its
  tokens so it sits beside one.
- `user-select: all` on the chip so one click selects the reference to paste into chat.
- Styles go in `ui/ui.css` only, never in a page stylesheet, so no page lane has to be
  touched for the look.
- `web/src/state/ids.ts` (new, pure): `refLabel(kind, id)` and `parseRef(text)`, below.
  A test file for each, plus a card on `ui/KitPage.tsx`.

## Where each list gets it

Paths are under `web/src`. "Held" marks a file an open lane is changing (see the next
section).

| Kind | List or row | File | Form |
| --- | --- | --- | --- |
| decision | Inbox row and answered list | `pages/Inbox.tsx`, `components/DecisionCard.tsx` | full, replaces the plain "decision N" span |
| decision | Inspector header | `inspector/decisionPane.tsx` (via `DecisionCard`) | full |
| decision | Lane page "waiting on" links | `pages/Lane.tsx` | full |
| decision | Run inspector Asked tab | `inspector/runPane.tsx` | full |
| question | Inbox question row | `pages/Inbox.tsx` (`it.question_id`) | full |
| question | Question inspector | `inspector/questionPane.tsx` | full |
| question | Waiting-on-you strip in the thread | `chat/questions.tsx` | full, beside the existing label |
| request | Inbox "request to route" row | `pages/Inbox.tsx` (`it.request_id`) | full, next to the route command |
| request | Lane page request list | `pages/Lane.tsx` | full |
| item | Next-up rows, planned rows | `pages/Next.tsx` (lines for `#{it.id}` and `row.row_id`) | `bare`, since the page is the item list; the planned row id stays as is |
| item | Backlog rail | `components/Backlog.tsx` | full |
| item | Lane page items | `pages/Lane.tsx` | full |
| item | Board rows for next items | `pages/Board.tsx` | full |
| run | Runs table | `pages/Runs.tsx` | `bare`, link to the log |
| run | Timeline table | `components/TimelineTable.tsx` | `bare`, link |
| run | Lane runs table, commit list | `pages/Lane.tsx` | `bare` / full |
| run | Log page title | `pages/Log.tsx` | keep the heading, add a chip in the sub line |
| run | Run inspector header | `inspector/runPane.tsx` | full |
| run | Board rows, dismiss buttons | `pages/Board.tsx` | full on the row; button text keeps "run N" |
| run | Inbox decision meta ("run N" link) | `components/DecisionCard.tsx` | full, link |
| topic | Topic management rows | `components/Topics.tsx` | full |
| topic | Topic bar chips and pills | `components/TopicBar.tsx`, `TopicPill.tsx` | none visible: a filter chip with a number would be noise; the ID goes in the `title` |

Not changed: `components/Work.tsx` (plan row ids), `pages/Settings.tsx`, `pages/Usage.tsx`
(rows key on run IDs but list no run column to type), the Artifacts page's topic option
(held, and a `<select>` option stays plain text; it can read "topic 12: title" later).

## Search and filter by ID

Client-side only; no API change. The lists are already in memory in `state/`.

- **`parseRef(text)`** accepts what people paste from chat or type: `decision 275`,
  `decision #275`, `d275`, `#275`, `275`. It returns `{kind?, id}` or null. A bare number has
  no kind and matches any kind on the current page.
- **Per-page find box** on the three pages that already filter with a toolbar: Inbox
  (`pages/Inbox.tsx`, beside the kind filter), Runs (`pages/Runs.tsx`) and Next up
  (`pages/Next.tsx`). The box matches the typed ID first, then falls back to a
  case-insensitive substring of the title or question. It composes with the existing filters
  and with "Clear filters". `Board.tsx` gets the same box after its polish lane lands.
- **Deep link by ID** (a "go to" that opens `decision 275` wherever it lives, using the
  inspector's `withOpen`) needs the shell's command surface and `router.ts`, both held. It is
  the last step and optional.
- The chip also makes the reference copyable (`user-select: all`), which covers the
  reverse direction: page to chat.

Chat linkifying "decision 275" in agent text is a natural follow-on but lives in
`chat/md.tsx` and `chat/thread.tsx`, both held, so it is out of this plan.

## Splitting into merge requests

Open lanes and what they hold (read from their branches and worktrees on 2026-10-15):

| Lane | Files |
| --- | --- |
| `feat/web-status-rail` | `App.tsx`, `router.ts`, `shell/Shell.tsx`, `shell/chatLink.ts`, `chat/md.tsx`, `chat/thread.tsx`, `pages/Chat.tsx`, new `pages/Status.tsx`, `components/BackLink.tsx`, `DaemonStatus.*`, `StatusCounts.*`, `styles/chat.css`, `styles/layout/daemon.css`, `styles/layout/shell.css`, `styles/pages/status.css` |
| `fix/web-polish-board` | `styles/layout/rows.css` so far; `pages/Board.tsx` is likely |
| `fix/web-polish-settings` | nothing committed yet; `pages/Settings.tsx` and `styles/settings.css` |
| `fix/web-artifacts-view` | `artifacts/*`, `pages/Artifacts.tsx`, `styles/pages/artifacts.css` |
| `fix/web-keep-going-meter` | `components/KeepGoing.*` |

None of the files in the table above are in the first four rows except `pages/Board.tsx`
(possible) and the Artifacts select. Each MR below touches only free files, and the order
keeps them independent: after MR 1, any of MR 2 to 5 can land in any order.

| MR | Content | Files | Avoids |
| --- | --- | --- | --- |
| 1. Chip and parser | `IdChip`, its CSS, `state/ids.ts`, tests, kit card | `ui/IdChip.tsx`, `ui/index.ts`, `ui/ui.css`, `ui/KitPage.tsx`, `ui/ui.test.tsx`, `state/ids.ts` and its test | all held files; `ui/` is held by nobody |
| 2. Decisions, questions, requests | the Inbox, cards and inspector panes | `components/DecisionCard.tsx`, `pages/Inbox.tsx`, `inspector/decisionPane.tsx`, `questionPane.tsx`, `chat/questions.tsx`, with their tests | `chat/thread.tsx` and `chat/md.tsx` (held); `questions.tsx` is a separate file |
| 3. Runs | tables, log header, run inspector | `pages/Runs.tsx`, `components/TimelineTable.tsx`, `pages/Log.tsx`, `inspector/runPane.tsx` | none held |
| 4. Items and topics | Next up, Backlog rail, Lane page, Topics | `pages/Next.tsx`, `components/Backlog.tsx`, `pages/Lane.tsx`, `components/Topics.tsx`, `TopicBar.tsx`, `TopicPill.tsx` | none held; `Lane.tsx` also takes the run and request chips, which MR 3 does not touch |
| 5. Find by ID | find box on Inbox, Runs, Next up | the same page files as 2 to 4, so land after them | held files |
| 6. Board | chips on board rows and the find box | `pages/Board.tsx` | wait for `fix/web-polish-board` to merge |
| 7. Deep link (optional) | "go to" by ID | `shell/Shell.tsx`, `router.ts` | wait for `feat/web-status-rail` to merge |

Rules for every MR: chip styles only in `ui/ui.css`; no edits to a page stylesheet; if a
held file turns out to be needed, drop that row from the MR and add it to the later one
rather than rebasing across the other lane. `make check` and the web tests gate each.

## Decisions for the maintainer

1. **Format.** Recommended: `decision 275` with a `bare` form where the column names the
   kind. Alternative: `D275`-style codes (shorter, but collide and need learning).
2. **Topic IDs.** Recommended: show them only in the Topics management list. Alternative:
   also on every topic chip.
3. **Scope of 7 and chat linkifying.** Recommended: defer both until the status rail lands.
