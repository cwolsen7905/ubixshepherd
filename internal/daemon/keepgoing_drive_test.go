package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// driveRecorder is a StartRun that records its calls and starts a running run on the lane
// (opening none, so an item's repo has to hold a lane of its own).
type driveRecorder struct {
	mu    sync.Mutex
	calls []DriveStart
}

func (r *driveRecorder) install(s *Server, clk *fakeClock) {
	s.KeepGoing().Drive().StartRun = func(ctx context.Context, req DriveStart) (store.Run, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if req.Lane.ID == 0 {
			repos, _ := s.Store.Repos(ctx, req.WorkspaceID)
			for _, rp := range repos {
				if rp.Name == req.Item.Repo {
					name := fmt.Sprintf("n-%d", req.Item.ID)
					l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: rp.ID, Name: name, Branch: name, Base: "main", Worktree: filepath.Join(rp.Path, name), State: store.LaneOpen})
					if err != nil {
						return store.Run{}, err
					}
					req.Lane = l
					id := l.ID
					s.Store.UpdateNextItem(ctx, req.Item.ID, store.NextPatch{LaneID: &id})
				}
			}
		}
		run, err := s.Store.CreateRun(ctx, store.Run{LaneID: req.Lane.ID, Agent: "claude", State: store.RunRunning, Started: clk.Now()})
		r.calls = append(r.calls, req)
		return run, err
	}
}

func (r *driveRecorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		out = append(out, fmt.Sprintf("%s:%d", c.Action, c.Item.ID))
	}
	return out
}

func driveTick(s *Server, clk *fakeClock) {
	clk.Add(keepGoingTick)
	s.KeepGoing().EvaluateAll(context.Background())
}

func TestDriveStartsByRankAndNeverPlanned(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	ctx := context.Background()
	rec := &driveRecorder{}
	rec.install(s, clk)
	repos := []string{}
	ws2, names := driveRepos(t, s, 3)
	repos = append(repos, names...)
	ws = ws2
	if _, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Not yet", Repo: repos[0], State: store.NextPlanned, Source: store.NextByPerson}); err != nil {
		t.Fatal(err)
	}
	a := driveApproved(t, s, repos[1], "A")
	b := driveApproved(t, s, repos[2], "B")
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 6})
	driveTick(s, clk)
	got := rec.actions()
	want := []string{fmt.Sprintf("start_item:%d", a.ID), fmt.Sprintf("start_item:%d", b.ID)}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestDriveStartsOneItemPerRepoPerPass(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	rec := &driveRecorder{}
	rec.install(s, clk)
	a := approvedItem(t, s, "First in app")
	approvedItem(t, s, "Second in app")
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 6})
	driveTick(s, clk)
	if got := rec.actions(); len(got) != 1 || got[0] != fmt.Sprintf("start_item:%d", a.ID) {
		t.Fatalf("two lanes in one repo would share its scope: %v", got)
	}
}

func TestDriveContinuesAndReRunsLanes(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	rec := &driveRecorder{}
	rec.install(s, clk)
	// A lane that never ran: its approved step starts.
	fresh := approvedItem(t, s, "Fresh lane")
	if _, err := s.Store.UpdateNextItem(ctx, fresh.ID, store.NextPatch{LaneID: &lane.ID}); err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	driveTick(s, clk)
	if got := rec.actions(); len(got) != 1 || got[0] != fmt.Sprintf("continue:%d", fresh.ID) {
		t.Fatalf("want a continue: %v", got)
	}
	// A crash-interrupted run is re-run; one that ended well, or that Shepherd stopped
	// for a restart (the restart resumes it), is left alone.
	for _, c := range []struct {
		state, reason, want string
	}{
		{store.RunSucceeded, "", ""},
		{store.RunFailed, "", ""},
		{store.RunStopped, "", ""},
		{store.RunInterrupted, store.ReasonRestart, ""},
		{store.RunInterrupted, "", "rerun"},
	} {
		endRunsT(t, s, c.state, c.reason)
		before := len(rec.actions())
		driveTick(s, clk)
		got := rec.actions()
		if c.want == "" && len(got) != before {
			t.Errorf("%s/%s: started %v", c.state, c.reason, got[before:])
		}
		if c.want != "" && (len(got) != before+1 || !strings.HasPrefix(got[before], c.want)) {
			t.Errorf("%s: want %s, got %v", c.state, c.want, got[before:])
		}
	}
}

