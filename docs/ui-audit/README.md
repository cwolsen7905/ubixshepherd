# Web UI audit: width, layout and consistency

An audit of the Shepherd web UI (`web/`), written against the code at the head of `dev`.
It changes no app code. It records what is wrong, what the Decisions page does right, one
shared layout and width system to apply everywhere, and a split of the work into merge
requests that each fit one lane.

## Summary

- **Most pages use 50 to 60% of a wide window.** Every page except the board and the
  chat is held to one 68rem column (`--measure-page`, `web/src/styles/theme.css:66`),
  anchored to the left edge of the shell (`--page-margin-left: 0`). At 1920 px that column
  is about 1120 px and the right 700 px is empty. At 2560 px it is 1224 px of 2560.
- **The width is not the whole story.** The Decisions page is itself only a 68rem column,
  with a card body capped near 100 characters. It reads better because its content has
  structure: a one-line headline, rendered markdown, set-apart "why" and "recommended"
  blocks, and options that look like keys. The fix is to give every page that kind of
  structure and to spend the extra width on columns and tables, not on longer lines.
- **Four real bugs, not just spacing.** The project page's rows truncate lane names at
  about 14 characters with 500 px free (a grid with the wrong number of tracks). The Next
  up list wraps every title while 800 px sits unused. The top bar overflows at 1024 px. The
  chat prints a decision's full raw text as one grey paragraph above its formatted card.
- **Six container widths, three heading styles, two units.** `.page` 68rem, `.page-wide`
  and `.board` 96rem, `.next` 64rem, `.page.chat` 1800px, the shell at 112rem, and prose at
  76ch or 100ch. Chat, Next up and topics use `px` literals where the rest use `rem` tokens.
- **The fix is one layout system**, a handful of tokens and two page shapes, applied
  through the shared `.page` container so each page's own CSS shrinks. Seven small MRs
  below, in an order that puts the foundation and the worst bug first.

## How the shots were taken

- Headless Chrome at 1440, 1920, 2560 and 1024 px wide, scale 1, capped at 900 px tall
  (the first screen is what shows the width problem). Light theme.
- **They are not the maintainer's real data.** This repo is public, and the live daemon
  holds private repo names, decision text and local paths, so none of those shots are
  committed. The committed shots use the web stub daemon's made-up data (`web/shots`). The
  stub has no answers for the projects, usage, settings or topics endpoints, so for those a
  throwaway proxy served the live daemon's answers with every repo, project and path name
  replaced and all free text blanked. Free text on those pages therefore reads "An example
  note for this item."; the layout is real, the words are not.
- Only read-only navigation: no click started, closed or answered anything. The rail's
  "Next up" tab was opened for the second chat shot, which is a view switch.
- Not shot: the narrow (below 760 px) layout, the dark theme, the Changes and
  Conversation tabs of a lane, and the settings diff review. They share the same
  containers, so the findings carry over, but they deserve their own pass.

Widths in pixels depend on the root size, which scales from 15px at 1440 to 18px by about
2400 (`html { font-size: clamp(...) }`, `theme.css`):

| Window | 1 rem | `.page` (68rem) | `.board`, `.page-wide` (96rem) | shell (112rem) | `.next` (64rem) |
| --- | --- | --- | --- | --- | --- |
| 1024 | 15 px | 1020 (fills) | 1440 (fills) | fills | 960 (fills) |
| 1440 | 15 px | 1020 (71%) | 1440 (fills) | 1680 | 960 (67%) |
| 1920 | 16.5 px | 1122 (58%) | 1584 (83%) | 1848 | 1056 (55%) |
| 2560 | 18 px | 1224 (48%) | 1728 (68%) | 2016 | 1152 (45%) |

## The reference: what makes Decisions better

Shots: [1440](shots/decisions-1440.png), [1920](shots/decisions-1920.png),
[2560](shots/decisions-2560.png), [1024](shots/decisions-1024.png).

