package dispatch

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestStillNeededNamesTheItemAndTheRule(t *testing.T) {
	if StillNeeded(nil, "dev") != "" {
		t.Error("a lane with no item gets no check")
	}
	got := StillNeeded(&store.NextItem{ID: 4, Title: "Add the thing", Plan: "docs/thing.md"}, "dev")
	for _, want := range []string{"next-up item 4", "Add the thing", "docs/thing.md", "already landed on dev", "make no commits", AlreadyDone, "you do not"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRunForAnItemLaneGetsTheCheckInItsBrief(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	it, _ := f.st.AddNextItem(ctx, store.NextItem{Title: "Add the thing", Repo: "app", State: store.NextApproved})
	id := f.lane.ID
	f.st.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id})

	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "build it"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.wait(t, run.ID)
	if in := stdinOf(t, run); !strings.Contains(in, "already landed on") || !strings.Contains(in, "Add the thing") {
		t.Errorf("brief lacks the still-needed check: %s", in)
	}

	// A lane with no item is briefed as before.
	g := newFixture(t, "quick")
	run2, _ := g.runner.Start(ctx, StartRequest{LaneID: g.lane.ID, Agent: "claude", Prompt: "build it"})
	run2 = g.wait(t, run2.ID)
	if in := stdinOf(t, run2); strings.Contains(in, "already landed on") {
		t.Errorf("a lane with no item got the check: %s", in)
	}
}

func TestAlreadyDoneReportOffersMarkDoneAndChangesNothing(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	it, _ := f.st.AddNextItem(ctx, store.NextItem{Title: "Add the thing", Repo: "app", State: store.NextApproved})
	id := f.lane.ID
	f.st.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id})
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "w"})
	f.wait(t, run.ID)

	f.runner.Record(ctx, store.Event{RunID: run.ID, Kind: EventReport, Status: "done", Text: AlreadyDone + " landed in abc1234, see src/thing.go"})
	feed, _ := f.st.Feed(ctx, 0, 100)
	var line string
	for _, fi := range feed {
		if fi.Kind == store.FeedPlan {
			line = fi.Text
		}
	}
	if !strings.Contains(line, "may already be done") || !strings.Contains(line, "abc1234") || !strings.Contains(line, "Mark it done?") {
		t.Errorf("feed line = %q", line)
	}
	if got, _ := f.st.NextItem(ctx, it.ID); got.State != store.NextApproved {
		t.Errorf("state = %s: Shepherd must not mark it", got.State)
	}

	// An ordinary done report offers nothing.
	before := len(feed)
	f.runner.Record(ctx, store.Event{RunID: run.ID, Kind: EventReport, Status: "done", Text: "added tests"})
	after, _ := f.st.Feed(ctx, 0, 100)
	for _, fi := range after[before:] {
		if fi.Kind == store.FeedPlan {
			t.Errorf("unexpected plan line %q", fi.Text)
		}
	}
}

