package desk

import "context"

// The front desk is told, on every [Shepherd] event turn, what it may do about work
// nobody asked for. That text lives here, once, for the daemon's desk (digest), the
// terminal chat's event text and the standing briefs, so the copies cannot drift
// (docs/keep-going.md, section 5).

// NoNewWork is the sentence when keep-going is off: the desk reports and waits.
const NoNewWork = "Do not start new work they have not asked for."

// KeepGoingOn is the sentence when keep-going is on. The limits are Shepherd's, not the
// desk's: this only removes the wait between approvals and the action.
const KeepGoingOn = "Keep-going is on: start the next approved item and continue approved next steps, unless a decision needs the person. Limits and refusals come from Shepherd; do not work around them."

// EventInstruction is what a [Shepherd] event turn ends with, picked by the keep-going
// flag. Both the daemon's desk and shepherd chat call it, so neither keeps its own copy.
func EventInstruction(keepGoing bool) string {
	s := "Tell the person briefly what matters and what needs them. Route any request that needs routing. "
	if keepGoing {
		return s + KeepGoingOn
	}
	return s + NoNewWork
}

// BriefNoNewWork is the standing instruction's sentence about [Shepherd] events. It holds
// whichever way the flag stands: the flag changes, a standing brief is given once.
const BriefNoNewWork = "do not start new work they have not asked for, unless an event says keep-going is on"

// KeepGoingBrief is the standing paragraph describing keep-going mode and what it never
// covers. Shepherd enforces the limits server-side; a refusal comes back as a tool
// result with its reason, which the desk relays.
const KeepGoingBrief = `- Keep-going mode (keep_going: on, off, status) lets you carry on with what is already approved. The person turns it on in their own words ("keep going"), in a turn they started, and off the same way ("stop and wait for me"); you may call off at any time, and a [Shepherd] event says when it is on. While it is on, in an event turn: start the first approved Next-up item that has no lane (lane_open with its item, then lane_run), continue a run whose next step the repo's autonomy already approves, follow up a failed pipeline, and re-run a run a restart interrupted. It never covers: answering an agent's decision, approving, deferring, dropping or reordering Next-up items, a merge or a push where the repo's rule is human, tags, releases and deploys, or work you only thought of (that is planned, never started). When the next step is one of those, say so in one line and leave it. Shepherd refuses a start past its limits (spend cap, concurrent runs, idle time, end of day, starts per lane) and turns the mode off itself at one; relay the reason and do not work around it.`

// keepGoingOn reads the flag for a workspace. An unreadable flag is off: the cautious
// text is the one that does not start work.
func (d *Desk) keepGoingOn(ctx context.Context) bool {
	row, err := d.m.o.Store.KeepGoing(ctx, d.ws.ID)
	return err == nil && row.On
}
