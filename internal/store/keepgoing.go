package store

import "time"

// Who turned keep-going on or off (KeepGoing.By).
const (
	KeepByCLI    = "cli"
	KeepByWeb    = "web"
	KeepByDesk   = "desk"
	KeepByLimit  = "limit"
	KeepByPerson = "person"
)

// Why keep-going turned itself off (KeepGoing.ReasonOff).
const (
	KeepOffPerson = "person"
	KeepOffCap    = "cap"
	KeepOffBudget = "budget"
	KeepOffIdle   = "idle"
	KeepOffDayEnd = "day_end"
)

// KeepGoing is a workspace's keep-going mode: a flag that lets the front desk act on
// what is already approved without waiting. It lives in the store so it survives a
// restart. Cap, MaxRuns and IdleHours are this session's overrides of keep_going: in
// the config; 0 means the config's.
type KeepGoing struct {
	WorkspaceID int64 `json:"workspace_id"`
	On          bool  `json:"on"`
	// Since is when it was turned on; By who did it. SpentAtStart is the day's spend
	// at that moment, so what it spent since is the difference.
	Since        time.Time `json:"since,omitempty"`
	By           string    `json:"by,omitempty"`
	SpentAtStart float64   `json:"spent_at_start,omitempty"`
	// ReasonOff and OffAt say how and when it last went off.
	ReasonOff string    `json:"reason_off,omitempty"`
	OffAt     time.Time `json:"off_at,omitempty"`
	// LastAction is the last autonomous action or message from the person: idle time
	// counts from it.
	LastAction time.Time `json:"last_action,omitempty"`
	Cap        float64   `json:"cap,omitempty"`
	MaxRuns    int       `json:"max_runs,omitempty"`
	IdleHours  float64   `json:"idle_hours,omitempty"`
}
