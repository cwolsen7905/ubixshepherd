package store

import "time"

// DeskLogEvent is one swarm event a desk turn carried, with the repo, lane and run it
// was about as they were named when the turn began.
type DeskLogEvent struct {
	Kind string `json:"kind"`
	Repo string `json:"repo,omitempty"`
	Lane string `json:"lane,omitempty"`
	Run  int64  `json:"run,omitempty"`
}

// DeskTurnLog is what is worth keeping about a desk turn at the moment it began, for the
// desk log (docs/desk-log.md): what the conversation's own events cannot say later,
// because the conversation is trimmed and topics and lanes move. The rest of the log is
// derived at read time.
type DeskTurnLog struct {
	WorkspaceID int64 `json:"workspace_id"`
	Turn        int64 `json:"turn"`
	// StartedBy is who began the turn: person, event or shepherd.
	StartedBy string `json:"started_by"`
	// Topic is the title of the topic the turn was filed under then, "" for none.
	Topic string `json:"topic,omitempty"`
	// Waiting is the Waiting-on-you count shown to the desk; nil when none was shown.
	Waiting *int           `json:"waiting,omitempty"`
	Events  []DeskLogEvent `json:"events,omitempty"`
	Created time.Time      `json:"created"`
}
