package muster

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

type fixture struct {
	t    *testing.T
	ctx  context.Context
	st   *sqlite.DB
	p    *Pass
	now  time.Time
	ws   store.Workspace
	repo store.Repo
	lane store.Lane
	// files, branches and pids are what the fake machine has.
	files    map[string]bool
	branches map[string]bool
	alive    map[int]bool
	cfg      config.Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "app", Path: "/w/app"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(ctx, ws.ID)
	f := &fixture{t: t, ctx: ctx, st: st, now: time.Now().UTC(), ws: ws, repo: repos[0],
		files: map[string]bool{}, branches: map[string]bool{"feat/x": true}, alive: map[int]bool{}, cfg: config.Default()}
	f.lane, err = st.CreateLane(ctx, store.Lane{RepoID: f.repo.ID, Name: "feat/x", Branch: "feat/x", Worktree: "/wt/x", State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	f.files["/wt/x"] = true
	f.p = &Pass{
		Store: st, Conf: func() config.Config { return f.cfg }, Clock: func() time.Time { return f.now },
		Probe: Probe{
			Exists:   func(p string) bool { return f.files[filepath.ToSlash(p)] },
			Branch:   func(_ context.Context, _, b string) bool { return f.branches[b] },
			Alive:    func(pid int) bool { return f.alive[pid] },
			InBranch: func(context.Context, string, string, string) bool { return false },
			Snapshot: func(int64, string) bool { return false },
		},
	}
	return f
}

func (f *fixture) items(state string) []store.MusterItem {
	f.t.Helper()
	got, err := f.st.MusterItems(f.ctx, store.MusterFilter{State: state})
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) kind(state, kind string) *store.MusterItem {
	for _, it := range f.items(state) {
		if it.Kind == kind {
			return &it
		}
	}
	return nil
}

func (f *fixture) feed() string {
	items, _ := f.st.Feed(f.ctx, 0, 100)
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Kind + ": " + it.Text + "\n")
	}
	return b.String()
}

func (f *fixture) artifact(path, intent string) store.Artifact {
	f.t.Helper()
	a, _, err := f.st.UpsertArtifact(f.ctx, store.Artifact{WorkspaceID: f.ws.ID, RepoID: f.repo.ID, LaneID: f.lane.ID, Path: path,
		Kind: store.ArtifactMarkdown, Source: store.ArtifactDiscovered, Intent: intent, IntentBy: store.IntentByDefault,
		State: store.ArtifactLive, SHA256: "h-" + path})
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func TestMissingArtifactIsMarkedGoneAndUndone(t *testing.T) {
	f := newFixture(t)
	here, gone := f.artifact("here.md", store.ArtifactKeep), f.artifact("gone.md", store.ArtifactKeep)
	f.files["/wt/x/here.md"] = true

	if n := f.p.Run(f.ctx); n != 1 {
		t.Fatalf("raised %d, want 1", n)
	}
	if a, _ := f.st.Artifact(f.ctx, gone.ID); a.State != store.ArtifactGone {
		t.Errorf("gone.md = %s", a.State)
	}
	if a, _ := f.st.Artifact(f.ctx, here.ID); a.State != store.ArtifactLive {
		t.Errorf("here.md = %s", a.State)
	}
	it := f.kind(store.MusterApplied, KindArtifactMissing)
	if it == nil || !it.Auto || it.UndoUntil == nil || !strings.Contains(it.Reason, "gone.md") {
		t.Fatalf("log item = %+v", it)
	}
	if !strings.Contains(f.feed(), "muster: tidied: gone.md") {
		t.Errorf("feed = %s", f.feed())
	}

	if _, err := f.p.Undo(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.st.Artifact(f.ctx, gone.ID); a.State != store.ArtifactLive {
		t.Errorf("after undo gone.md = %s", a.State)
	}
	// The person's undo is respected: the same finding is not made again.
	if n := f.p.Run(f.ctx); n != 0 {
		t.Errorf("second pass raised %d after an undo", n)
	}
	if a, _ := f.st.Artifact(f.ctx, gone.ID); a.State != store.ArtifactLive {
		t.Errorf("gone.md tidied again: %s", a.State)
	}
}

func TestWholeWorktreeMissingDoesNotMarkArtifactsGone(t *testing.T) {
	f := newFixture(t)
	a := f.artifact("a.md", store.ArtifactKeep)
	delete(f.files, "/wt/x") // an unmounted volume: nothing under it can be seen
	f.p.Run(f.ctx)
	if got, _ := f.st.Artifact(f.ctx, a.ID); got.State != store.ArtifactLive {
		t.Errorf("artifact = %s", got.State)
	}
	if f.kind(store.MusterOpen, KindLaneGone) == nil {
		t.Error("the lane is not flagged")
	}
}

func TestCommittedOnTheBaseCountsAfterTheBranchIsDeleted(t *testing.T) {
	f := newFixture(t)
	l, _ := f.st.CreateLane(f.ctx, store.Lane{RepoID: f.repo.ID, Name: "feat/z", Branch: "feat/z", Base: "dev", Worktree: "/wt/z", State: store.LaneClosed})
	a2, _, _ := f.st.UpsertArtifact(f.ctx, store.Artifact{WorkspaceID: f.ws.ID, RepoID: f.repo.ID, LaneID: l.ID, Path: "b.md",
		Kind: store.ArtifactMarkdown, Source: store.ArtifactDiscovered, Intent: store.ArtifactKeep, IntentBy: store.IntentByAgent, State: store.ArtifactLive})
	f.p.Probe.InBranch = func(_ context.Context, _, branch, _ string) bool { return branch == "dev" }
	f.p.Run(f.ctx)
	if got, _ := f.st.Artifact(f.ctx, a2.ID); got.State != store.ArtifactInRepo {
		t.Errorf("artifact = %s, want committed", got.State)
	}
}

func TestFailedCheckDoesNotClearOpenFindings(t *testing.T) {
	f := newFixture(t)
	f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "Legacy?"})
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindQuestionNoAbout) == nil {
		t.Fatal("not flagged")
	}
	// The inbox check fails part way: its findings must stay open.
	f.p.Store = failingDecisions{f.st}
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindQuestionNoAbout) == nil {
		t.Error("an open finding was cleared by a check that failed")
	}
}

