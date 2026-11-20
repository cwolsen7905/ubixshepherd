package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// mergedThenDirty sees the request open, then merged while the worktree holds
// uncommitted work, so the close on the merge poll is refused. It returns the dirt's path.
func (f *fixture) mergedThenDirty(t *testing.T, merged forge.MR) string {
	t.Helper()
	head := f.head(t)
	f.f.mr = &forge.MR{IID: 21, State: "opened", SHA: head, Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	dirt := filepath.Join(f.lane.Worktree, "wip.txt")
	os.WriteFile(dirt, []byte("mine"), 0o644)
	merged.IID, merged.SHA = 21, head
	merged.State = "merged"
	merged.Created = time.Now().Add(time.Minute)
	f.f.mr = &merged
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneOpen {
		t.Fatal("closed a dirty lane")
	}
	return dirt
}

// A merge the poll could not act on (merge commit or squash) is retried from the forge's
// word once the worktree is clean, with no force, and not before a settle interval.
func TestForgeMergedLaneClosesOnceTheWorktreeIsClean(t *testing.T) {
	for name, mr := range map[string]forge.MR{
		"merge commit": {MergeSHA: "aaaa1111bbbb"},
		"squash":       {MergeSHA: "5ca5ca5ca5ca"}, // the forge reports the squash commit as MergeSHA
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			clock := time.Now()
			f.w.Clock = func() time.Time { return clock }
			dirt := f.mergedThenDirty(t, mr)
			os.Remove(dirt)

			f.w.Check(f.ctx) // starts the clock
			if f.state(t) != store.LaneOpen {
				t.Fatal("retried at once")
			}
			clock = clock.Add(AutoCloseSettle + time.Second)
			f.w.Check(f.ctx)
			if f.state(t) != store.LaneClosed {
				t.Fatalf("lane not closed; feed = %s", f.feed(t))
			}
			if lf, _ := f.st.LaneForge(f.ctx, f.lane.ID); lf.MergeSHA != mr.MergeSHA {
				t.Errorf("lane forge = %+v", lf)
			}
		})
	}
}

func TestForgeMergedRetryStillRefusesADirtyWorktree(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	f.w.Clock = func() time.Time { return clock }
	f.mergedThenDirty(t, forge.MR{MergeSHA: "aaaa1111bbbb"})
	f.w.Check(f.ctx)
	clock = clock.Add(AutoCloseSettle + time.Second)
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneOpen {
		t.Fatal("closed a lane with uncommitted work")
	}
	if _, err := os.Stat(f.lane.Worktree); err != nil {
		t.Fatal("worktree removed")
	}
}

// A request merged before the lane existed proves nothing, retry or not.
func TestForgeMergedRetryIgnoresARequestOlderThanTheLane(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	f.w.Clock = func() time.Time { return clock }
	dirt := f.mergedThenDirty(t, forge.MR{MergeSHA: "aaaa1111bbbb"})
	os.Remove(dirt)
	f.f.mr.Created = time.Now().Add(-48 * time.Hour)
	f.w.Check(f.ctx)
	clock = clock.Add(AutoCloseSettle + time.Second)
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneOpen {
		t.Fatal("closed on a request older than the lane")
	}
}

// A lane the poll left open because its request was already merged on first sight stays
// open: the retry acts only on a merge seen happening.
func TestForgeMergedRetryLeavesAFirstSightMergeAlone(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	f.w.Clock = func() time.Time { return clock }
	head := f.head(t)
	f.f.mr = &forge.MR{IID: 31, State: "merged", SHA: head, MergeSHA: "aaaa1111bbbb", Created: time.Now().Add(time.Minute)}
	f.w.Check(f.ctx)
	clock = clock.Add(AutoCloseSettle + time.Second)
	f.w.Check(f.ctx)
	clock = clock.Add(AutoCloseSettle + time.Second)
	f.w.Check(f.ctx)
	if f.state(t) != store.LaneOpen {
		t.Fatalf("closed a lane left open on first sight; feed = %s", f.feed(t))
	}
}

// A refusal that lasts posts nothing more than the merge poll's own message.
func TestForgeMergedRetryDoesNotRepeatARefusal(t *testing.T) {
	f := newFixture(t)
	clock := time.Now()
	f.w.Clock = func() time.Time { return clock }
	f.mergedThenDirty(t, forge.MR{MergeSHA: "aaaa1111bbbb"})
	before := strings.Count(f.feed(t), "cannot close")
	for i := 0; i < 3; i++ {
		clock = clock.Add(AutoCloseSettle + time.Second)
		f.w.Check(f.ctx)
	}
	if after := strings.Count(f.feed(t), "cannot close"); after != before || before != 1 {
		t.Errorf("refusal posted %d times, then %d; feed = %s", before, after, f.feed(t))
	}
}
