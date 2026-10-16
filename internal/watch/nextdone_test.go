package watch

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// closeByHand closes the fixture's lane the way a squash-merged lane ends up: closed in
// the store with no merge recorded.
func (f *fixture) closeByHand(t *testing.T) {
	t.Helper()
	if err := f.st.SetLaneState(f.ctx, f.lane.ID, store.LaneClosed); err != nil {
		t.Fatal(err)
	}
}

// repoPath is the fixture repo's path, and head the lane branch's tip.
func (f *fixture) repoPath(t *testing.T) string {
	t.Helper()
	wss, _ := f.st.Workspaces(f.ctx)
	repos, _ := f.st.Repos(f.ctx, wss[0].ID)
	return repos[0].Path
}

func (f *fixture) head(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", f.repoPath(t), "rev-parse", "refs/heads/feat/x").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) linkedItem(t *testing.T, state string) store.NextItem {
	t.Helper()
	it, err := f.st.AddNextItem(f.ctx, store.NextItem{Title: "ship x", Repo: "app", State: state})
	if err != nil {
		t.Fatal(err)
	}
	id := f.lane.ID
	it, err = f.st.UpdateNextItem(f.ctx, it.ID, store.NextPatch{LaneID: &id})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestSquashMergedClosedLaneFinishesItsItem(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	f.f.mr = &forge.MR{IID: 9, State: "merged", URL: "https://gl/mr/9", MergeSHA: "abc123456789", SHA: f.head(t), Created: time.Now().Add(time.Minute)}

	f.w.Check(f.ctx)

	got, _ := f.st.NextItem(f.ctx, it.ID)
	if got.State != store.NextDone || got.DoneAt == nil || got.MR != "!9" {
		t.Fatalf("item = %+v", got)
	}
	lf, _ := f.st.LaneForge(f.ctx, f.lane.ID)
	if lf.MRState != "merged" || lf.MergeSHA != "abc123456789" {
		t.Errorf("lane forge = %+v", lf)
	}
	if feed := f.feed(t); !strings.Contains(feed, "plan: next-up item 1 done: ship x (lane feat/x merged)") {
		t.Errorf("feed = %s", feed)
	}
}

func TestClosedLaneWithoutAMergeKeepsItsItem(t *testing.T) {
	for name, mr := range map[string]*forge.MR{
		"no request":     nil,
		"request closed": {IID: 9, State: "closed"},
		"request open":   {IID: 9, State: "opened"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			it := f.linkedItem(t, store.NextApproved)
			f.closeByHand(t)
			f.f.mr = mr
			f.w.Check(f.ctx)
			if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextApproved {
				t.Errorf("state = %s, want approved", got.State)
			}
		})
	}
}

func TestDroppedAndDoneItemsAreLeftAlone(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextDropped)
	f.closeByHand(t)
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc", SHA: f.head(t), Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextDropped {
		t.Errorf("state = %s, want dropped", got.State)
	}
}

func TestClosedLaneIsAskedAboutOnlyOncePerHour(t *testing.T) {
	f := newFixture(t)
	f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.f.mr = &forge.MR{IID: 9, State: "closed"}
	f.w.Check(f.ctx)
	first := f.f.calls
	f.w.Check(f.ctx)
	if f.f.calls != first {
		t.Errorf("asked again within the hour: %d then %d calls", first, f.f.calls)
	}
	now = now.Add(ClosedRecheck + time.Minute)
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc", SHA: f.head(t), Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	items, _ := f.st.NextItems(f.ctx)
	if items[0].State != store.NextDone {
		t.Errorf("state = %s after the hour, want done", items[0].State)
	}
}

func TestMergedRequestOlderThanTheLaneIsNotItsProof(t *testing.T) {
	for name, created := range map[string]time.Time{"before the lane": time.Now().Add(-48 * time.Hour), "no creation time": {}} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			it := f.linkedItem(t, store.NextApproved)
			f.closeByHand(t)
			f.f.mr = &forge.MR{IID: 3, State: "merged", MergeSHA: "abc", Created: created}
			f.w.Check(f.ctx)
			if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextApproved {
				t.Errorf("state = %s, want approved", got.State)
			}
		})
	}
}

func TestForgeErrorOnAClosedLaneKeepsTheItemAndIsAskedAgainAfterTheHour(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.f.err = errors.New("500 server error")
	f.w.Check(f.ctx)
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextApproved {
		t.Fatalf("state = %s after a forge error, want approved", got.State)
	}
	if st := f.w.Status(); st.Error == "" {
		t.Error("the forge error was not noted in the pass's status")
	}
	first := f.f.calls
	if first == 0 {
		t.Fatal("the forge was never asked")
	}
	f.w.Check(f.ctx)
	if f.f.calls != first {
		t.Errorf("asked again within the hour: %d then %d", first, f.f.calls)
	}
	now = now.Add(ClosedRecheck + time.Minute)
	f.f.err = nil
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc", SHA: f.head(t), Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	if f.f.calls == first {
		t.Error("not asked again after the hour")
	}
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextDone {
		t.Errorf("state = %s after the hour, want done", got.State)
	}
}

