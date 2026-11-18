# Inbox entries that carry their context

An Inbox row used to be a truncated first line. Opening it, or asking the desk, was the only
way to learn what it was about. Each entry now shows four things under its title.

| Part | Source |
| --- | --- |
| What it is | kind, repo and lane, and a link to the merge request when the lane has one |
| Why it waits | a fixed sentence per kind, plus the daemon's detail line when it has one |
| The choices | a decision's options, read from the live decision list; a next-up suggestion's buttons stay as they are |
| The recommendation | a decision's recommendation and its reason; a question shows its full text |

## Stale entries

An entry is flagged "Probably finished" when its subject is over: its merge request has merged
or closed, or its lane has closed. The flag names the reason and keeps the row, so the person
decides; nothing is closed by the page. The muster's out-of-date check still closes questions
and decisions of closed lanes on its own schedule.

## Where it lives

The page builds the parts with `inboxContext` in `web/src/pages/Inbox.tsx` from the snapshot's
decisions and lanes, so no API change is needed. Questions arrive with their text in the
waiting item, so a question's context is that text.

## Left out

- A question's choices: the desk's questions carry text only.
- Lanes missing from the snapshot are never flagged: absence is not evidence.
