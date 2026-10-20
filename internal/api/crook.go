package api

import (
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Crook's verdict log (docs/crook-why.md).
//
//	GET /v1/crook/verdicts        CrookVerdicts, newest first; query rule, kind, action,
//	                              lane, run, since (RFC 3339 or Nd for N days) and limit
//	GET /v1/crook/verdicts/{id}   CrookWhy: one verdict and its path through the stages
//	GET /v1/crook/rules           CrookRules; query since, default 7d
const (
	PathCrookVerdicts = "/v1/crook/verdicts"
	PathCrookRules    = "/v1/crook/rules"
)

// PathCrookVerdict is GET /v1/crook/verdicts/{id}.
func PathCrookVerdict(id int64) string { return PathCrookVerdicts + "/" + strconv.FormatInt(id, 10) }

// CrookVerdicts is the list answer.
type CrookVerdicts struct {
	Verdicts []store.CrookVerdict `json:"verdicts"`
}

// CrookStep is one stage on the path to a verdict: "no opinion" for a stage that had
// nothing to say, "decided" for the one that did.
type CrookStep struct {
	Stage   string `json:"stage"`
	Outcome string `json:"outcome"`
}

// CrookWhy is one verdict with the stages it passed through, in order, ending at the one
// that decided. Path is empty for a verdict logged without a stage.
type CrookWhy struct {
	Verdict store.CrookVerdict `json:"verdict"`
	Path    []CrookStep        `json:"path,omitempty"`
}

// CrookRules is what each rule allowed, held, asked and refused since a time.
type CrookRules struct {
	Since string                 `json:"since"`
	Rules []store.CrookRuleCount `json:"rules"`
}