type failingDecisions struct{ *sqlite.DB }

func (failingDecisions) Decisions(context.Context, string) ([]store.Decision, error) {
	return nil, errors.New("database is locked")
}

func TestUndoWindowEnds(t *testing.T) {
	f := newFixture(t)
	f.artifact("gone.md", store.ArtifactKeep)
	f.p.Run(f.ctx)
	it := f.kind(store.MusterApplied, KindArtifactMissing)
	f.now = f.now.Add(8 * 24 * time.Hour) // past the 7 day default
	if _, err := f.p.Undo(f.ctx, it.ID); err != ErrNotUndoable {
		t.Errorf("undo after the window = %v", err)
	}
}

func TestClosedLaneArtifactGetsItsIntentApplied(t *testing.T) {
	f := newFixture(t)
	scratch, keep, undecided := f.artifact("s.md", store.ArtifactScratch), f.artifact("k.md", store.ArtifactKeep), f.artifact("u.md", store.ArtifactUndecided)
	f.p.Probe.Snapshot = func(id int64, _ string) bool { return id == keep.ID }
	f.st.SetLaneState(f.ctx, f.lane.ID, store.LaneClosed)
	f.p.Run(f.ctx)
	for _, c := range []struct {
		a    store.Artifact
		want string
	}{{scratch, store.ArtifactGone}, {keep, store.ArtifactKept}, {undecided, store.ArtifactLive}} {
		if got, _ := f.st.Artifact(f.ctx, c.a.ID); got.State != c.want {
			t.Errorf("%s = %s, want %s", c.a.Path, got.State, c.want)
		}
	}
}

