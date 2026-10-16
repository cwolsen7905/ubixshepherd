package dispatch

import (
	"context"
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
