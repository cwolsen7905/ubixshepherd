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
| `ship` | An open lane with commits not on its branch at origin, no run going, in a repo whose `autonomy.push` is `shepherd` | Ship (`POST /v1/lanes/{id}/ship`, after Shepherd's own checks) |
| `ship` | The same, in a repo whose `autonomy.push` is `human` | Open the lane; the person pushes it |
| `next` | A Next-up item the desk or an agent proposed, still planned, with no open lane | Review, on the Next up page, where approval already is |

A repo whose `autonomy.push` is `agent` never lists its lanes to ship: the agent pushes
its own. Approving and shipping stay the person's own click; the list gives no tool to the
desk or an agent.

## Where it shows

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