func TestUndecidedArtifactsJoinOneBatch(t *testing.T) {
	f := newFixture(t)
	a, b := f.artifact("a.md", store.ArtifactUndecided), f.artifact("b.md", store.ArtifactUndecided)
	f.files["/wt/x/a.md"], f.files["/wt/x/b.md"] = true, true
	zero := 0
	f.cfg.Muster.StaleDays = &zero
	f.now = f.now.Add(2 * 24 * time.Hour)
	if n := f.p.Run(f.ctx); n != 0 {
		t.Fatalf("raised %d before the batch days", n)
	}
	f.now = f.now.Add(2 * 24 * time.Hour)
	if n := f.p.Run(f.ctx); n != 1 {
		t.Fatalf("raised %d, want one batch", n)
	}
	it := f.kind(store.MusterOpen, KindArtifactUndecided)
	if it == nil || it.Fix != FixArtifactKeepAll || !strings.Contains(it.Reason, "2 artifacts") {
		t.Fatalf("batch = %+v", it)
	}
	if _, err := f.p.Apply(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		if got, _ := f.st.Artifact(f.ctx, id); got.Intent != store.ArtifactKeep || got.IntentBy != store.IntentByPerson {
			t.Errorf("artifact %d = %s by %s", id, got.Intent, got.IntentBy)
		}
	}
	if _, err := f.p.Undo(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.Artifact(f.ctx, a.ID); got.Intent != store.ArtifactUndecided || got.IntentBy != store.IntentByDefault {
		t.Errorf("after undo = %s by %s", got.Intent, got.IntentBy)
	}
}

func TestCollapseShowsIdenticalContentOnce(t *testing.T) {
	list := []store.Artifact{{ID: 1, LaneID: 1, SHA256: "x"}, {ID: 2, LaneID: 1, SHA256: "x"}, {ID: 3, LaneID: 2, SHA256: "x"}, {ID: 4, LaneID: 1}, {ID: 5, LaneID: 1}}
	got := Collapse(list)
	var ids []int64
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	if len(ids) != 4 || ids[0] != 1 || ids[1] != 3 || ids[2] != 4 || ids[3] != 5 {
		t.Errorf("ids = %v", ids)
	}
}

func TestBoardFlagsGoneStaleAndClosedMR(t *testing.T) {
	f := newFixture(t)
	delete(f.files, "/wt/x")
	delete(f.branches, "feat/x")
	f.now = f.now.Add(4 * 24 * time.Hour)
	f.st.PutLaneForge(f.ctx, store.LaneForge{LaneID: f.lane.ID, MR: 7, MRState: "closed"})
	f.p.Run(f.ctx)
	for _, k := range []string{KindLaneGone, KindLaneStale, KindLaneMRClosed} {
		if f.kind(store.MusterOpen, k) == nil {
			t.Errorf("no %s finding; have %+v", k, f.items(""))
		}
	}
	if g := f.kind(store.MusterOpen, KindLaneGone); g != nil && !strings.Contains(g.Reason, "worktree /wt/x and branch feat/x") {
		t.Errorf("reason = %s", g.Reason)
	}
	// The lane comes back: the finding clears itself.
	f.files["/wt/x"], f.branches["feat/x"] = true, true
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindLaneGone) != nil || f.kind(store.MusterCleared, KindLaneGone) == nil {
		t.Errorf("lane.gone did not clear: %+v", f.items(""))
	}
}

func TestStaleDaysCanBeTurnedOff(t *testing.T) {
	f := newFixture(t)
	zero := 0
	f.cfg.Muster.StaleDays = &zero
	f.now = f.now.Add(60 * 24 * time.Hour)
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindLaneStale) != nil {
		t.Error("stale finding with the check off")
	}
	f.cfg.Muster.Off = true
	f.cfg.Muster.StaleDays = nil
	if n := f.p.Run(f.ctx); n != 0 {
		t.Errorf("raised %d with muster off", n)
	}
}

func TestDeadRunBecomesInterruptedOnSecondSighting(t *testing.T) {
	f := newFixture(t)
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunRunning})
	run.PID = 4242
	f.st.UpdateRun(f.ctx, run)
	f.now = f.now.Add(time.Hour)
	f.p.Run(f.ctx)
	if got, _ := f.st.Run(f.ctx, run.ID); got.State != store.RunRunning {
		t.Fatalf("interrupted on first sighting: %s", got.State)
	}
	f.p.Run(f.ctx)
	got, _ := f.st.Run(f.ctx, run.ID)
	if got.State != store.RunInterrupted || got.Ended == nil {
		t.Fatalf("run = %s", got.State)
	}
	it := f.kind(store.MusterApplied, KindRunDead)
	if it == nil || it.UndoUntil != nil {
		t.Errorf("item = %+v: marking a dead run interrupted cannot be undone", it)
	}
	if _, err := f.p.Undo(f.ctx, it.ID); err != ErrNotUndoable {
		t.Errorf("undo = %v", err)
	}
}

func TestRunWithNoPidIsAskedAboutAfterTheGrace(t *testing.T) {
	f := newFixture(t)
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunRunning})
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindRunNoPID) != nil {
		t.Fatal("asked inside the grace")
	}
	f.now = f.now.Add(time.Hour)
	f.p.Run(f.ctx)
	it := f.kind(store.MusterOpen, KindRunNoPID)
	if it == nil || it.Auto {
		t.Fatalf("finding = %+v", it)
	}
	if got, _ := f.st.Run(f.ctx, run.ID); got.State != store.RunRunning {
		t.Fatalf("interrupted unasked: %s", got.State)
	}
	if _, err := f.p.Apply(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.Run(f.ctx, run.ID); got.State != store.RunInterrupted {
		t.Errorf("run = %s", got.State)
	}
}

