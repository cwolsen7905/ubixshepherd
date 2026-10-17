//go:build keepgoing_checks

package daemon

// The keep-going checks, KEEP-C-01 to KEEP-C-12 (spec section 5.6.1), as tests. They are
// the definition of done for the daemon-side drive (next-up #123, PLAN-008) and are
// expected to fail until it lands; run them with `make check-keep-going`. They sit behind
// the keepgoing_checks build tag so `make check` stays green meanwhile. The lane that
// builds the drive must not edit them.
//
// The interface they assume is the smallest the drive needs (stubs in keepgoing_drive.go):
//
//   - The daemon's tick is the existing KeepGoing.EvaluateAll, the call the timer, a run
//     ending and a restart already make. The drive hangs off it: after the limits are
//     checked, while the mode is on, it starts work itself. Nothing here calls a new
//     "start" entry point, and nothing consults desk.wake or an attached client.
//   - KeepGoing.Drive().StartRun is the one place a run is started. The drive calls it
//     with a DriveStart (workspace, approved item, the lane to continue or the zero Lane
//     to open one, and a prompt built from the item). The fake here opens the lane, links
//     it to the item and creates a running run, as the real one will; tests read the
//     calls it recorded.
//   - KeepGoing.Drive().LinkOf says what an open question is linked to (item, lane or
//     topic). Unset, every question is unlinked and blocks nothing. A decision is linked
//     to its run's lane already.
//   - KeepGoing.SinceYouLeft(ctx, ws) is the one summary for the person coming back.
//
// Wording is asserted only where the spec fixes it: ids, titles, lane names and reasons.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// driveRec is the fake StartRun and what it saw.
type driveRec struct {
	mu     sync.Mutex
	starts []DriveStart
	s      *Server
	t      *testing.T
}

// fakeDrive installs a StartRun that opens a lane in the item's repo, links the item and
// records a running run.
func fakeDrive(t *testing.T, s *Server, clk *fakeClock) *driveRec {
	t.Helper()
	d := &driveRec{s: s, t: t}
	s.KeepGoing().Drive().StartRun = func(ctx context.Context, req DriveStart) (store.Run, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		lane := req.Lane
		if lane.ID == 0 {
			repos, err := s.Store.Repos(ctx, req.WorkspaceID)
			if err != nil {
				return store.Run{}, err
			}
			for _, r := range repos {
				if r.Name == req.Item.Repo {
					name := fmt.Sprintf("item-%d", req.Item.ID)
					lane, err = s.Store.CreateLane(ctx, store.Lane{RepoID: r.ID, Name: name, Branch: name, Base: "main",
						Worktree: filepath.Join(r.Path, "wt", name), State: store.LaneOpen})
					if err != nil {
						return store.Run{}, err
					}
					id := lane.ID
					if _, err := s.Store.UpdateNextItem(ctx, req.Item.ID, store.NextPatch{LaneID: &id}); err != nil {
						return store.Run{}, err
					}
				}
			}
			if lane.ID == 0 {
				return store.Run{}, fmt.Errorf("no repo %q", req.Item.Repo)
			}
		}
		run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", Prompt: req.Prompt, State: store.RunRunning, Started: clk.Now()})
		if err != nil {
			return run, err
		}
		req.Lane = lane
		d.starts = append(d.starts, req)
		return run, nil
	}
	return d
}

func (d *driveRec) items() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []int64
	for _, st := range d.starts {
		out = append(out, st.Item.ID)
	}
	return out
}

func (d *driveRec) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.starts)
}

// endRun finishes every running run of the workspace in the given state, the way a run
// ending does, and returns how many.
func endRuns(t *testing.T, s *Server, state string, limit int) int {
	t.Helper()
	ctx := context.Background()
	runs, err := s.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range runs {
		if n == limit {
			break
		}
		r.State = state
		now := time.Now()
		r.Ended = &now
		if err := s.Store.UpdateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
		n++
	}
	return n
}

