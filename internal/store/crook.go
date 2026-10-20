package store

import "time"

// CrookVerdict is one logged Crook verdict (docs/crook-why.md). Facts is a JSON object.
type CrookVerdict struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	Action      string    `json:"action"`
	Actor       string    `json:"actor"`
	Kind        string    `json:"kind"`
	Rule        string    `json:"rule"`
	RuleVersion int       `json:"rule_version"`
	Stage       string    `json:"stage"`
	Reason      string    `json:"reason,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	Lane        string    `json:"lane,omitempty"`
	Run         int64     `json:"run,omitempty"`
	Facts       string    `json:"facts,omitempty"`
}

// CrookFilter selects verdicts; a zero field matches all. Limit 0 means 50.
type CrookFilter struct {
	Rule, Kind, Action, Lane string
	Run                      int64
	Since                    time.Time
	Limit                    int
}

// CrookRuleCount is how many verdicts of each kind one rule gave in a period.
type CrookRuleCount struct {
	Rule    string `json:"rule"`
	Proceed int    `json:"proceed"`
	Ask     int    `json:"ask"`
	Hold    int    `json:"hold"`
	Refuse  int    `json:"refuse"`
}