| What | Where | Why it works |
| --- | --- | --- |
| The question is a headline: `--t-lg`, 600, 1.35 line height | `app.css` `.decision-q` | The one thing you must read is the biggest thing on the page |
| The body is markdown, rendered, with code styled and a clamp with "Show all" | `decisions.css` `.decision-md`, `.is-clamped` | Long plans become scannable lists instead of one grey block |
| A reading measure of `min(100%, 100ch)` | `decisions.css:4` | Lines stay readable at any window width; the card is the measure, not the page |
| "Why this is yours" and "Recommended" are separate asides with their own rule and wash | `decisions.css` `.decision-aside`, `.decision-rec` | The reason and the advice are findable without reading the question |
| Options are full-width keyed buttons, the recommended one marked | `app.css` `.option`, `decisions.css` `.option.is-recommended` | The answer is a click or a number key, not a form |
| One tone for "needs you": the lantern wash and band | `theme.css` `--lantern*` | The page's state is visible from across the room |

What it does **not** do is use the width: the card is about 810 px wide in a 1920 px window
and the right 40% is empty (see the Decisions section below). Treat it as the reference for
*content structure and typography*, not for container width.

## Cross-cutting findings

These cause most of the per-page problems, so they are listed once.

1. **One narrow column for most pages.** `.page { max-width: var(--measure-page); margin-left:
   var(--page-margin-left) }` (`app.css:132`, `theme.css`). The comment says `0` keeps
   pages on the shell's left edge and `auto` centres them; neither uses the width. Affects
   Decisions, Lane, Log, Project, Projects, Next up (its own narrower 64rem), Settings'
   main column.
2. **Containers disagree.** Board rows start at the shell edge, other pages start one
   `--pad` in, and page titles are pushed another `--gutter` right, so the left edge of
   content is at three different x positions as you move between pages (see
   [board](shots/board-1920.png) against [runs](shots/runs-1920.png)). The glyph rail is
   deliberate, but only the titles and the glyphs should sit on it, not the page box.
3. **Lists are grids with the wrong column count.** `.row` is a 7-track grid, `.row-finished`
   another, `.project-row` a 5-track grid whose rows have 4 or 5 children in different
   orders. `.row > span` is `nowrap` with an ellipsis, so a child in a narrow track is cut
   and a child in a wide track floats mid-row. There is no shared row component.
4. **Text-width is not separated from page-width.** Prose, tables and cards all inherit the
   page column. A page cannot be wide for its table and narrow for its prose.
5. **Mixed units and tokens.** `chat.css`, `next.css`, `topics.css`, `conversation.css` use
   `px` (`8px`, `6px`, `10px 12px`) and `3px 4px` where `app.css` uses `--s-*`. The
   spacing scale therefore does not follow the root-size scaling on wide displays.
6. **Three heading styles.** Page h1 (`--t-xl`), board and lane section heads
   (`.group-head`, `.section-h`: `--t-sm`, muted, 600) and project and usage h2
   (`--t-lg`, 600, with a count). The same kind of heading looks different per page.
7. **Buttons are mostly consistent, with exceptions.** `.button`, `.button-primary`
   (lantern) and `.link-button` are the system (`app.css` comment). Exceptions: Next up
   shows a lantern primary "Approve" on every planned row (twenty primaries on a page,
   against the "one action that matters" rule); "New project" is a bordered button holding
   underlined link text; "Close lane" on the board is a bare outlined button under its
   row; the log's "Expand tool calls" and "Follow" are text with a key hint.
8. **Empty states are three patterns.** The centred `.empty` block (64ch, auto margins),
   an inline muted sentence ("Nothing waits on you here."), and a per-group sentence on the
   board ("Nothing needs attention here." repeated for every project). No icon, no next
   step, no common class.
9. **The bar does not hold at 1024.** `.nav a` wraps ("Next up" breaks to two lines), the
   bar overflows the window by a few pixels (document is 1031 px at a 1024 px window) and
   the help button is clipped ([board at 1024](shots/board-1024.png)). The counts only
   hide below 760 px (`app.css`, `@media (max-width: 760px)`).