func TestKeepAllReturnsStoreErrors(t *testing.T) {
	f := newFixture(t)
	a := f.artifact("a.md", store.ArtifactUndecided)
	it, _, _ := f.st.AddMuster(f.ctx, store.MusterItem{Key: "k", Kind: KindArtifactUndecided, SubjectKind: "artifact", SubjectID: a.ID,
		Fix: FixArtifactKeepAll, FixArg: fmt.Sprintf(`{"IDs":[%d,999]}`, a.ID)})
	if _, err := f.p.Apply(f.ctx, it.ID); err != nil {
		t.Fatalf("a vanished artifact must be skipped: %v", err)
	}
	f.p.Store = brokenArtifact{f.st}
	it2, _, _ := f.st.AddMuster(f.ctx, store.MusterItem{Key: "k2", Kind: KindArtifactUndecided, SubjectKind: "artifact", SubjectID: a.ID,
		Fix: FixArtifactKeepAll, FixArg: fmt.Sprintf(`{"IDs":[%d]}`, a.ID)})
	if _, err := f.p.Apply(f.ctx, it2.ID); err == nil {
		t.Error("a store failure was reported as success")
	}
}

type brokenArtifact struct{ *sqlite.DB }

func (brokenArtifact) Artifact(context.Context, int64) (store.Artifact, error) {
	return store.Artifact{}, errors.New("database is locked")
}

func TestLiveRunIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunRunning})
	run.PID = 4242
	f.st.UpdateRun(f.ctx, run)
	f.now = f.now.Add(time.Hour)
	f.alive[4242] = true
	f.p.Run(f.ctx)
	f.p.Run(f.ctx)
	if got, _ := f.st.Run(f.ctx, run.ID); got.State != store.RunRunning {
		t.Errorf("a live run = %s", got.State)
	}
}