func TestAlreadyDoneReportIsIgnoredWhenTheLaneHasCommits(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	it, _ := f.st.AddNextItem(ctx, store.NextItem{Title: "Add the thing", Repo: "app", State: store.NextApproved})
	id := f.lane.ID
	f.st.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id})
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "w"})
	f.wait(t, run.ID)
	for _, args := range [][]string{{"-C", f.lane.Worktree, "-c", "user.name=t", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "work"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	f.runner.Record(ctx, store.Event{RunID: run.ID, Kind: EventReport, Status: "done", Text: AlreadyDone + " abc1234"})
	feed, _ := f.st.Feed(ctx, 0, 100)
	for _, fi := range feed {
		if fi.Kind == store.FeedPlan {
			t.Errorf("offered mark-done despite commits: %q", fi.Text)
		}
	}
}

// landOnBase pushes a commit with the message to origin's main, as a merged branch
// would appear, and refreshes the lane repo's remote-tracking ref.
func landOnBase(t *testing.T, f *fixture, msg string) {
	t.Helper()
	ctx := context.Background()
	repo, _ := f.st.Repo(ctx, f.lane.RepoID)
	for _, args := range [][]string{
		{"-C", repo.Path, "commit", "-q", "--allow-empty", "-m", msg},
		{"-C", repo.Path, "push", "-q", "origin", "HEAD:main"},
		{"-C", repo.Path, "fetch", "-q", "origin"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func itemFor(t *testing.T, f *fixture, mr string) store.NextItem {
	t.Helper()
	ctx := context.Background()
	it, _ := f.st.AddNextItem(ctx, store.NextItem{Title: "Add the thing", Repo: "app", State: store.NextApproved, MR: mr})
	id := f.lane.ID
	f.st.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &id})
	return it
}

func TestRunIsNotStartedWhenTheBaseNamesTheLaneBranch(t *testing.T) {
	f := newFixture(t, "quick")
	ctx := context.Background()
	it := itemFor(t, f, "")
	landOnBase(t, f, "Merge branch '"+f.lane.Branch+"' into 'main'")

	_, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "build it"})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "already done") || !strings.Contains(err.Error(), "force") {
		t.Fatalf("want a refusal naming the reason and force, got %v", err)
	}
	if runs := mustRuns(f.st); len(runs) != 0 {
		t.Errorf("a run was recorded: %v", runs)
	}
	feed, _ := f.st.Feed(ctx, 0, 100)
	found := false
	for _, e := range feed {
		if strings.Contains(e.Text, "run not started") && strings.Contains(e.Text, "item") {
			found = true
		}
	}
	if !found {
		t.Errorf("no feed line for item %d: %v", it.ID, feed)
	}

	run, err := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "build it", Force: true})
	if err != nil {
		t.Fatalf("force should start: %v", err)
	}
	f.wait(t, run.ID)
}

func TestRunIsNotStartedWhenTheBaseNamesTheItemsMR(t *testing.T) {
	f := newFixture(t, "quick")
	itemFor(t, f, "https://forge.example/g/p/-/merge_requests/42")
	landOnBase(t, f, "feat: the thing\n\nSee merge request g/p!42")

	// Another project's !42 does not count.
	g := newFixture(t, "quick")
	itemFor(t, g, "https://forge.example/g/p/-/merge_requests/42")
	landOnBase(t, g, "feat: other\n\nSee merge request g/other!42")
	if run, err := g.runner.Start(context.Background(), StartRequest{LaneID: g.lane.ID, Agent: "claude", Prompt: "w"}); err != nil {
		t.Fatalf("another project's MR number refused the run: %v", err)
	} else {
		g.wait(t, run.ID)
	}
	if _, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "w"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("want a refusal, got %v", err)
	}
}

func TestAlreadyDoneCheckLeavesOtherRunsAlone(t *testing.T) {
	// An unrelated commit on the base, and an MR number that only prefixes another.
	f := newFixture(t, "quick")
	itemFor(t, f, "4")
	landOnBase(t, f, "fix: other work, see !42")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "w"})
	if err != nil {
		t.Fatalf("nothing on the base names the work: %v", err)
	}
	f.wait(t, run.ID)

	// A lane with no item is never checked, even when the base names its branch.
	g := newFixture(t, "quick")
	landOnBase(t, g, "Merge branch '"+g.lane.Branch+"' into 'main'")
	run2, err := g.runner.Start(context.Background(), StartRequest{LaneID: g.lane.ID, Agent: "claude", Prompt: "w"})
	if err != nil {
		t.Fatalf("a lane with no item was checked: %v", err)
	}
	g.wait(t, run2.ID)
}

func TestMRRef(t *testing.T) {
	for in, want := range map[string][2]string{
		"12":  {"", "12"},
		"!12": {"", "12"},
		"https://h.example/g/p/-/merge_requests/12/": {"g/p", "12"},
		"":    {"", ""},
		"abc": {"", ""},
	} {
		if p, n := mrRef(in); p != want[0] || n != want[1] {
			t.Errorf("mrRef(%q) = %q, %q, want %q", in, p, n, want)
		}
	}
}
