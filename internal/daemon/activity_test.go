package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestLaneActivityPicksTheLatest(t *testing.T) {
	base := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	lane := store.Lane{Created: at(0)}
	for _, c := range []struct {
		name   string
		st     store.Activity
		commit time.Time
		lf     store.LaneForge
		at     time.Time
		by     string
	}{
		{"nothing", store.Activity{}, time.Time{}, store.LaneForge{}, at(0), api.ActivityCreated},
		{"run", store.Activity{At: at(5), By: store.ActivityRun}, at(3), store.LaneForge{MRUpdated: at(4)}, at(5), store.ActivityRun},
		{"decision", store.Activity{At: at(6), By: store.ActivityDecision}, time.Time{}, store.LaneForge{}, at(6), store.ActivityDecision},
		{"commit", store.Activity{At: at(5), By: store.ActivityRun}, at(9), store.LaneForge{}, at(9), api.ActivityCommit},
		{"mr", store.Activity{At: at(5), By: store.ActivityRun}, at(6), store.LaneForge{MRUpdated: at(7)}, at(7), api.ActivityMR},
		{"pipeline", store.Activity{At: at(5), By: store.ActivityRun}, at(6), store.LaneForge{MRUpdated: at(7), PipelineFinished: at(12)}, at(12), api.ActivityPipeline},
		{"running pipeline", store.Activity{}, time.Time{}, store.LaneForge{PipelineStarted: at(4)}, at(4), api.ActivityPipeline},
		{"tie goes to the run", store.Activity{At: at(5), By: store.ActivityRun}, at(5), store.LaneForge{}, at(5), store.ActivityRun},
	} {
		got, by := laneActivity(lane, c.st, c.commit, c.lf)
		if got == nil || !got.Equal(c.at) || by != c.by {
			t.Errorf("%s: got %v by %s, want %v by %s", c.name, got, by, c.at, c.by)
		}
	}
}

func TestLaneViewTimes(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	ws, fresh := seedLane(t, s, "fresh")
	mr := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: fresh.RepoID, Name: "withmr", Branch: "withmr", Base: "main",
		Worktree: fresh.Worktree + "-mr", State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: l.ID, MR: 4, MRState: "opened", Pipeline: 2, PipelineStatus: "running",
		MRCreated: mr, MRUpdated: mr.Add(time.Hour), PipelineStarted: mr.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var lanes []api.LaneView
	if code := call(t, ts, s.Token, "GET", fmt.Sprintf("%s?workspace_id=%d", api.PathLanes, ws.ID), nil, &lanes); code != 200 {
		t.Fatalf("lanes: %d", code)
	}
	by := map[string]api.LaneView{}
	for _, v := range lanes {
		by[v.Name] = v
	}
	f := by["fresh"]
	if f.LastActivity == nil || f.LastActivityBy != api.ActivityCreated || !f.LastActivity.Equal(fresh.Created) {
		t.Errorf("fresh: %v by %q", f.LastActivity, f.LastActivityBy)
	}
	if f.MRCreated != nil || f.MRUpdated != nil || f.PipelineStarted != nil || f.PipelineFinished != nil {
		t.Errorf("fresh has forge times: %+v", f)
	}
	m := by["withmr"]
	if m.MRCreated == nil || !m.MRCreated.Equal(mr) || m.MRUpdated == nil || !m.MRUpdated.Equal(mr.Add(time.Hour)) {
		t.Errorf("mr times: %v %v", m.MRCreated, m.MRUpdated)
	}
	if m.PipelineStarted == nil || m.PipelineFinished != nil {
		t.Errorf("pipeline times: %v %v", m.PipelineStarted, m.PipelineFinished)
	}
	// The lane was made now and the forge times are in the past, so created leads.
	if m.LastActivityBy != api.ActivityCreated {
		t.Errorf("withmr last activity by %q", m.LastActivityBy)
	}
}
