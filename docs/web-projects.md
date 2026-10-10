# Web projects

The **Projects** tab of the web UI (`shepherd web`) shows projects, which until now existed
only in `config.yaml` and the store (see [projects.md](projects.md)), and the **Board**
can group its lanes by project. The views are read-only: a project is changed in
[Settings](web-settings.md#projects) (the **Edit** link on a project's page and the **New
project** button on the Projects page lead there) or the configuration file, and its brief
is approved by the person through the front desk.

## Projects view

- **List** (`#/projects`): a card per project with its repos, open lanes, running lanes,
  what waits on the person, and spend against its budget. Repos that are in no project
  are one more card, **Unassigned**, shown only when it holds a repo.
- **Project page** (`#/projects/<name>`, and `#/unassigned`):
  - its repos, with spend today and against the budget's period (a bar, the words, and a
    note at 80% and at the cap; a project with no cap shows the spend only);
  - what waits on the person: open decisions of its lanes and requests of its lanes, or
    addressed to the project, that are not over;
  - open lanes with their merge request and pipeline badge, as on the Board;
  - recent runs (the last 20);
  - the brief: the approved text with its age and a note when it is past
    `brief_max_age`, and any open draft marked as not approved. Only the approved text is
    ever given to agents. Unassigned has no brief and no budget.

Spend is the same figure the dispatcher holds runs against (`dispatch.BudgetLines`), with
today's dollars beside it when the period is a month. Token and context detail stays on
the [Usage](usage-stats.md) page: the usage stats are not cut by project, and the store
knows no projects, so the page links there rather than repeating it.

## Board grouped by project

The Board's rows are unchanged; they sit under a header per project with a summary
(`3 open · 1 running · 1 waiting · $1.20 today`), collapsible, projects by name and
Unassigned last. A project with nothing needing attention still shows its header, since
an empty project is information.

- **Open** is the daemon's count of open lanes, including idle ones the Board has no row
  for. **Running** and **waiting** are counted from the rows beneath, so a summary never
  disagrees with them. **Spend** is today's dollars from the daemon.
- **View: By project / Flat** is remembered in the browser (`localStorage`, key
  `shepherd.board.view`), as are the collapsed groups. With no project configured the
  toggle is not shown and the Board is flat as before.
- Keyboard navigation (`j`/`k`) skips rows of collapsed groups.
- Closed lanes stay in the flat **Finished** history below the groups: they carry no
  attention state, and that list is a range filter more than a status.

## API

All read-only, open to browser sessions and the front desk like the other reads, and
refused to agents' worker tokens.

| Route | Answers |
|---|---|
| `GET /v1/projects` | `{projects: [ProjectSummary]}`: each configured project by name, then Unassigned (`unassigned: true`, empty name) when some repo is in none |
| `GET /v1/projects/{name}` | `ProjectDetail`: the summary, `open_lanes`, `runs`, `pending_decisions`, `pending_requests` and `brief` (a `BriefView`) |
| `GET /v1/unassigned` | `ProjectDetail` for the repos in no project, without a brief or budget |
| `GET /v1/projects/{name}/brief` | unchanged: the brief alone |

Unassigned has its own path so that no project name can clash with it. A project's
summary is built from the daemon's own reads (lanes, runs, decisions, requests, spend
rollup and the live configuration); nothing new is stored.

## Later: the chat dock

Design notes only; `internal/chat` is not touched by this work. The terminal dock
(`internal/chat/dock.go`) already shows what the Board shows, as a counts row and
attention-sorted items within a budget of a third of the screen. Bringing projects in
should keep that budget and the sort, and add grouping only where it earns its rows.

- **Counts row stays global.** Today's `2 needs you · 1 broken · 3 working` is the glance
  that matters; per-project counts would make it wider than a narrow terminal. Keep it,
  and put the project in the item rows.
- **Name the project on the item, not a header.** A header per project costs a row each
  and the dock has six or so. Prefix a lane with its project when more than one project
  has items (`shop/feat-login`), the way the dock already qualifies a lane name two repos
  share (`laneClashes`). With one project, or none, it shows nothing new.
- **Group only on demand.** Sorting by urgency is the dock's point; grouping by project
  breaks it. Offer a toggle, as the web does: a key or a `/group` command switches the
  expanded dock (the Ctrl-O view, which has room) to project headers with a summary
  line each (`shop  3 open · 1 running · 1 waiting · $1.20`), and leaves the compact dock
  sorted by urgency. Remember the choice and the collapsed projects in the client's
  settings (`/v1/settings`), as the desk model is, not a file of the dock's own.
- **Share the arithmetic.** The grouping and summary rules are small and live in the web
  model (`web/src/model/projects.ts`). The terminal needs the same numbers, so the
  summary is worth computing once, in the daemon, as `GET /v1/projects` already does, and
  the dock should read it rather than re-derive it. The one exception is running and
  waiting, which the web counts from the rows; the dock has the same rows, and should do
  the same, so a summary never disagrees with what is listed.
- **Budget pressure is the one thing worth a warning colour.** A project at 80% of its
  cap, or at a hard cap and holding runs, deserves a dock row of its own even when its
  lanes are quiet, in the broken tone for a hard hold and the warn tone otherwise. It is
  already a feed item when it happens; the dock should keep it as state while it lasts.
- **The front desk gets the same view.** Its operator tools can read `GET /v1/projects`
  and answer "what is the shop project doing?" without reading every lane. Worth adding
  as a tool once the shape has settled in the web; the desk reads only, so nothing about
  approvals changes.
- **Open questions for the person:** whether Unassigned should be hidden in the dock when
  it holds only idle lanes; and whether a project's spend belongs in the compact dock at
  all, or only in the expanded view.
