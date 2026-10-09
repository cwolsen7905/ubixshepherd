package api

import (
	"testing"
)

func TestMRState(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "opened": MRStateOpen, "OPEN": MRStateOpen, "merged": MRStateMerged,
		"closed": MRStateClosed, "locked": MRStateUnknown, "draft?": MRStateUnknown,
	} {
		if got := MRState(in); got != want {
			t.Errorf("MRState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPipelineStatus(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "created": PipelinePending, "preparing": PipelinePending, "running": PipelineRunning,
		"success": PipelinePassed, "failed": PipelineFailed, "canceled": PipelineCanceled,
		"skipped": PipelineSkipped, "manual": PipelineUnknown, "something new": PipelineUnknown,
	} {
		if got := PipelineStatus(in); got != want {
			t.Errorf("PipelineStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