// tick is one daemon tick: a minute passes and the timer's work runs.
func tick(s *Server, clk *fakeClock) {
	clk.Add(keepGoingTick)
	s.KeepGoing().EvaluateAll(context.Background())
}

// approvedIn adds an approved item for a repo.
func approvedIn(t *testing.T, s *Server, repo, title string) store.NextItem {
	t.Helper()
	it, err := s.Store.AddNextItem(context.Background(), store.NextItem{Title: title, Repo: repo, State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// manyRepos replaces the server's workspace with one of n repos and returns its name list.
func manyRepos(t *testing.T, s *Server, n int) (store.Workspace, []string) {
	t.Helper()
	root := t.TempDir()
	var repos []store.Repo
	var names []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("r%02d", i)
		names = append(names, name)
		repos = append(repos, store.Repo{Name: name, Path: filepath.Join(root, name)})
	}
	ws, err := s.Store.SaveWorkspace(context.Background(), store.Workspace{Name: "many", Path: root}, repos)
	if err != nil {
		t.Fatal(err)
	}
	return ws, names
}

func feedHas(t *testing.T, s *Server, subs ...string) bool {
	t.Helper()
	for _, l := range keepFeed(t, s) {
		ok := true
		for _, sub := range subs {
			ok = ok && strings.Contains(l, sub)
		}
		if ok {
			return true
		}
	}
	return false
}

// KEEP-C-01: nothing attached, one tick, the highest-ranked startable item runs.
func TestKeepGoingStartsWorkWithNoClient(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	d := fakeDrive(t, s, clk)
	cfg := s.LiveConfig()
	cfg.Desk.Wake = config.WakeAttached
	s.applyConfig(cfg)
	first := approvedItem(t, s, "First by rank")
	approvedItem(t, s, "Second by rank")
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 1})
	tick(s, clk)
	got := d.items()
	if len(got) != 1 || got[0] != first.ID {
		t.Fatalf("want one start, for item %d (highest rank); got %v", first.ID, got)
	}
	if !strings.Contains(d.starts[0].Prompt, "First by rank") {
		t.Errorf("the prompt is built from the item: %q", d.starts[0].Prompt)
	}
	if !feedHas(t, s, fmt.Sprint(first.ID), d.starts[0].Lane.Name) {
		t.Errorf("want a feed line naming item %d and lane %s: %q", first.ID, d.starts[0].Lane.Name, keepFeed(t, s))
	}
}

// KEEP-C-02: a run ends, the next startable item starts within a minute, no person turn.
func TestKeepGoingStartsNextWhenRunEnds(t *testing.T) {
	for _, state := range []string{store.RunSucceeded, store.RunFailed} {
		t.Run(state, func(t *testing.T) {
			s, _, clk, _, _ := keepSetup(t)
			ws, repos := manyRepos(t, s, 3)
			d := fakeDrive(t, s, clk)
			a := approvedIn(t, s, repos[0], "A")
			b := approvedIn(t, s, repos[1], "B")
			c := approvedIn(t, s, repos[2], "C")
			keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 1})
			tick(s, clk)
			if got := d.items(); len(got) != 1 || got[0] != a.ID {
				t.Fatalf("first start: %v", got)
			}
			tick(s, clk)
			if d.count() != 1 {
				t.Fatalf("the slot is full: %v", d.items())
			}
			if endRuns(t, s, state, 1) != 1 {
				t.Fatal("no run to end")
			}
			tick(s, clk) // within one minute of the run ending
			if got := d.items(); len(got) != 2 || got[1] != b.ID {
				t.Fatalf("after the run ended (%s) want item %d next (then %d): %v", state, b.ID, c.ID, got)
			}
		})
	}
}

