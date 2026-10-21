package store

import "time"

// Muster item states. An item is a finding the upkeep pass made, or a fix it applied
// on its own. Open ones wait on the person under "To tidy".
const (
	MusterOpen      = "open"      // a finding, waiting for the person to apply or dismiss it
	MusterApplied   = "applied"   // the fix was done, by the pass or by the person
	MusterUndone    = "undone"    // an applied fix was taken back
	MusterDismissed = "dismissed" // the person said leave it
	MusterCleared   = "cleared"   // the finding stopped being true on its own
)

// MusterItem is one finding or applied fix of the upkeep pass, with the reason. It is
// the log: nothing muster does is left out of it.
type MusterItem struct {
	ID int64 `json:"id"`
	// Key makes a finding unique: the same finding is never made twice, whatever became
	// of the first (applied, undone or dismissed).
	Key string `json:"key"`
	// Kind says what was found, for example "artifact.missing" or "lane.stale".
	Kind string `json:"kind"`
	// SubjectKind and SubjectID name what it is about: artifact, lane, run, next,
	// question or decision.
	SubjectKind string `json:"subject_kind"`
	SubjectID   int64  `json:"subject_id"`
	// Repo is the repo's name when the subject belongs to one.
	Repo   string `json:"repo,omitempty"`
	Reason string `json:"reason"`
	// Fix names the one-click fix; empty when the finding is only to be seen. FixArg
	// is its argument, JSON.
	Fix    string `json:"fix,omitempty"`
	FixArg string `json:"fix_arg,omitempty"`
	// Before is what the fix changed, JSON, so Undo can put it back.
	Before string `json:"before,omitempty"`
	State  string `json:"state"`
	// Auto: the pass applied it on its own, with no click.
	Auto    bool      `json:"auto,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// Applied is when the fix was done. UndoUntil is when undo stops being offered;
	// nil for a fix that cannot be undone.
	Applied   *time.Time `json:"applied,omitempty"`
	UndoUntil *time.Time `json:"undo_until,omitempty"`
}

// MusterFilter narrows MusterItems; a zero field matches everything. Limit 0 means 500.
type MusterFilter struct {
	State string
	Kind  string
	Limit int
}

// MusterPatch is a change to a muster item: nil fields are left alone.
type MusterPatch struct {
	State     *string
	Reason    *string
	Before    *string
	Applied   *time.Time
	UndoUntil *time.Time
}