// endRunsT ends every running run in state, with reason.
func endRunsT(t *testing.T, s *Server, state, reason string) {
	t.Helper()
	ctx := context.Background()
	runs, err := s.Store.Runs(ctx, 0, store.RunRunning, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		// A new latest run to end, so the lane's last run is in the state under test.
		all, _ := s.Store.Runs(ctx, 0, "", 1)
		if len(all) == 0 {
			t.Fatal("no runs")
		}
		r, err := s.Store.CreateRun(ctx, store.Run{LaneID: all[0].LaneID, Agent: "claude", State: store.RunRunning, Started: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		runs = []store.Run{r}
	}
	for _, r := range runs {
		now := time.Now()
		r.State, r.Ended, r.Reason = state, &now, reason
		if err := s.Store.UpdateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDriveHoldsAVagueItemAndStartsOthers(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ws, repos := driveRepos(t, s, 2)
	rec := &driveRecorder{}
	rec.install(s, clk)
	s.KeepGoing().Drive().Ready = driveReady
	vague := driveApproved(t, s, repos[0], "Make it nicer")
	ok, err := s.Store.AddNextItem(context.Background(), store.NextItem{Title: "Clear", Repo: repos[1], Plan: "docs/plan.md", State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	driveTick(s, clk)
	driveTick(s, clk)
	if got := rec.actions(); len(got) != 1 || got[0] != fmt.Sprintf("start_item:%d", ok.ID) {
		t.Fatalf("only the clear item starts: %v", got)
	}
	n := 0
	for _, l := range keepFeed(t, s) {
		if strings.Contains(l, fmt.Sprintf("could not start item %d", vague.ID)) && strings.Contains(l, "no plan doc") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the feed names the vague item once, got %d: %q", n, keepFeed(t, s))
	}
}

func TestDrivePromptCarriesTheItem(t *testing.T) {
	p := drivePrompt(store.NextItem{ID: 9, Title: "Do it", Plan: "docs/x.md", DoneWhen: "tests pass", Note: "mind the cache"})
	for _, want := range []string{"item 9", "Do it", "docs/x.md", "tests pass", "mind the cache"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q: %s", want, p)
		}
	}
}

func TestDriveLaneNameIsAValidSlug(t *testing.T) {
	for title, want := range map[string]string{
		"Keep-going drive (ADR 7): the daemon!": "item-124-keep-going-drive-adr",
		"   ":                                   "item-124",
	} {
		if got := driveLaneName(store.NextItem{ID: 124, Title: title}); got != want {
			t.Errorf("%q: got %q, want %q", title, got, want)
		}
	}
}

// The questions a topic holds block the item that shares the topic, and nothing else.
func TestDriveTopicLinkedQuestionBlocksItsItem(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, repos := driveRepos(t, s, 2)
	rec := &driveRecorder{}
	rec.install(s, clk)
	held := driveApproved(t, s, repos[0], "In the topic")
	free := driveApproved(t, s, repos[1], "Elsewhere")
	tp, err := s.Store.AddTopic(ctx, store.Topic{Title: "Colour"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.LinkTopic(ctx, store.TopicLink{TopicID: tp.ID, Kind: store.TopicNextItem, Ref: fmt.Sprint(held.ID), By: store.TopicLinkPerson}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, TopicID: tp.ID, Text: "which colour?", State: store.QuestionOpen}); err != nil {
		t.Fatal(err)
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	driveTick(s, clk)
	if got := rec.actions(); len(got) != 1 || got[0] != fmt.Sprintf("start_item:%d", free.ID) {
		t.Fatalf("the topic's question holds only its item: %v", got)
	}
}

func TestDriveStartWithoutRunnerStartsNothing(t *testing.T) {
	s, _, clk, ws, _ := keepSetup(t)
	approvedItem(t, s, "Waits for a runner")
	keepOn(t, s, ws, api.KeepGoingSet{})
	driveTick(s, clk) // no StartRun, no Runner: the drive must not panic or start
	if row := keepRow(t, s, ws); !row.On {
		t.Fatalf("mode: %+v", row)
	}
}

// driveOpen opens a real lane for the item, scoped to the repo, links it and names it.
func TestDriveOpenMakesAndLinksTheItemsLane(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@e", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@e"} {
		t.Setenv(k, v)
	}
	s, _ := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	g := func(dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	origin := filepath.Join(root, "origin.git")
	g(root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	g(root, "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644)
	g(seed, "add", ".")
	g(seed, "commit", "-q", "-m", "init")
	g(seed, "push", "-q", "origin", "HEAD:main")
	wsDir := filepath.Join(root, "ws")
	os.MkdirAll(wsDir, 0o755)
	g(wsDir, "clone", "-q", origin, filepath.Join(wsDir, "app"))
	ws, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: wsDir}, []store.Repo{{Name: "app", Path: filepath.Join(wsDir, "app")}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	s.Fold.Config, s.Fold.Store = cfg, s.Store
	it, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "Real lane", Repo: "app", State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := s.driveOpen(ctx, DriveStart{WorkspaceID: ws.ID, Item: it})
	if err != nil {
		t.Fatal(err)
	}
	if lane.Name != driveLaneName(it) || lane.State != store.LaneOpen || lane.Origin.Via != "keep-going" {
		t.Errorf("lane: %+v", lane)
	}
	got, err := s.Store.NextItem(ctx, it.ID)
	if err != nil || got.LaneID != lane.ID {
		t.Errorf("the item is linked to its lane: %+v %v", got, err)
	}
	// A second item in the same repo is refused by the whole-repo scope, naming the lane.
	it2, _ := s.Store.AddNextItem(ctx, store.NextItem{Title: "Second", Repo: "app", State: store.NextApproved, Source: store.NextByPerson})
	if _, err := s.driveOpen(ctx, DriveStart{WorkspaceID: ws.ID, Item: it2}); err == nil || !strings.Contains(err.Error(), lane.Name) {
		t.Errorf("want a refusal naming lane %s: %v", lane.Name, err)
	}
}

// driveRepos replaces the server's workspace with one of n repos named r00, r01 ...
func driveRepos(t *testing.T, s *Server, n int) (store.Workspace, []string) {
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

// driveApproved adds an approved item for a repo.
func driveApproved(t *testing.T, s *Server, repo, title string) store.NextItem {
	t.Helper()
	it, err := s.Store.AddNextItem(context.Background(), store.NextItem{Title: title, Repo: repo, State: store.NextApproved, Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestSinceYouLeftIsEmptyBeforeTheModeEverRan(t *testing.T) {
	s, _, _, ws, _ := keepSetup(t)
	sum, err := s.KeepGoing().SinceYouLeft(context.Background(), ws.ID)
	if err != nil || sum != "" {
		t.Fatalf("got %q, %v", sum, err)
	}
}

func TestSinceYouLeftSaysWhatHappenedAndWhyItStopped(t *testing.T) {
	s, _, clk, _, _ := keepSetup(t)
	ctx := context.Background()
	ws, repos := driveRepos(t, s, 2)
	rec := &driveRecorder{}
	rec.install(s, clk)
	driveApproved(t, s, repos[0], "Gamma work")
	driveApproved(t, s, repos[1], "Delta work")
	keepOn(t, s, ws, api.KeepGoingSet{MaxRuns: 2, IdleHours: 12})
	driveTick(s, clk)
	runs, _ := s.Store.Runs(ctx, 0, store.RunRunning, 10)
	if len(runs) != 2 {
		t.Fatalf("setup: %d runs", len(runs))
	}
	now := time.Now()
	runs[0].State, runs[0].Ended = store.RunSucceeded, &now
	runs[1].State, runs[1].Ended, runs[1].Error = store.RunFailed, &now, "tests failed\nsecond line"
	for _, r := range runs {
		if err := s.Store.UpdateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: runs[0].LaneID, MR: 12, MRState: "merged"}); err != nil {
		t.Fatal(err)
	}
	clk.Add(24 * time.Hour)
	s.KeepGoing().EvaluateAll(ctx)
	sum, err := s.KeepGoing().SinceYouLeft(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Gamma work", "Delta work", "!12", "merged", "tests failed", offText(store.KeepOffDayEnd)} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	if strings.Contains(sum, "second line") {
		t.Errorf("only the first line of an error:\n%s", sum)
	}
	st, err := s.KeepGoing().Status(ctx, ws.ID)
	if err != nil || st.Summary != sum {
		t.Errorf("status carries the summary: %v\n%s", err, st.Summary)
	}
}

// A start that fails does not use the lane's starts for the day, so a lane that keeps
// failing to start is not handed back for runs that never began.
func TestDriveFailedStartDoesNotUseTheLanesStarts(t *testing.T) {
	s, _, clk, ws, lane := keepSetup(t)
	ctx := context.Background()
	it := approvedItem(t, s, "Cannot start yet")
	if _, err := s.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &lane.ID}); err != nil {
		t.Fatal(err)
	}
	fail := true
	var calls int
	s.KeepGoing().Drive().StartRun = func(ctx context.Context, req DriveStart) (store.Run, error) {
		calls++
		if fail {
			return store.Run{}, fmt.Errorf("the agent is not installed")
		}
		return s.Store.CreateRun(ctx, store.Run{LaneID: req.Lane.ID, Agent: "claude", State: store.RunRunning, Started: clk.Now()})
	}
	keepOn(t, s, ws, api.KeepGoingSet{})
	max := s.LiveConfig().KeepGoing.StartsPerLane()
	for i := 0; i < max+2; i++ {
		driveTick(s, clk)
	}
	if calls != max+2 {
		t.Fatalf("every tick should try again: %d calls", calls)
	}
	if n, err := s.Store.LaneStarts(ctx, lane.ID, clk.Now().Format("2006-01-02")); err != nil || n != 0 {
		t.Fatalf("failed starts used the lane's starts: %d %v", n, err)
	}
	for _, l := range keepFeed(t, s) {
		if strings.Contains(l, "handed lane") {
			t.Errorf("the lane was handed back for starts that never began: %q", l)
		}
	}
	fail = false
	driveTick(s, clk)
	if calls != max+3 {
		t.Fatalf("the lane still starts once it can: %d calls", calls)
	}
	if n, _ := s.Store.LaneStarts(ctx, lane.ID, clk.Now().Format("2006-01-02")); n != 1 {
		t.Errorf("the start that happened counts once: %d", n)
	}
}
