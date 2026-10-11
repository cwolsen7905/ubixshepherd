package daemon

import (
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// laneActivity picks the latest of a lane's moments: what the store knows of its runs,
// decisions and requests, its newest commit beyond the base, what the forge last said of
// its merge request and pipeline, and its creation as the floor. On a tie the earlier
// listed source wins, so a run that ended as the pipeline finished reads as a run.
func laneActivity(l store.Lane, st store.Activity, commit time.Time, lf store.LaneForge) (*time.Time, string) {
	best, by := l.Created, api.ActivityCreated
	for _, c := range []struct {
		at time.Time
		by string
	}{
		{st.At, st.By},
		{commit, api.ActivityCommit},
		{lf.MRUpdated, api.ActivityMR},
		{lf.PipelineFinished, api.ActivityPipeline},
		{lf.PipelineStarted, api.ActivityPipeline},
	} {
		if c.at.After(best) {
			best, by = c.at, c.by
		}
	}
	return &best, by
}
