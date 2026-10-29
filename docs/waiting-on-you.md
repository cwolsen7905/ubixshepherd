# Waiting on you: one list of what needs the person

Status: **built** (Next-up item 54, approved by the maintainer 2026-10-13).

## The problem

Only an agent's `ask_human` became a pinned decision. The other things that need the
person's action lived in the desk's chat text and were easy to lose: a merge request held
at the `require-approval` job, a lane with committed work nobody pushed, a Next-up item
proposed and not yet approved. The person had to dig through the thread to find them.

## The list

`GET /v1/waiting` (`internal/api/waiting.go`, collected by `internal/waiting`) returns the
items with their counts, oldest first. It is derived from the store and the lanes' git
state on each read; nothing is stored for it. `workspace_id` narrows the lanes to one
workspace.

| Kind | What qualifies | Action |
|---|---|---|
| `decision` | An open decision (an agent's `ask_human`) | Answer, on the Decisions page |
| `mr` | An open lane merge request with no approval whose pipeline is held at, or failed at, the `require-approval` job | Approve (the Lane page's approve control: approve, then Shepherd retries that job, since the forge does not re-run on approval) |
| `mr` | An open lane merge request whose pipeline failed elsewhere and no run is working on it | Open the lane; nothing Shepherd can do for the person |
| `mr` | An open lane merge request with unresolved review threads the watcher has not handed back because the fix budget (`watch.fix_tries`) is spent or the lane has no agent conversation | Open the lane; the threads are the person's now (listed once in the feed) |
| `ship` | An open lane with commits not on its branch at origin, no run going, in a repo whose `autonomy.push` is `shepherd` | Ship (`POST /v1/lanes/{id}/ship`, after Shepherd's own checks) |
| `ship` | The same, in a repo whose `autonomy.push` is `human` | Open the lane; the person pushes it |
| `next` | A Next-up item the desk or an agent proposed, still planned, with no open lane | Review, on the Next up page, where approval already is |

A repo whose `autonomy.push` is `agent` never lists its lanes to ship: the agent pushes
its own. Approving and shipping stay the person's own click; the list gives no tool to the
desk or an agent.

## Item ids

Every item has a short id, sent as `id` and shown the same way everywhere, so "D410 yes"
or "Q271 a" can name one item without confusion across messages. The id is computed from
the item's kind and the id of the record it is about; nothing is stored, so it is the same
on every read and does not depend on the item's text.

| Kind | Id | Example | Keyed to |
|---|---|---|---|
| `decision` | `D<decision>` | `D410` | the decision |
| `question` | `Q<question>` | `Q271` | the desk question |
| `request` | `R<request>` | `R111` | the request |
| `next` | `N<item>` | `N255` | the Next-up item (every variant of it shares the id) |
| `scope` | `W<decision>` | `W410` | the approved scope-widening decision |
| `blocked` | `B<run>` | `B318` | the run that reported blocked (`run_id`) |
| `ship` | `S<lane>` | `S42` | the lane |
| `artifact` | `F<lane>` | `F42` | the lane |
| `mr` | `<repo>!<number>` | `app!175` | the repo and its merge request number |

A merge request uses the forge's own reference rather than a lane id, so it can be traced
on the forge; the repo is part of it because merge request numbers repeat across repos.
`ship` and `artifact` items are one per lane, so the lane id names "this lane's unpushed
work" and keeps naming it as commits come and go. Ids are unique within the list, and the
letter keeps `D12` and `Q12` apart. Decisions, requests, questions, next-up items and runs
are never deleted; a lane deleted after it failed to open could free its number, which is
the only way an id could be reused. An item that lacks the record id it is keyed by has no
id.

## Where it shows

- **Ids:** the Inbox row's sub-line, the chat strip's chips and menu, and the desk's line (`[Waiting on you (N)] D410 decision 410 (repo:lane): ...`)
  all lead with the id. A daemon older than this shows none.

- **Web header:** a "N waiting on you" badge, hidden while nothing waits. It opens a
  dropdown with each item and its action. The component reads the endpoint every five
  seconds while the tab is visible.
- **Dock (`shepherd chat`):** a "N waiting on you" count at the end of the counts row.
- **Desk event digest:** the `[Shepherd] Since your last turn` message (the front desk's
  wake-up digest and the terminal chat's brief) ends with one tagged line,
  `[Waiting on you (N)] decision 12 (repo:lane): ...; MR !175 (repo:lane): ...`, and asks the
  desk to repeat it at the end of its reply. The line is left out when nothing waits.

The Next-up count in the header and the dock's "to approve" count are unchanged; this list
includes the same items among the rest.