func TestApprovedScopeChangeIsOfferedAndUndone(t *testing.T) {
	f := newFixture(t)
	f.st.SetLaneScope(f.ctx, f.lane.ID, []string{"internal/muster/**"})
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunSucceeded})
	d, _ := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "May I widen scope to internal/store/muster.go?"})
	f.st.AnswerDecision(f.ctx, d.ID, "yes")
	f.p.Run(f.ctx)
	it := f.kind(store.MusterOpen, KindLaneScope)
	if it == nil {
		t.Fatalf("no scope finding: %+v", f.items(""))
	}
	if _, err := f.p.Apply(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := f.st.Lane(f.ctx, f.lane.ID); len(l.Scope) != 2 || l.Scope[1] != "internal/store/muster.go" {
		t.Errorf("scope = %v", l.Scope)
	}
	if _, err := f.p.Undo(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := f.st.Lane(f.ctx, f.lane.ID); len(l.Scope) != 1 {
		t.Errorf("scope after undo = %v", l.Scope)
	}
}

func TestNextUpChecks(t *testing.T) {
	f := newFixture(t)
	add := func(title, repo string) store.NextItem {
		it, err := f.st.AddNextItem(f.ctx, store.NextItem{Title: title, Repo: repo, State: store.NextApproved})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	norepo := add("Write the thing", "")
	first := add("Add the muster upkeep pass", "app")
	dup := add("Add muster upkeep pass", "app")
	other := add("Add muster upkeep pass", "elsewhere")
	closed := add("Closed lane work", "app")
	cl, _ := f.st.CreateLane(f.ctx, store.Lane{RepoID: f.repo.ID, Name: "feat/c", Branch: "feat/c", State: store.LaneClosed})
	lid := cl.ID
	f.st.PutLaneForge(f.ctx, store.LaneForge{LaneID: cl.ID, MR: 4, MRState: "closed"})
	f.st.UpdateNextItem(f.ctx, closed.ID, store.NextPatch{LaneID: &lid})
	f.p.Probe.Implemented = func(_ context.Context, it store.NextItem) (bool, string) { return it.ID == first.ID, "requirement 1" }

	f.p.Run(f.ctx)
	for _, k := range []string{KindNextNoRepo, KindNextLaneClosed, KindNextImplemented, KindNextDuplicate} {
		if f.kind(store.MusterOpen, k) == nil {
			t.Errorf("no %s finding", k)
		}
	}
	d := f.kind(store.MusterOpen, KindNextDuplicate)
	if d == nil || d.SubjectID != dup.ID {
		t.Errorf("duplicate = %+v, want item %d", d, dup.ID)
	}
	_ = norepo
	_ = other

	// Dropping the duplicate is one click and can be undone.
	if _, err := f.p.Apply(f.ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.NextItem(f.ctx, dup.ID); got.State != store.NextDropped {
		t.Errorf("dup = %s", got.State)
	}
	f.p.Undo(f.ctx, d.ID)
	if got, _ := f.st.NextItem(f.ctx, dup.ID); got.State != store.NextApproved {
		t.Errorf("dup after undo = %s", got.State)
	}

	// Back to approved clears the dead lane link; marking done sets and undo clears done.
	re := f.kind(store.MusterOpen, KindNextLaneClosed)
	f.p.Apply(f.ctx, re.ID)
	if got, _ := f.st.NextItem(f.ctx, closed.ID); got.LaneID != 0 || got.State != store.NextApproved {
		t.Errorf("closed item = %+v", got)
	}
	im := f.kind(store.MusterOpen, KindNextImplemented)
	f.p.Apply(f.ctx, im.ID)
	if got, _ := f.st.NextItem(f.ctx, first.ID); got.State != store.NextDone {
		t.Errorf("first = %s", got.State)
	}
	f.p.Undo(f.ctx, im.ID)
	if got, _ := f.st.NextItem(f.ctx, first.ID); got.State != store.NextApproved || got.DoneAt != nil {
		t.Errorf("first after undo = %+v", got)
	}
}

func TestSimilarity(t *testing.T) {
	for _, c := range []struct {
		a, b string
		dup  bool
	}{
		{"Add the muster upkeep pass", "Add muster upkeep pass", true},
		{"Fix login", "Fix logout", false},
		{"Fix login bug", "Fix logout bug", false},
		{"Docs", "Docs", false},
	} {
		if got := similarity(titleWords(c.a), titleWords(c.b)) >= DuplicateThreshold; got != c.dup {
			t.Errorf("%q vs %q = %v", c.a, c.b, got)
		}
	}
}

func TestInboxClosesOutOfDateAndReopens(t *testing.T) {
	f := newFixture(t)
	other, _ := f.st.CreateLane(f.ctx, store.Lane{RepoID: f.repo.ID, Name: "feat/y", Branch: "feat/y", State: store.LaneClosed})
	dead, _ := f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "About a closed lane?", Repo: "app", LaneID: other.ID})
	live, _ := f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "About an open lane?", Repo: "app", LaneID: f.lane.ID})
	it, _ := f.st.AddNextItem(f.ctx, store.NextItem{Title: "finished thing", Repo: "app", State: store.NextDone})
	done, _ := f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "About a done item?", Repo: "app", NextID: it.ID})
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: other.ID, Agent: "claude", State: store.RunSucceeded})
	dec, _ := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "Which?"})

	f.p.Run(f.ctx)
	for _, id := range []int64{dead.ID, done.ID} {
		if q, _ := f.st.Question(f.ctx, id); q.State != store.QuestionOutdated || q.Closed == nil {
			t.Errorf("question %d = %s", id, q.State)
		}
	}
	if q, _ := f.st.Question(f.ctx, live.ID); q.State != store.QuestionOpen {
		t.Errorf("live question = %s", q.State)
	}
	if d, _ := f.st.Decision(f.ctx, dec.ID); d.State != store.DecisionOutdated {
		t.Errorf("decision = %s", d.State)
	}
	log := f.kind(store.MusterApplied, KindQuestionOutdated)
	if log == nil || !strings.Contains(log.Reason, "is closed") && !strings.Contains(log.Reason, "is done") {
		t.Fatalf("reason = %+v", log)
	}
	if _, err := f.p.Undo(f.ctx, log.ID); err != nil {
		t.Fatal(err)
	}
	if q, _ := f.st.Question(f.ctx, log.SubjectID); q.State != store.QuestionOpen || q.Closed != nil {
		t.Errorf("reopened question = %+v", q)
	}
}

