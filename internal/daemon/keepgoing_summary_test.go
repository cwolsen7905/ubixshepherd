package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// countStore counts the per-lane reads the summary makes.
type countStore struct {
	store.Store
	lane, laneForge, lanes, runs atomic.Int64
}

func (c *countStore) Lane(ctx context.Context, id int64) (store.Lane, error) {
	c.lane.Add(1)
	return c.Store.Lane(ctx, id)
}
func (c *countStore) LaneForge(ctx context.Context, id int64) (store.LaneForge, error) {
	c.laneForge.Add(1)
	return c.Store.LaneForge(ctx, id)
}
func (c *countStore) Lanes(ctx context.Context, repo int64) ([]store.Lane, error) {
	c.lanes.Add(1)
	return c.Store.Lanes(ctx, repo)
}
func (c *countStore) Runs(ctx context.Context, lane int64, state string, limit int) ([]store.Run, error) {
	c.runs.Add(1)
	return c.Store.Runs(ctx, lane, state, limit)
}

func TestItemForPicksTheItemThatOwnedTheRun(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	done := t0.Add(2 * time.Hour)
	first := store.NextItem{ID: 1, Created: t0, DoneAt: &done}
	second := store.NextItem{ID: 2, Created: t0.Add(3 * time.Hour)}
	items := []store.NextItem{first, second}
	if it := itemFor(items, store.Run{Started: t0.Add(time.Hour)}); it == nil || it.ID != 1 {
		t.Errorf("a run during the first item: %+v", it)
	}
	if it := itemFor(items, store.Run{Started: t0.Add(4 * time.Hour)}); it == nil || it.ID != 2 {
		t.Errorf("a run during the second item: %+v", it)
	}
	if it := itemFor(nil, store.Run{Started: t0}); it != nil {
		t.Errorf("no item: %+v", it)
	}
}

func TestSinceYouLeftAttributesEachRunToItsLaneItemAndAgent(t *testing.T) {
	s, _, _, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, _ := driveRepos(t, s, 2)
	rs, _ := s.Store.Repos(ctx, ws.ID)
	keepOn(t, s, ws, api.KeepGoingSet{})
	mk := func(repo store.Repo, name, agent string, itemTitle string) {
		l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: repo.ID, Name: name, Branch: name, Base: "main", Worktree: filepath.Join(repo.Path, name), State: store.LaneOpen})
		if err != nil {
			t.Fatal(err)
		}
		if itemTitle != "" {
			it := driveApproved(t, s, repo.Name, itemTitle)
			if _, err := s.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &l.ID}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: agent, State: store.RunSucceeded, Started: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	mk(rs[0], "alpha", "claude", "Alpha work")
	mk(rs[0], "beta", "copilot", "")
	mk(rs[1], "gamma", "opencode", "Gamma work")
	sum, err := s.KeepGoing().SinceYouLeft(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Alpha work" on lane alpha by claude`, "lane beta by copilot", `"Gamma work" on lane gamma by opencode`} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	if strings.Contains(sum, `"Alpha work" on lane beta`) || strings.Contains(sum, `"Gamma work" on lane alpha`) {
		t.Errorf("a run is credited to another lane's item:\n%s", sum)
	}
}

func TestSinceYouLeftReadsAreBoundedByLanesNotRuns(t *testing.T) {
	s, _, _, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, _ := driveRepos(t, s, 3)
	rs, _ := s.Store.Repos(ctx, ws.ID)
	keepOn(t, s, ws, api.KeepGoingSet{})
	var lanes []store.Lane
	for i := 0; i < 4; i++ {
		l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: rs[i%3].ID, Name: fmt.Sprintf("l%d", i), Branch: "b", Base: "main", Worktree: filepath.Join(rs[i%3].Path, fmt.Sprint(i)), State: store.LaneOpen})
		if err != nil {
			t.Fatal(err)
		}
		lanes = append(lanes, l)
	}
	for i := 0; i < 200; i++ {
		if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: lanes[i%4].ID, Agent: "claude", State: store.RunSucceeded, Started: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// Runs from before keep-going was turned on are not read past.
	if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: lanes[0].ID, Agent: "claude", State: store.RunSucceeded, Started: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	cs := &countStore{Store: s.Store}
	s.Store = cs
	if _, err := s.KeepGoing().SinceYouLeft(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	if n := cs.lane.Load(); n > int64(len(lanes)) {
		t.Errorf("Lane called %d times for %d lanes over 200 runs", n, len(lanes))
	}
	if n := cs.laneForge.Load(); n > int64(len(lanes)) {
		t.Errorf("LaneForge called %d times for %d lanes over 200 runs", n, len(lanes))
	}
	if n := cs.runs.Load(); n != 1 {
		t.Errorf("Runs called %d times, want 1", n)
	}
}

func TestSinceYouLeftKeepsRunsOfClosedLanes(t *testing.T) {
	s, _, _, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, _ := driveRepos(t, s, 1)
	rs, _ := s.Store.Repos(ctx, ws.ID)
	keepOn(t, s, ws, api.KeepGoingSet{})
	l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: rs[0].ID, Name: "merged-one", Branch: "b", Base: "main", Worktree: filepath.Join(rs[0].Path, "m"), State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", State: store.RunSucceeded, Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: l.ID, MR: 7, MRState: "merged"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetLaneState(ctx, l.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
	sum, err := s.KeepGoing().SinceYouLeft(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lane merged-one by claude", "!7"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
}