func TestWorkPushedAfterTheMergeKeepsTheItemOpen(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	merged := f.head(t)
	// A commit on the lane's branch after the request's head.
	g := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", f.repoPath(t)}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("symbolic-ref", "HEAD", "refs/heads/feat/x")
	g("-c", "user.name=t", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "later")
	g("checkout", "-q", "--detach")
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc", SHA: merged, Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextApproved {
		t.Errorf("state = %s, want approved: the branch holds work the merge does not cover", got.State)
	}
}

func TestClosedLanePollBacksOffAndIsForgottenWhenSettled(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.f.mr = &forge.MR{IID: 9, State: "closed"}
	f.w.Check(f.ctx) // n=1: next ask after 1h
	waits := []time.Duration{}
	for i := 0; i < 12; i++ {
		before := f.f.calls
		// Find the shortest wait that makes the watcher ask again.
		step := time.Hour
		var waited time.Duration
		for f.f.calls == before {
			now = now.Add(step)
			waited += step
			f.w.Check(f.ctx)
			if waited > 30*24*time.Hour {
				t.Fatal("never asked again")
			}
		}
		waits = append(waits, waited)
	}
	if waits[0] != time.Hour || waits[1] != 2*time.Hour || waits[2] != 4*time.Hour {
		t.Errorf("waits start %v, want 1h, 2h, 4h", waits[:3])
	}
	if last := waits[len(waits)-1]; last != ClosedRecheckMax {
		t.Errorf("settles at %v, want %v", last, ClosedRecheckMax)
	}
	if len(f.w.closedRead) != 1 {
		t.Errorf("closedRead = %v", f.w.closedRead)
	}
	// Once the item is no longer open nothing is remembered.
	done := store.NextDropped
	f.st.UpdateNextItem(f.ctx, it.ID, store.NextPatch{State: &done})
	f.w.Check(f.ctx)
	if len(f.w.closedRead) != 0 {
		t.Errorf("closedRead not pruned: %v", f.w.closedRead)
	}
}

func TestForgeErrorsDoNotLengthenTheClosedLaneWait(t *testing.T) {
	f := newFixture(t)
	f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.f.err = errors.New("500 server error")
	for i := 0; i < 6; i++ {
		f.w.Check(f.ctx)
		now = now.Add(ClosedRecheck + time.Minute)
	}
	if p := f.w.closedRead[f.lane.ID]; p.n != 0 {
		t.Errorf("after six failed calls n = %d, want 0", p.n)
	}
	f.f.err = nil
	f.f.mr = &forge.MR{IID: 9, State: "closed"}
	f.w.Check(f.ctx)
	if p := f.w.closedRead[f.lane.ID]; p.n != 1 {
		t.Errorf("after one answer n = %d, want 1", p.n)
	}
}

// A store that cannot list the items must not make the watcher forget what it has
// learned about closed lanes.
type failingItems struct {
	store.Store
	fail bool
}

func (s *failingItems) NextItems(ctx context.Context) ([]store.NextItem, error) {
	if s.fail {
		return nil, errors.New("database is locked")
	}
	return s.Store.NextItems(ctx)
}

func TestStoreReadFailureKeepsTheClosedLanePollState(t *testing.T) {
	f := newFixture(t)
	fs := &failingItems{Store: f.st}
	f.w.Store = fs
	f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	f.f.mr = &forge.MR{IID: 9, State: "closed"}
	f.w.Check(f.ctx)
	if len(f.w.closedRead) != 1 {
		t.Fatalf("closedRead = %v", f.w.closedRead)
	}
	fs.fail = true
	f.w.Check(f.ctx)
	if len(f.w.closedRead) != 1 {
		t.Errorf("a failed read forgot the poll state: %v", f.w.closedRead)
	}
}

func TestLaneWithNoRequestIsAskedAtLeastDaily(t *testing.T) {
	f := newFixture(t)
	f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.f.mr = nil
	for i := 0; i < 8; i++ { // enough answers to pass the cap a request would reach
		f.w.Check(f.ctx)
		now = now.Add(ClosedRecheckNoMR + time.Minute)
	}
	if got := f.w.closedRead[f.lane.ID].wait(); got != ClosedRecheckNoMR {
		t.Errorf("wait with no request = %v, want %v", got, ClosedRecheckNoMR)
	}
	// The request is opened and merged in the meantime: found within the day.
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc", SHA: f.head(t), Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	items, _ := f.st.NextItems(f.ctx)
	if items[0].State != store.NextDone {
		t.Errorf("state = %s, want done", items[0].State)
	}
}

func TestMergedRequestWithoutAMergeCommitIsNotProof(t *testing.T) {
	f := newFixture(t)
	it := f.linkedItem(t, store.NextApproved)
	f.closeByHand(t)
	f.f.mr = &forge.MR{IID: 9, State: "merged", SHA: f.head(t), Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextApproved {
		t.Errorf("state = %s: lane close would refuse this request, so must the watcher", got.State)
	}
}
