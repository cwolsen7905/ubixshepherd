package api

import "time"

// Keep-going mode (docs/keep-going.md): a per-workspace switch that lets the front desk
// act on what is already approved without waiting between approvals and action.
//
//	GET  /v1/keep-going?workspace_id=N  KeepGoingStatus
//	POST /v1/keep-going                 KeepGoingSet, answers KeepGoingStatus
//
// Turning it on is the person's: the daemon refuses it from an agent's token and from
// the front desk outside a turn the person started. Off is always allowed. The daemon
// turns it off itself at a limit and says why in Off.
const PathKeepGoing = "/v1/keep-going"

// KeepGoingSet is the body of POST /v1/keep-going. WorkspaceID 0 means the daemon's
// only workspace. On with Cap, MaxRuns or IdleHours set overrides those limits for
// this session; 0 keeps the config's.
type KeepGoingSet struct {
	WorkspaceID int64   `json:"workspace_id,omitempty"`
	On          bool    `json:"on"`
	Cap         float64 `json:"cap,omitempty"`
	MaxRuns     int     `json:"max_runs,omitempty"`
	IdleHours   float64 `json:"idle_hours,omitempty"`
}

// KeepGoingStatus is the mode, its limits and how close each is.
type KeepGoingStatus struct {
	WorkspaceID int64 `json:"workspace_id"`
	On          bool  `json:"on"`
	// Since and By are when and who turned it on, while it is on.
	Since time.Time `json:"since,omitempty"`
	By    string    `json:"by,omitempty"`
	// ReasonOff and OffAt say how it last went off: person, cap, budget, idle or
	// day_end.
	ReasonOff string    `json:"reason_off,omitempty"`
	OffAt     time.Time `json:"off_at,omitempty"`
	// SpentUSD is what the day's spend grew by since it was turned on, against CapUSD.
	SpentUSD float64 `json:"spent_usd"`
	CapUSD   float64 `json:"cap_usd"`
	// RunsNow is how many runs are running in the workspace, against MaxRuns.
	RunsNow int `json:"runs_now"`
	MaxRuns int `json:"max_runs"`
	// IdleHours is the limit; LastAction is when the idle time counts from.
	IdleHours  float64   `json:"idle_hours"`
	LastAction time.Time `json:"last_action,omitempty"`
	// DayEnd is when it will turn itself off at the latest, while it is on.
	DayEnd time.Time `json:"day_end,omitempty"`
	// LaneStarts is how many autonomous starts a lane may have in a day.
	LaneStarts int `json:"lane_starts"`
	// BudgetUSD is the daily budget when one is set (0: none) and SpentTodayUSD the
	// day's spend, which crossing turns the mode off.
	BudgetUSD     float64 `json:"budget_usd,omitempty"`
	SpentTodayUSD float64 `json:"spent_today_usd"`
	// Recent is the last ten keep-going feed items, oldest first: on, off and each
	// autonomous action.
	Recent []KeepGoingAction `json:"recent,omitempty"`
	// Summary is what keep-going did since it was last turned on: items started, runs
	// finished and failed, merge requests, and why it stopped. Empty if never turned on.
	Summary string `json:"summary,omitempty"`
}

// KeepGoingAction is one keep-going feed item as status lists it.
type KeepGoingAction struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
	Lane int64     `json:"lane,omitempty"`
}