## Per-page findings, ranked by impact

Impact weighs how often the page is open, how bad the problem is, and how much of the
window is wasted. Shots are at 1920 unless noted.

### 1. Project and Unassigned: rows truncate with room to spare (bug)

[project 1920](shots/project-1920.png), [2560](shots/project-2560.png),
[1440](shots/project-1440.png), [1024](shots/project-1024.png),
[unassigned](shots/unassigned-1920.png).

- Lane names are cut at about 14 characters ("chore/inference-tun…", "ubixshe…"),
  detail text at 8 ("succeeded · …"), and the age sits at 55% of the row, with 500 px of
  row to its right that nothing uses.
- Cause: `.project-row { grid-template-columns: gutter 1.3fr 1fr 3fr 3rem }`
  (`projects.css:71`) is applied to rows with different children in `Project.tsx`
  (decision rows have 4 children, lane rows 5, run rows 5, in different orders), so the
  `3fr` track gets an age or an MR badge and the real detail lands in a `3rem` track.
  `.row > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap }`
  (`app.css`) then truncates.
- Fix: one grid per section (decisions, lanes, runs) with named tracks and the detail in a
  `1fr` track; at table measure the same rows show full lane names, the repo, MR state and
  a full detail line.
- The Unassigned page, with many repos, prints them as one wrapped paragraph in the
  header and an identical "Add docs/WORK.md" line per repo under Work. Collapse the repo
  list behind a count and show Work once with the repos it applies to.

### 2. Next up: every title wraps, 800 px empty

[1920](shots/next-1920.png), [2560](shots/next-2560.png),
[1440](shots/next-1440.png), [1024](shots/next-1024.png).

- `.next { max-width: 64rem }` (`next.css:2`) is the narrowest page and the one with the
  most text per row. The action cluster (Approve, Defer, Drop, up, down, "Link to
  lane...") takes about 470 px, leaving titles about 420 px at 1920, so almost every title
  and every note wraps to two to four lines. At 2560 the list is 1150 px in 2560.
- The add form's placeholders are cut ("docs/plan.md (optiona") because the three inputs
  share a row with the Approved box and Add.
- Twenty lantern "Approve" buttons make the page's primary colour meaningless (see
  cross-cutting 7). Use `.button` for Approve and keep the lantern for the one next item's
  "Start".
- Fix: table measure; a row of [state glyph] [id + title + badges, then note below at full
  width] [fixed actions column]; the lane link select collapses into the actions as an
  overflow control; `rem` tokens in place of the `px` literals in `next.css`.

### 3. Lane: narrow main column, raw decision text in the timeline

[1920](shots/lane-1920.png), [2560](shots/lane-2560.png), [1440](shots/lane-1440.png),
[1024](shots/lane-1024.png).

- `.page.lane` is 68rem with a 20rem rail inside it, so the runs table and the timeline
  get about 780 px at 1920. The timeline entries wrap to two or three lines each and the
  runs table is tight; the rail's facts (scope, commits, worktree path) are fine but the
  worktree path wraps mid-word.
- A timeline entry for a decision or a run's task prints the entire question or prompt as
  one run-on line (markdown flattened to text with `**` and `-` visible). The Decisions
  card already knows how to render and clamp that text (`DecisionCard`, `decision-md`);
  the timeline should show a one-line clamp and link to the decision.
- Fix: page at table measure with `lane-grid` as main plus rail; keep the tab bar on the
  rail; reuse the clamp from the decision card for long text; give the runs table an
  explicit `Task` column that takes the free width.

### 4. Settings: a 40-field form in the middle of a wide box

[1920](shots/settings-1920.png).