// KEEP-C-03: up to max_runs start, the rest wait and start as runs end.
func TestKeepGoingFillsFreeSlots(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ws, repos := manyRepos(t, s, 13)
	d := fakeDrive(t, s, clk)
	for i, r := range repos {
		approvedIn(t, s, r, fmt.Sprintf("Item %d", i))
	}
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 6})
	tick(s, clk)
	if n := d.count(); n != 6 {
		t.Fatalf("want exactly 6 starts, got %d", n)
	}
	tick(s, clk)
	if n := d.count(); n != 6 {
		t.Fatalf("no more than max_runs: %d starts", n)
	}
	endRuns(t, s, store.RunSucceeded, 2)
	tick(s, clk)
	if n := d.count(); n != 8 {
		t.Fatalf("two slots freed, want 8 starts in all, got %d", n)
	}
	seen := map[int64]bool{}
	for _, id := range d.items() {
		if seen[id] {
			t.Errorf("item %d started twice", id)
		}
		seen[id] = true
	}
}

// KEEP-C-04: with desk.wake attached and the last client gone an hour ago, a run ending
// has the same result as C-02.
func TestKeepGoingIgnoresWakeModeWhileOn(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ws, repos := manyRepos(t, s, 2)
	d := fakeDrive(t, s, clk)
	cfg := s.LiveConfig()
	cfg.Desk.Wake = config.WakeAttached
	s.applyConfig(cfg)
	if cfg.Desk.WakeMode() != config.WakeAttached {
		t.Fatal("setup: wake is not attached")
	}
	approvedIn(t, s, repos[0], "A")
	b := approvedIn(t, s, repos[1], "B")
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 1, IdleHours: 12})
	tick(s, clk)
	clk.Add(time.Hour) // the last client left an hour ago
	endRuns(t, s, store.RunSucceeded, 1)
	s.KeepGoing().EvaluateAll(context.Background())
	if got := d.items(); len(got) != 2 || got[1] != b.ID {
		t.Fatalf("want item %d started with no client attached: %v", b.ID, got)
	}
}

// KEEP-C-05: open questions linked to nothing do not hold an item, nor appear in an alert.
func TestUnlinkedQuestionDoesNotBlockItem(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	d := fakeDrive(t, s, clk)
	it := approvedItem(t, s, "Headings at 600 weight")
	var ids []int64
	for i := 0; i < 14; i++ {
		q, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: fmt.Sprintf("question %d?", i), State: store.QuestionOpen})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, q.ID)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	tick(s, clk)
	if got := d.items(); len(got) != 1 || got[0] != it.ID {
		t.Fatalf("item %d must be startable with 14 unrelated questions open: %v", it.ID, got)
	}
	clk.Add(stallGrace)
	s.KeepGoing().EvaluateAll(ctx)
	for _, a := range stallAlerts(t, s) {
		for _, id := range ids {
			if strings.Contains(a, fmt.Sprintf("question %d ", id)) {
				t.Errorf("the alert blames unlinked question %d: %s", id, a)
			}
		}
	}
}

// KEEP-C-06: a linked blocker holds only its own item.
func TestLinkedBlockerHoldsOnlyItsItem(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ctx := context.Background()
	d := fakeDrive(t, s, clk)
	ws, repos := manyRepos(t, s, 4)
	byQuestion := approvedIn(t, s, repos[0], "Held by a linked question")
	onLane := approvedIn(t, s, repos[1], "Held by a decision on its lane")
	free1 := approvedIn(t, s, repos[2], "Free one")
	free2 := approvedIn(t, s, repos[3], "Free two")
	// The question is linked to the first item, as it will be when created.
	q, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: "which colour?", State: store.QuestionOpen})
	if err != nil {
		t.Fatal(err)
	}
	s.KeepGoing().Drive().LinkOf = func(x store.Question) QuestionLink {
		if x.ID == q.ID {
			return QuestionLink{ItemID: byQuestion.ID}
		}
		return QuestionLink{}
	}
	// The second item has a lane with an open agent decision.
	repo := repoByName(t, s, ws, repos[1])
	l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: repo.ID, Name: "held", Branch: "held", Base: "main",
		Worktree: filepath.Join(repo.Path, "wt", "held"), State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.UpdateNextItem(ctx, onLane.ID, store.NextPatch{LaneID: &l.ID}); err != nil {
		t.Fatal(err)
	}
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", State: store.RunSucceeded, Started: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "which db?", State: store.DecisionOpen}); err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 6})
	tick(s, clk)
	tick(s, clk)
	got := map[int64]bool{}
	for _, id := range d.items() {
		got[id] = true
	}
	if got[byQuestion.ID] {
		t.Errorf("item %d has a linked open question and must not start", byQuestion.ID)
	}
	for _, st := range d.starts {
		if st.Lane.ID == l.ID || st.Item.ID == onLane.ID {
			t.Errorf("lane %s has an open decision and must not be continued: %+v", l.Name, st)
		}
	}
	if !got[free1.ID] || !got[free2.ID] {
		t.Errorf("other items still start: %v", d.items())
	}
}

