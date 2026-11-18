package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestAttachments(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	a, err := db.AddAttachment(ctx, store.Attachment{WorkspaceID: 1, SHA: "aa11", Name: "a.png", Mime: "image/png", Kind: "image", Size: 5})
	if err != nil || a.ID == 0 {
		t.Fatal(a, err)
	}
	b, _ := db.AddAttachment(ctx, store.Attachment{WorkspaceID: 1, SHA: "aa11", Name: "b.png", Mime: "image/png", Kind: "image", Size: 5})
	other, _ := db.AddAttachment(ctx, store.Attachment{WorkspaceID: 2, SHA: "bb22", Name: "o.log", Mime: "text/plain", Kind: "text", Size: 1})
	if _, err := db.TakeAttachments(ctx, 1, 9, []int64{a.ID, other.ID}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("another workspace's file: %v", err)
	}
	if got, _ := db.Attachment(ctx, a.ID); got.Turn != 0 {
		t.Errorf("a failed take kept a file: %+v", got)
	}
	e, err := db.AddDeskEvent(ctx, store.DeskEvent{WorkspaceID: 1, Kind: store.DeskUser, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.TakeAttachments(ctx, 1, e.Turn, []int64{b.ID, a.ID})
	if err != nil || len(got) != 2 || got[0].ID != b.ID {
		t.Fatalf("take: %+v %v", got, err)
	}
	if _, err := db.TakeAttachments(ctx, 1, e.Turn, []int64{a.ID}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("taken twice: %v", err)
	}
	es, err := db.DeskEvents(ctx, 1, 0, 10)
	if err != nil || len(es) != 1 || len(es[0].Attachments) != 2 || es[0].Attachments[0].Name != "a.png" {
		t.Fatalf("events: %+v %v", es, err)
	}
	// Nothing is old enough yet except by the clock; the other workspace's file is orphaned.
	free, err := db.SweepAttachments(ctx, time.Now().Add(time.Minute))
	if err != nil || len(free) != 1 || free[0] != "bb22" {
		t.Fatalf("sweep: %v %v", free, err)
	}
	if _, err := db.Attachment(ctx, other.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("swept file: %v", err)
	}
	// Trimming the turn away frees the shared hash.
	if err := db.TrimDeskEvents(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	free, _ = db.SweepAttachments(ctx, time.Now())
	if len(free) != 1 || free[0] != "aa11" {
		t.Errorf("after trim: %v", free)
	}
}
