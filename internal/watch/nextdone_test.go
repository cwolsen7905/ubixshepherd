package watch

import (
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
	f.f.mr = &forge.MR{IID: 9, State: "merged", URL: "https://gl/mr/9", MergeSHA: "abc123456789"}

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
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc"}
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
	f.f.mr = &forge.MR{IID: 9, State: "merged", MergeSHA: "abc"}
	f.w.Check(f.ctx)
	items, _ := f.st.NextItems(f.ctx)
	if items[0].State != store.NextDone {
		t.Errorf("state = %s after the hour, want done", items[0].State)
	}
}
