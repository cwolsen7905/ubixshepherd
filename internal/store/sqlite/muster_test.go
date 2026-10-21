package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestMusterItems(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	it, created, err := db.AddMuster(ctx, store.MusterItem{Key: "k1", Kind: "lane.stale", SubjectKind: "lane", SubjectID: 3, Reason: "idle"})
	if err != nil || !created || it.State != store.MusterOpen || it.Created.IsZero() {
		t.Fatalf("add: %+v %v %v", it, created, err)
	}
	again, created, err := db.AddMuster(ctx, store.MusterItem{Key: "k1", Kind: "lane.stale", SubjectKind: "lane", Reason: "other"})
	if err != nil || created || again.ID != it.ID || again.Reason != "idle" {
		t.Fatalf("same key: %+v %v %v", again, created, err)
	}
	if _, _, err := db.AddMuster(ctx, store.MusterItem{Key: "", Kind: "x", SubjectKind: "y"}); err == nil {
		t.Error("an item with no key was accepted")
	}
	db.AddMuster(ctx, store.MusterItem{Key: "k2", Kind: "next.no-repo", SubjectKind: "next", Auto: true})

	t0 := time.Now().UTC()
	until := t0.Add(time.Hour)
	st, before := store.MusterApplied, `{"state":"live"}`
	got, err := db.UpdateMuster(ctx, it.ID, store.MusterPatch{State: &st, Before: &before, Applied: &t0, UndoUntil: &until})
	if err != nil || got.State != store.MusterApplied || got.Before != before || got.Applied == nil || got.UndoUntil == nil || !got.UndoUntil.Equal(until) {
		t.Fatalf("update: %+v %v", got, err)
	}
	if _, err := db.UpdateMuster(ctx, 999, store.MusterPatch{}); err != store.ErrNotFound {
		t.Errorf("missing = %v", err)
	}
	if open, _ := db.MusterItems(ctx, store.MusterFilter{State: store.MusterOpen}); len(open) != 1 || open[0].Key != "k2" || !open[0].Auto {
		t.Errorf("open: %+v", open)
	}
	all, _ := db.MusterItems(ctx, store.MusterFilter{})
	if len(all) != 2 || all[0].Key != "k2" {
		t.Errorf("all, newest first: %+v", all)
	}
}

func TestQuestionAboutAndState(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	q, err := db.AddQuestion(ctx, store.Question{WorkspaceID: 1, Text: "which?", Repo: "app", LaneID: 4, NextID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Question(ctx, q.ID); got.Repo != "app" || got.LaneID != 4 || got.NextID != 9 || !got.HasAbout() {
		t.Errorf("about not stored: %+v", got)
	}
	if (store.Question{Repo: "app"}).HasAbout() || (store.Question{LaneID: 1}).HasAbout() {
		t.Error("a question with half a link has an about")
	}
	out, err := db.SetQuestionState(ctx, q.ID, store.QuestionOutdated)
	if err != nil || out.State != store.QuestionOutdated || out.Closed == nil {
		t.Fatalf("outdated: %+v %v", out, err)
	}
	back, err := db.SetQuestionState(ctx, q.ID, store.QuestionOpen)
	if err != nil || back.State != store.QuestionOpen || back.Closed != nil {
		t.Fatalf("reopened: %+v %v", back, err)
	}
	if _, err := db.SetQuestionState(ctx, 99, store.QuestionOpen); err != store.ErrNotFound {
		t.Errorf("missing = %v", err)
	}
}