func TestOldQuestionsGoToDigestThenSkipped(t *testing.T) {
	f := newFixture(t)
	q, _ := f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "Still wanted?", Repo: "app", LaneID: f.lane.ID})
	f.now = time.Now().UTC().Add(2 * 24 * time.Hour)
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindInboxDigest) != nil {
		t.Fatal("digest on day 2")
	}
	f.now = time.Now().UTC().Add(4 * 24 * time.Hour)
	f.p.Run(f.ctx)
	d := f.kind(store.MusterOpen, KindInboxDigest)
	if d == nil || !strings.Contains(d.Reason, "question") {
		t.Fatalf("digest = %+v", d)
	}
	if got, _ := f.st.Question(f.ctx, q.ID); got.State != store.QuestionOpen {
		t.Fatalf("skipped on day 4: %s", got.State)
	}
	f.now = time.Now().UTC().Add(11 * 24 * time.Hour) // 3 + 7 days
	f.p.Run(f.ctx)
	if got, _ := f.st.Question(f.ctx, q.ID); got.State != store.QuestionSkipped {
		t.Errorf("question = %s, want skipped", got.State)
	}
}

func TestQuestionWithoutALinkIsFlagged(t *testing.T) {
	f := newFixture(t)
	f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "Legacy?"})
	f.p.Run(f.ctx)
	if it := f.kind(store.MusterOpen, KindQuestionNoAbout); it == nil {
		t.Fatalf("not flagged: %+v", f.items(""))
	}
}

func TestDismissAndApplyNeedAnOpenFinding(t *testing.T) {
	f := newFixture(t)
	f.st.AddQuestion(f.ctx, store.Question{WorkspaceID: f.ws.ID, Text: "Legacy?"})
	f.p.Run(f.ctx)
	it := f.kind(store.MusterOpen, KindQuestionNoAbout)
	if _, err := f.p.Dismiss(f.ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Dismiss(f.ctx, it.ID); err != ErrNotOpen {
		t.Errorf("second dismiss = %v", err)
	}
	if _, err := f.p.Apply(f.ctx, it.ID); err != ErrNotOpen {
		t.Errorf("apply after dismiss = %v", err)
	}
	f.p.Run(f.ctx)
	if f.kind(store.MusterOpen, KindQuestionNoAbout) != nil {
		t.Error("a dismissed finding came back")
	}
}

func TestAboutLinks(t *testing.T) {
	f := newFixture(t)
	if err := ValidateQuestion(store.Question{Text: "x"}); err != store.ErrNoAbout {
		t.Errorf("unlinked question = %v", err)
	}
	if err := ValidateQuestion(store.Question{Repo: "app", NextID: 3}); err != nil {
		t.Errorf("linked question = %v", err)
	}
	if err := ValidateNext(store.NextItem{Title: "x"}); err != ErrNoRepo {
		t.Errorf("repo-less item = %v", err)
	}
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunSucceeded})
	d, _ := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "?"})
	a, err := AboutDecision(f.ctx, f.st, d)
	if err != nil || a.Label() != "app / feat/x" {
		t.Errorf("decision about = %+v, %v", a, err)
	}
	q := store.Question{Repo: "app", LaneID: f.lane.ID}
	if got := AboutQuestion(f.ctx, f.st, q).Label(); got != "app / feat/x" {
		t.Errorf("question label = %q", got)
	}
	if got := (About{Repo: "app", NextID: 12}).Label(); got != "app / next-up 12" {
		t.Errorf("next label = %q", got)
	}
}

func TestDecisionWithUnreadableRunStaysInTheDigest(t *testing.T) {
	f := newFixture(t)
	run, _ := f.st.CreateRun(f.ctx, store.Run{LaneID: f.lane.ID, Agent: "claude", State: store.RunSucceeded})
	d, _ := f.st.CreateDecision(f.ctx, store.Decision{RunID: run.ID, Question: "Which one?"})
	f.p.Store = unreadableRun{f.st}
	f.now = time.Now().UTC().Add(4 * 24 * time.Hour)
	f.p.Run(f.ctx)
	dg := f.kind(store.MusterOpen, KindInboxDigest)
	if dg == nil || !strings.Contains(dg.Reason, fmt.Sprintf("decision %d", d.ID)) {
		t.Fatalf("digest = %+v", dg)
	}
	if got, _ := f.st.Decision(f.ctx, d.ID); got.State != store.DecisionOpen {
		t.Errorf("decision = %s", got.State)
	}
}

type unreadableRun struct{ *sqlite.DB }

func (unreadableRun) Run(context.Context, int64) (store.Run, error) {
	return store.Run{}, errors.New("row unreadable")
}
