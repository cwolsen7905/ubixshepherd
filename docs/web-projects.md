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

## The chat dock

`shepherd chat`'s dock (`internal/chat`) shows projects without giving up its sort by
urgency or its budget of a third of the screen. See [chat.md](chat.md) for the keys.

- **The counts row is global.** `2 needs you · 1 broken · 3 working` is the glance that
  matters, and per-project counts would not fit a narrow terminal.
- **The project is in the item row.** When more than one project has items, a lane shows
  as `shop/feat-login`, the way the dock already qualifies a lane name two repos share.
  An item of Unassigned has no prefix. With one project, or none, or a daemon that does
  not list projects, nothing changes.
- **Grouping is on demand and only in the expanded dock.** `/dock` opens the whole dock;
  `p` there, or `/group`, switches it to a header per project with a summary line
  (`shop  3 open · 1 running · 1 waiting · $1.20`). The compact dock stays sorted by
  urgency. The number keys `1` to `9`, or `/group collapse <project>`, fold a project.
  The choice and the folded projects are kept in the daemon's settings (`dock.group` and
  `dock.collapsed`), as the desk's model is.
- **Open lanes and spend come from `GET /v1/projects`; running and waiting come from the
  dock's own rows**, so a summary always agrees with what is listed.
- **Unassigned is hidden when it has nothing to show**, that is, when it holds only idle
  lanes. A project with no open lanes and no items is hidden too.
- **A project's spend is shown only in the expanded dock.** The compact dock's rows are
  for what needs action.
- **Budget pressure has a row of its own while it lasts**, even when the project's lanes
  are quiet: in the warn tone from 80% of a cap, and in the broken tone, marked
  `runs held`, for a hard cap that is reached.
