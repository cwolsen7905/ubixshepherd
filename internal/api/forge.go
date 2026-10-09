package api

import "strings"

// MR states in a LaneView's mr_state: the forge's own states mapped to a small closed
// set. A lane with no merge request has no mr_state (the field is absent).
const (
	MRStateOpen    = "open"
	MRStateMerged  = "merged"
	MRStateClosed  = "closed"
	MRStateUnknown = "unknown" // any state the forge reports that is not one of the above
)

// Pipeline statuses in a LaneView's pipeline_status: the forge's statuses mapped to a
// closed set. A lane whose merge request has no pipeline (as far as Shepherd has seen)
// has no pipeline_status (the field is absent).
const (
	PipelinePending  = "pending" // created, queued, preparing or scheduled
	PipelineRunning  = "running"
	PipelinePassed   = "passed"
	PipelineFailed   = "failed"
	PipelineCanceled = "canceled"
	PipelineSkipped  = "skipped"
	PipelineUnknown  = "unknown" // anything else, such as a manual pipeline
)

// MRState maps a forge's merge request state ("opened" on GitLab, "OPEN" on GitHub) to
// one of the MRState values. "" stays "": there is no merge request to describe.
func MRState(forgeState string) string {
	switch strings.ToLower(strings.TrimSpace(forgeState)) {
	case "":
		return ""
	case "opened", "open":
		return MRStateOpen
	case "merged":
		return MRStateMerged
	case "closed":
		return MRStateClosed
	}
	return MRStateUnknown
}

// PipelineStatus maps a forge's pipeline status to one of the Pipeline values. "" stays
// "": no pipeline has been seen.
func PipelineStatus(forgeStatus string) string {
	switch strings.ToLower(strings.TrimSpace(forgeStatus)) {
	case "":
		return ""
	case "created", "waiting_for_resource", "preparing", "pending", "scheduled", "queued":
		return PipelinePending
	case "running":
		return PipelineRunning
	case "success", "passed":
		return PipelinePassed
	case "failed":
		return PipelineFailed
	case "canceled", "cancelled", "canceling":
		return PipelineCanceled
	case "skipped":
		return PipelineSkipped
	}
	return PipelineUnknown
}