- The page is `.page-wide` (96rem) but `.settings-main` is back at 68rem
  (`settings.css:12`) and textareas stop at 40rem. At 1920 the form is about 1120 px with
  the left nav, so labels, help text, the control, the "default:" hint and the "Set"
  checkbox are scattered across a 1000 px strip with the remaining 450 px empty.
- Each field is label on top, control below, hint beside, checkbox far right, so the eye
  zig-zags down a page that is several screens long.
- Fix: a two-column field grid at 1280 and up: label, help and default on the left (24rem),
  control and "Set" on the right, with the section nav sticky. At narrower widths it
  stays stacked.

### 5. Chat: long lines and a flattened decision event

[1920](shots/chat-1920.png), [2560](shots/chat-2560.png), [1440](shots/chat-1440.png),
[1024](shots/chat-1024.png).

- `.page.chat` already uses the width (1800px), so the thread is about 1250 px of text
  at 1920: assistant messages run 150 to 170 characters a line, well past the 100ch
  reading measure Decisions uses. The rail is a sound 35% (18 to 30rem).
- On live data a decision event is printed as a grey paragraph of the whole question
  (about 1000 characters, cut with "..." mid-sentence) directly above the formatted card,
  so the same decision appears twice, once unreadable. The stub data in the shots has short
  events and does not show it; look at any decision with a long plan.
- At 1024 the topic bar is cut off on the right instead of wrapping or scrolling
  ([chat 1024](shots/chat-1024.png)), and the rail takes a third of a narrow window.
- Fix: cap message prose at the shared reading measure, clamp feed events to one line
  with the id, and let the topic bar wrap or scroll with a visible affordance. Keep the
  thread at full width for code and tool rows.

### 6. Board: width mostly used, density is the issue

[1920](shots/board-1920.png), [2560](shots/board-2560.png), [1440](shots/board-1440.png),
[1024](shots/board-1024.png).

- The board is the one page at table measure (96rem). It fills a 1440 window, but at 2560
  it is left-anchored with about 560 px empty on the right.
- Every project with nothing to show still takes a header and an "Nothing needs attention
  here." line, so the first screen is mostly empty groups before any lane. Collapse empty
  projects to a single line with their spend, or fold them under "N quiet projects".
- Group heads are `--t-sm` muted while the rest of the app uses larger heads (see
  cross-cutting 6). The "Open" link is bold and underlined on every group.
- At 1024 the bar overflows (cross-cutting 9) and the row falls back to the stacked
  layout only below 760, so the 7-track grid is squeezed.

### 7. Projects: three cards across, empty right

[1920](shots/projects-1920.png).

- `.project-cards` is `repeat(auto-fill, minmax(18rem, 1fr))` inside the 68rem page, so
  three cards across at 1920 and the same at 2560. At table measure it would be four to
  five, and every card's stats row would stay on one line.
- Cards have unequal heights because repo lists wrap (the Unassigned card most of all) and
  the budget bar and its "at its hard cap" note sit at different offsets. Fix the card to
  a single repo line with a count and a tooltip, and align the stats and meter to the
  bottom.
- "New project" sits on its own row under the sub line; put it in the page head next to
  the title like the range filters elsewhere.

### 8. Runs: fills its box, but the useful column is the shortest

[1920](shots/runs-1920.png).

