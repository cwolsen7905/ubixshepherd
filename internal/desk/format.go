package desk

// How the front desk opens a message. The text lives here once, for the daemon's desk
// (Brief and the digest) and shepherd chat (DeskBrief and its event text), so the copies
// cannot drift (the same pattern as keepgoing.go).

// NothingNeedsYou is the exact opening line when nothing waits on the person.
const NothingNeedsYou = "Nothing needs you."

// NeedsYouOpening is the opening when something does.
const NeedsYouOpening = "Needs you:"

// FormatBrief is the standing paragraph on how every message to the person is laid out.
const FormatBrief = `- Every message to the person opens with one of two things, so they see at once whether it wants anything from them. First line "` + NeedsYouOpening + `" followed by a numbered list, one line per item: what to do, then a link ("1. Approve [!287](url): inspector, pipeline green, threads resolved."). Or, when nothing waits on them, exactly the line "` + NothingNeedsYou + `". Then "Status:" with short bullets, one per lane or MR; background, if any, comes last. If you ask something, it is item 1 under "` + NeedsYouOpening + `", phrased as a yes/no or a short choice: one question per message, or a numbered list. List an item once per message, and never cut one off mid-word: shorten it to a whole phrase.
- The tagged "[Waiting on you (N)] ..." line in an event turn is context for you, not text for the person: never paste or quote it. Name only items that are new since the person's last turn or that you ask for something on now, and point to the Inbox for the rest ("10 more in Inbox").`

// WaitingInstruction follows the tagged waiting line in an event turn.
const WaitingInstruction = "That line is context for you: do not paste it. Open your reply with \"" + NeedsYouOpening + "\" (numbered) or \"" + NothingNeedsYou + "\", name only items new since the person's last turn or that you ask for now, and point to the Inbox for the rest."
