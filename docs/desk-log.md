# Desk log

`shepherd desk log` writes one JSON line per front desk turn, so you and the desk can look
back after a week of use and see which messages carried signal and which were noise. It is
passive: it reads what Shepherd already keeps and changes nothing about how the desk behaves.

```
shepherd desk log [--since <when>] [--until <when>] [--workspace <id>]
shepherd desk log --summary [--since <when>] [--until <when>] [--json]
```

`--since` and `--until` take a day (`2026-11-01`, local time; `--until` includes that whole
day), an RFC 3339 time, or a number of days before now (`7d`). History before `--since` still
counts when deciding whether a Needs-you item is a repeat.

## One line per turn

| Field | Meaning |
|---|---|
| `turn`, `time` | The turn's id (the thread message that started it) and when it began. |
| `started_by` | `person`, `event` (a digest of swarm events) or `shepherd` (any other message Shepherd sent the desk). |
| `topic` | The title of the topic the turn is filed under, when it has one. |
| `events` | For an event turn, each event the digest carried: `kind`, and `repo`, `lane`, `run` where they can be named. |
| `waiting` | The Waiting-on-you count the desk was shown; absent when it was not shown one. |
| `reply_chars` | The reply's length in characters. |
| `opening` | `needs_you`, `nothing_needs_you`, `other`, or `none` when there was no reply. |
| `needs_you_items` | How many items the reply listed under "Needs you:". |
| `needs_you_repeated` | How many of those name an id (`!12`, `#7`) or link an earlier turn's list named. An item that names neither is never counted as a repeat. |
| `needs_you_keys` | The ids and links those items name. |
| `tools` | The desk's tool calls, by name only. |
| `next_did` | What the person did next; see below. |
| `next_turn`, `next_after_sec` | The person's next message and the seconds from the end of this turn to it. Absent until they write again. |

Replies and messages are not copied into the log: it holds counts, names and ids.

## What the person did next

`next_did` lists every one that applies, or one of the last two on its own.

| Value | Meaning |
|---|---|
| `question_answered`, `question_skipped` | A question the desk asked in this turn was answered or skipped. |
| `decision_answered` | A decision was answered after this turn. When the decision was in this turn's events, it belongs to this turn; otherwise to the last turn to finish before it was answered. |
| `next_up_changed` | A next-up item was approved, deferred or dropped after this turn (to the last turn to finish before it). |
| `follow_up` | The person's next message names an id or link this turn's reply named: a question about something already reported. |
| `nothing_traceable` | None of the above. |
| `no_next_turn` | The person has not written again. |

Only the person's own acts are searched for, between the end of the turn and the end of
their next message. A run of event turns shares one answer: it is credited to the turn that
finished last before it. A follow-up is attention, not action: it does not count as one in
the summary.

## Summary

`--summary` prints totals per day (turns by who started them, and reply characters), the
share of event turns that led to no action by the person, the Needs-you items listed in the
most turns, and the event kinds that most often sat in a turn that led to no action. Event
turns the person has not yet answered with a message are left out of the share.
`--summary --json` prints the same as JSON.

## Limits

The log is derived from the desk's kept conversation (the newest events per workspace, see
`desk.md`), the feed, questions, decisions and topics, so it reaches back only as far as
those do. An event line is matched to the feed item with the same text; one that cannot be
matched has kind `unknown`.