- At table measure it fills a 1920 window well. The lane column is allowed `22rem`
  (`.history-runs .run-lane`) and the Task column takes the rest but is ellipsised at
  about 95 characters. Most tasks start with the same boilerplate ("The person answered
  your question (decision N)") so the visible part says nothing. Show the repo and lane in
  one narrower column and start the task text after the boilerplate.
- The filters row is indented by `--gutter` while the table below is not.

### 9. Usage: the model for table pages

[1920](shots/usage-1920.png).

- Table measure, tiles and the context chart fill the box and the tables read well. Keep it.
- Minor: the numeric columns on the lane and model tables have uneven gaps because the
  first column is `width: auto`; the context chart's label column caps at 18rem so long
  lane names are cut; `usage.css` repeats `.tile` styling that `projects.css` also has for
  `.project-card`, so the card look is defined twice.

### 10. Timeline: wide and good, with small clips

[1920](shots/timeline-1920.png).

- It uses about 1600 of 1920 px with its own `.timeline-page` tokens, the right model for
  a wide visual page.
- Lane labels are cut at 12rem (`--tl-gutter`, `timeline.css:7`: "feat/post-image-light...",
  "review/ubfs-qwen25c...") with room to widen the gutter on a wide window. The repo
  group header uses the page's heading style rather than the shared one.

### 11. Log: readable, but nothing next to it

[1920](shots/log-1920.png).

- The mono log is 68rem, which is a good line length (about 100 characters), and the
  search bar is sticky. At wide sizes the right half of the window is empty; the run's
  facts (agent, model, tokens, cost, commits) would fit there as a rail like the lane page.
- The sticky tools bar bleeds past the column by `--pad` each side, so its rule is wider
  than the text it sits over.

### 12. Decisions: the reference, with one idea left

[1920](shots/decisions-1920.png), [1024](shots/decisions-1024.png).

- At 1920 the card is about 810 px and the page 1120 px; the page-sub paragraph wraps
  with an orphan ("send."), the "Requests no rule could route" block and the request row
  are a different, narrower width from the decision card, and "Show answered decisions"
  is a small link at the bottom.
- Two ideas to use the width: at 1600 and up, the waiting decision on the left and a rail
  on the right with requests and recent answers (the same main plus rail shape as the
  lane page); and align the sub-line, the requests block and the card to one measure.

## The shared fix: one layout and width system

Goal: change a token and every page follows, and no page needs its own `max-width`.

### Tokens (`web/src/styles/theme.css`)

| Token | Value | Replaces |
| --- | --- | --- |
| `--shell-max` | `112rem` (keep) | the box the bar and pages share |
| `--page-max` | `var(--shell-max)` | `--measure-page` 68rem, `--measure-table` 96rem, `.next` 64rem, `.page.chat` 1800px |
| `--measure-prose` | `100ch` | `--measure-read` 76ch and the 100ch in `decisions.css` |
| `--rail` | `20rem` (keep); `--rail-wide: 26rem` | the lane facts column, the chat rail's `clamp` |
| `--page-margin-left` | `auto` beyond `--shell-max` only | `0` |

Breakpoints (in one place, `theme.css` comment): 760 (stack), 1100 (two-column forms,
bar collapse), 1600 (main plus rail on Decisions, Log and Lane), 2000 (page capped and
centred).

### Two page shapes

1. **Fill**: `.page` fills the shell and every table, grid and card list inside it takes
   the free width. Used by Board, Runs, Usage, Project(s), Next up, Timeline, Settings.
2. **Split**: `.page-split`, a `minmax(0, 1fr)` main plus `--rail` side column, collapsing
   to one column below 1100. Used by Lane, Log, Decisions, Chat.

Prose inside either shape sits in a `.prose` box capped at `--measure-prose`, so a card or
a paragraph is readable on any window and the table beside it is not constrained.

### Shared pieces

- **Page head**: one `.page-head` with the title, an optional one-line sub (never wrapping
  to an orphan: cap at `--measure-prose` and let it break naturally), and an actions slot
  on the right for filters and primary buttons. Replaces the per-page header variants
  (Settings has none, Lane and Log shift theirs with a negative margin).
- **Section head**: one `.section-h` (`--t-lg`, 600, optional count) for Board groups,
  Lane sections and Project sections. Muted `--t-sm` is for captions only.
- **Row grid**: a `.row` base and a `--cols` custom property per section
  (`style="--cols: ..."` or a modifier class), so `.row-finished` and `.project-row`
  stop being separate grids. The flexible track is `minmax(0, 1fr)`; only fixed tracks
  truncate.
- **Empty state**: one `.empty` component (a sentence and an optional next step),
  used inline in a section or alone on a page, never centred.
- **Spacing**: no `px` literals; `--s-*` only, so spacing scales with the root size.
- **Bar**: `white-space: nowrap` on nav items, and a collapse step below 1100 px that
  hides the counts and shrinks the nav gap, so nothing wraps or overflows at 1024.

### What not to do

- Do not simply raise `--measure-page`. A 1500 px prose column is worse than a 1100 px
  one. The extra width should go to a second column or a wider table.
- Do not centre the page inside the shell below 2000 px; the bar and the glyph rail are
  left-anchored and the content should stay on them.

## Proposed MRs

Each is one lane's worth. They are ordered so the foundation lands first and the others
can run in parallel after it. Scopes are paths a lane would be scoped to.

| # | MR | Scope | Notes |
| --- | --- | --- | --- |
| 0 | Extend the shots stub: projects, project, unassigned, usage, config and topics answers | `web/shots/**` | Makes these screenshots reproducible with made-up data; no UI change. Can run any time |
| 1 | Layout foundation: tokens, `.page` and `.page-split`, shared head, section head, row grid, empty state, bar at 1024; split `app.css` into per-page files with no behaviour change | `web/src/styles/theme.css`, `web/src/styles/app.css` (and new `web/src/styles/*.css` it splits into), `web/src/App.tsx` | Must land first. The mechanical split first removes merge conflicts for 2 to 6 |
| 2 | Project, Projects and Unassigned: fix the row grids, card grid and repo list | `web/src/pages/Project.tsx`, `web/src/pages/Projects.tsx`, `web/src/styles/projects.css` and their tests | The one real bug; can ship before 1 with a local grid fix |
| 3 | Next up: wide rows, note under title, actions column, one primary | `web/src/pages/Next.tsx`, `web/src/styles/next.css`, `web/src/pages/Next.test.tsx` | Rail Next up (`Backlog`) not touched |
| 4 | Lane, Log and Runs: split pages, timeline clamp, runs task column | `web/src/pages/Lane.tsx`, `web/src/pages/Log.tsx`, `web/src/pages/Runs.tsx`, the lane, log and runs CSS from MR 1 | Reuses the decision card's clamp |
| 5 | Settings and Usage: two-column field grid, shared tile style | `web/src/pages/Settings.tsx`, `web/src/pages/Usage.tsx`, `web/src/styles/settings.css`, `web/src/styles/usage.css` | |
| 6 | Chat, Decisions and Timeline: prose measure, feed event clamp, topic bar wrap, Decisions rail, timeline gutter | `web/src/pages/Chat.tsx`, `web/src/pages/Decisions.tsx`, `web/src/conversation/**`, `web/src/styles/chat.css`, `conversation.css`, `topics.css`, `decisions.css`, `web/src/timeline/timeline.css` | Largest; may split Chat from Decisions and Timeline |

### Recommended order

1. **MR 1** first: it is the only blocker and it is mostly mechanical.
2. **MR 2 and MR 3 in parallel** with or straight after it: the most visible damage, and
   disjoint files. If MR 1 is slow, MR 2 can go first as a local fix.
3. **MR 4**, then **MR 5**, then **MR 6**: by how often the page is open and how much is
   gained. Chat is last because it already uses the width, so what remains there is polish.
4. **MR 0** whenever a lane is free; it is not on the critical path.

## Questions for the maintainer

1. **Fill the shell, or cap tables near 96rem?** The plan fills to `--shell-max`
   (112rem, 1848 px at 1920, 2016 px at 2560). A table at 2016 px is wide; capping tables
   at 96rem and letting only the bar and Timeline use the shell is the alternative.
   Recommendation: fill, and cap the main column of split pages.
2. **Should Decisions get a side rail at 1600 and up?** It spends the free width on
   requests and recent answers. Recommendation: yes, as the last item of MR 6.
3. **Should the board collapse empty projects by default?** Recommendation: yes, under a
   single "N quiet projects" line, expandable.