func repoByName(t *testing.T, s *Server, ws store.Workspace, name string) store.Repo {
	t.Helper()
	repos, err := s.Store.Repos(context.Background(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no repo %s", name)
	return store.Repo{}
}

// KEEP-C-07: a stall is acted on: the item starts, and no "nothing can start" alert.
func TestStallStartsWorkInsteadOfAlerting(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	d := fakeDrive(t, s, clk)
	// Nothing runs at first; the item is approved after the mode is on, as a person would.
	keepOn(t, s, ws, api.KeepGoingSet{})
	s.KeepGoing().EvaluateAll(context.Background())
	it := approvedItem(t, s, "Waiting for a quiet moment")
	for i := 0; i < 2; i++ {
		clk.Add(time.Minute)
		s.KeepGoing().EvaluateAll(context.Background())
	}
	if got := d.items(); len(got) != 1 || got[0] != it.ID {
		t.Fatalf("after 2 minutes want item %d started: %v", it.ID, got)
	}
	if a := stallAlerts(t, s); len(a) != 0 {
		t.Errorf("an item could start, yet: %v", a)
	}
}

// KEEP-C-08: a true stall alerts once, each item with its own blockers.
func TestStallAlertListsEachBlockedItemWithItsOwnBlockers(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	d := fakeDrive(t, s, clk)
	a := approvedItem(t, s, "Alpha")
	b := approvedItem(t, s, "Bravo")
	dep := []int64{a.ID}
	if _, err := s.Store.UpdateNextItem(ctx, b.ID, store.NextPatch{DependsOn: &dep}); err != nil {
		t.Fatal(err)
	}
	q, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: "which db?", State: store.QuestionOpen})
	if err != nil {
		t.Fatal(err)
	}
	stray, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Text: "unrelated?", State: store.QuestionOpen})
	if err != nil {
		t.Fatal(err)
	}
	s.KeepGoing().Drive().LinkOf = func(x store.Question) QuestionLink {
		if x.ID == q.ID {
			return QuestionLink{ItemID: a.ID}
		}
		return QuestionLink{}
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	s.KeepGoing().EvaluateAll(ctx)
	clk.Add(stallGrace)
	s.KeepGoing().EvaluateAll(ctx)
	clk.Add(time.Minute)
	s.KeepGoing().EvaluateAll(ctx)
	if d.count() != 0 {
		t.Fatalf("every item is blocked, yet started: %v", d.items())
	}
	al := stallAlerts(t, s)
	if len(al) != 1 {
		t.Fatalf("want one alert: %v", al)
	}
	text := al[0]
	ia, ib := strings.Index(text, fmt.Sprintf("Item %d ", a.ID)), strings.Index(text, fmt.Sprintf("Item %d ", b.ID))
	if ia < 0 || ib < 0 {
		t.Fatalf("want a line per item: %s", text)
	}
	lineA, lineB := text[ia:], text[ib:]
	if ia < ib {
		lineA, lineB = text[ia:ib], text[ib:]
	} else {
		lineA, lineB = text[ia:], text[ib:ia]
	}
	if !strings.Contains(lineA, fmt.Sprintf("question %d ", q.ID)) {
		t.Errorf("Alpha's blocker is question %d: %s", q.ID, lineA)
	}
	if strings.Contains(lineB, fmt.Sprintf("question %d ", q.ID)) {
		t.Errorf("Bravo is not blocked by Alpha's question: %s", lineB)
	}
	if !strings.Contains(strings.ToLower(lineB), "depend") || !strings.Contains(lineB, fmt.Sprint(a.ID)) {
		t.Errorf("Bravo's blocker is its dependency on item %d: %s", a.ID, lineB)
	}
	if strings.Contains(text, fmt.Sprintf("question %d ", stray.ID)) {
		t.Errorf("an unlinked question is named: %s", text)
	}
	if !strings.Contains(strings.ToLower(lineA), "reply") && !strings.Contains(strings.ToLower(lineA), "answer") {
		t.Errorf("the alert says what the person should do: %s", lineA)
	}
}

