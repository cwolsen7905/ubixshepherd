package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestTopicsLinksMergeAndClear(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := db.AddTopic(ctx, store.Topic{Title: "release", Source: store.TopicByDesk})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := db.AddTopic(ctx, store.Topic{Title: "post"})
	if _, err := db.AddTopic(ctx, store.Topic{Title: "  "}); err == nil {
		t.Fatal("a blank title was accepted")
	}

	link := func(topic int64, kind, ref, by string) store.TopicLink {
		l, err := db.LinkTopic(ctx, store.TopicLink{TopicID: topic, Kind: kind, Ref: ref, By: by})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	link(a.ID, store.TopicLane, "5", store.TopicLinkDesk)
	// Shepherd never reassigns; the desk may correct the desk; the person wins.
	if l := link(b.ID, store.TopicLane, "5", store.TopicLinkShepherd); l.TopicID != a.ID {
		t.Fatalf("shepherd reassigned: %+v", l)
	}
	if l := link(b.ID, store.TopicLane, "5", store.TopicLinkDesk); l.TopicID != b.ID {
		t.Fatalf("desk could not correct its own link: %+v", l)
	}
	if l := link(a.ID, store.TopicLane, "5", store.TopicLinkPerson); l.TopicID != a.ID || l.By != store.TopicLinkPerson {
		t.Fatalf("person move: %+v", l)
	}
	if l := link(b.ID, store.TopicLane, "5", store.TopicLinkDesk); l.TopicID != a.ID {
		t.Fatalf("desk overwrote a person's link: %+v", l)
	}
	if _, err := db.LinkTopic(ctx, store.TopicLink{TopicID: a.ID, Kind: "bogus", Ref: "1"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	link(b.ID, store.TopicDecision, "9", store.TopicLinkDesk)

	// Merge keeps links and the old id resolving.
	m, err := db.MergeTopics(ctx, b.ID, a.ID)
	if err != nil || m.ID != a.ID {
		t.Fatalf("merge: %+v %v", m, err)
	}
	if got, err := db.Topic(ctx, b.ID); err != nil || got.ID != a.ID {
		t.Fatalf("old id: %+v %v", got, err)
	}
	if ls, _ := db.TopicLinks(ctx, a.ID); len(ls) != 2 {
		t.Fatalf("links after merge: %+v", ls)
	}
	if ts, _ := db.Topics(ctx); len(ts) != 1 {
		t.Fatalf("merged topic listed: %+v", ts)
	}
	if _, err := db.MergeTopics(ctx, a.ID, a.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("self merge: %v", err)
	}
	if _, err := db.TopicOf(ctx, store.TopicRun, "1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("General: %v", err)
	}

	cleared := store.TopicCleared
	c, err := db.UpdateTopic(ctx, a.ID, store.TopicPatch{Status: &cleared})
	if err != nil || c.Status != store.TopicCleared || c.ClearedAt == nil {
		t.Fatalf("clear: %+v %v", c, err)
	}
	open := store.TopicOpen
	o, _ := db.UpdateTopic(ctx, a.ID, store.TopicPatch{Status: &open})
	if o.Status != store.TopicOpen || o.ClearedAt != nil {
		t.Fatalf("reopen: %+v", o)
	}
}
