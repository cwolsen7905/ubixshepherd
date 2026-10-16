package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestUnapprovedProposalsExpireToDeferred(t *testing.T) {
	f := newFixture(t)
	add := func(title, source string) store.NextItem {
		it, err := f.st.AddNextItem(f.ctx, store.NextItem{Title: title, Source: source, State: store.NextPlanned, Note: "why"})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	desk, agent, mine, chat := add("desk idea", store.NextByDesk), add("agent idea", store.NextByAgent), add("my note", store.NextByPerson), add("chat work", store.NextByChat)
	approved, _ := f.st.AddNextItem(f.ctx, store.NextItem{Title: "approved", Source: store.NextByDesk, State: store.NextApproved})

	now := time.Now()
	f.w.Clock = func() time.Time { return now }
	f.w.Check(f.ctx)
	for _, it := range []store.NextItem{desk, agent} {
		if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextPlanned {
			t.Fatalf("%s expired on day 0: %s", it.Title, got.State)
		}
	}

	now = now.Add(15 * 24 * time.Hour) // past the default 14 days
	f.w.Check(f.ctx)
	for _, it := range []store.NextItem{desk, agent} {
		got, _ := f.st.NextItem(f.ctx, it.ID)
		if got.State != store.NextDeferred || !strings.Contains(got.Note, "Expired to deferred on") || !strings.HasPrefix(got.Note, "why\n") {
			t.Errorf("%s = %s, note %q", it.Title, got.State, got.Note)
		}
	}
	for _, it := range []store.NextItem{mine, chat, approved} {
		if got, _ := f.st.NextItem(f.ctx, it.ID); got.State == store.NextDeferred {
			t.Errorf("%s expired, but it waits on no one", it.Title)
		}
	}
	feed := f.feed(t)
	if !strings.Contains(feed, "plan: next-up item 1 expired to deferred after 14 days unapproved: desk idea") {
		t.Errorf("feed = %s", feed)
	}
	// A second pass says nothing more.
	f.w.Check(f.ctx)
	if n := strings.Count(f.feed(t), "expired to deferred"); n != 2 {
		t.Errorf("%d expiry lines, want 2", n)
	}
}

func TestExpiryCanBeTurnedOff(t *testing.T) {
	f := newFixture(t)
	cfg := f.run.Conf()
	zero := 0
	cfg.Next.ExpireDays = &zero
	f.run.SetConfig(cfg)
	it, _ := f.st.AddNextItem(f.ctx, store.NextItem{Title: "idea", Source: store.NextByDesk})
	f.w.Clock = func() time.Time { return time.Now().Add(400 * 24 * time.Hour) }
	f.w.Check(f.ctx)
	if got, _ := f.st.NextItem(f.ctx, it.ID); got.State != store.NextPlanned {
		t.Errorf("state = %s with expiry off", got.State)
	}
}