// KEEP-C-09: limits hold for the drive's starts, with the reason.
func TestKeepGoingLimitsHoldOnDrive(t *testing.T) {
	t.Run("run cap", func(t *testing.T) {
		s, _, clk, _, _ := keepSetup(t)
		ws, repos := manyRepos(t, s, 3)
		d := fakeDrive(t, s, clk)
		for _, r := range repos {
			approvedIn(t, s, r, "item in "+r)
		}
		keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 2})
		tick(s, clk)
		tick(s, clk)
		if n := d.count(); n > 2 {
			t.Fatalf("run cap 2, %d starts", n)
		}
	})
	t.Run("spend cap", func(t *testing.T) {
		s, _, clk, ws, _ := keepSetup(t)
		d := fakeDrive(t, s, clk)
		approvedItem(t, s, "Costly")
		keepOn(t, s, ws, api.KeepGoingSet{Cap: 2})
		addSpend(t, s, "2026-10-11", 2.5)
		tick(s, clk)
		if d.count() != 0 {
			t.Fatalf("started past the spend cap: %v", d.items())
		}
		if row := keepRow(t, s, ws); row.On || row.ReasonOff != store.KeepOffCap {
			t.Fatalf("want the mode off for the cap: %+v", row)
		}
		if !feedHas(t, s, "spend cap") {
			t.Errorf("the feed gives the reason: %q", keepFeed(t, s))
		}
	})
	t.Run("day end", func(t *testing.T) {
		s, _, clk, ws, _ := keepSetup(t)
		d := fakeDrive(t, s, clk)
		approvedItem(t, s, "Late")
		keepOn(t, s, ws, api.KeepGoingSet{IdleHours: 48})
		clk.Add(24 * time.Hour)
		s.KeepGoing().EvaluateAll(context.Background())
		if d.count() != 0 {
			t.Fatalf("started after the day ended: %v", d.items())
		}
		if row := keepRow(t, s, ws); row.On || row.ReasonOff != store.KeepOffDayEnd {
			t.Fatalf("want the mode off at day end: %+v", row)
		}
	})
	t.Run("lane starts", func(t *testing.T) {
		s, _, clk, ws, lane := keepSetup(t)
		d := fakeDrive(t, s, clk)
		approvedItem(t, s, "Busy lane item")
		keepOn(t, s, ws, api.KeepGoingSet{})
		day := "2026-10-11"
		for i := 0; i < s.LiveConfig().KeepGoing.StartsPerLane(); i++ {
			if _, err := s.Store.CountLaneStart(context.Background(), lane.ID, day); err != nil {
				t.Fatal(err)
			}
		}
		if dec := s.KeepGoing().Allow(context.Background(), ws.ID, KeepContinue, lane.ID); dec.OK {
			t.Fatalf("Allow must refuse a lane out of starts: %+v", dec)
		}
		_ = d
	})
}

// KEEP-C-10: keep-going never starts a planned item, answers an agent decision or merges.
func TestKeepGoingNeverCoversPlannedOrHumanWork(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	d := fakeDrive(t, s, clk)
	if _, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Only planned", Repo: "app", State: store.NextPlanned, Source: store.NextByPerson}); err != nil {
		t.Fatal(err)
	}
	run, err := s.Store.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunSucceeded, Started: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "which db?", State: store.DecisionOpen})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: lane.ID, MR: 7, MRState: "opened"}); err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	for i := 0; i < 3; i++ {
		tick(s, clk)
	}
	if d.count() != 0 {
		t.Errorf("started work that was not approved or not keep-going's: %v", d.items())
	}
	got, err := s.Store.Decision(ctx, dec.ID)
	if err != nil || got.State != store.DecisionOpen || got.Answer != "" {
		t.Errorf("an agent decision must stay open and unanswered: %+v %v", got, err)
	}
	lf, err := s.Store.LaneForge(ctx, lane.ID)
	if err != nil || lf.MRState != "opened" || lf.MergeSHA != "" {
		t.Errorf("the MR must not be merged: %+v %v", lf, err)
	}
}

// KEEP-C-11: the morning summary says what happened overnight.
func TestKeepGoingSinceYouLeftSummary(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, repos := manyRepos(t, s, 4)
	d := fakeDrive(t, s, clk)
	titles := []string{"Nightly alpha", "Nightly bravo", "Nightly charlie", "Nightly delta"}
	for i, r := range repos {
		approvedIn(t, s, r, titles[i])
	}
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 4, IdleHours: 12})
	tick(s, clk)
	if d.count() != 4 {
		t.Fatalf("setup: the drive should have started 4 runs, got %d", d.count())
	}
	endRuns(t, s, store.RunSucceeded, 3)
	endRuns(t, s, store.RunFailed, 1)
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: d.starts[0].Lane.ID, MR: 41, MRState: "opened"}); err != nil {
		t.Fatal(err)
	}
	clk.Add(24 * time.Hour) // the day ends and the mode goes off
	s.KeepGoing().EvaluateAll(ctx)
	sum, err := s.KeepGoing().SinceYouLeft(ctx, ws.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	for _, want := range append(titles, "!41", "failed", offText(store.KeepOffDayEnd)) {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	if !strings.Contains(strings.ToLower(sum), "started") || !strings.Contains(strings.ToLower(sum), "finished") {
		t.Errorf("summary lacks started and finished:\n%s", sum)
	}
}

// KEEP-C-12: an autonomous start is named in the feed: item id, lane and action.
func TestKeepGoingNamesItsActionsInTheFeed(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	d := fakeDrive(t, s, clk)
	it := approvedItem(t, s, "Named")
	keepOn(t, s, ws, api.KeepGoingSet{})
	tick(s, clk)
	if d.count() != 1 {
		t.Fatalf("setup: want one start: %v", d.items())
	}
	lane := d.starts[0].Lane
	if !feedHas(t, s, fmt.Sprintf("item %d", it.ID), lane.Name, "start") {
		t.Errorf("want a line naming item %d, lane %s and the start: %q", it.ID, lane.Name, keepFeed(t, s))
	}
	for _, l := range keepFeed(t, s) {
		if l == "keep-going started the next approved item" {
			t.Errorf("the generic line names nothing: %q", l)
		}
	}
	// The same text reaches status.
	st, err := s.KeepGoing().Status(context.Background(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range st.Recent {
		if strings.Contains(a.Text, fmt.Sprintf("item %d", it.ID)) && strings.Contains(a.Text, lane.Name) {
			found = true
		}
	}
	if !found {
		t.Errorf("status.recent lacks the named action: %+v", st.Recent)
	}
}
