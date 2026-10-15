package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestArtifactUpsertKeepsFirstRunAndStrongerIntent(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	a, created, err := db.UpsertArtifact(ctx, store.Artifact{WorkspaceID: 1, LaneID: 7, RunID: 3, Path: "docs/plan.md",
		Kind: store.ArtifactMarkdown, Source: store.ArtifactDiscovered})
	if err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	if a.Intent != store.ArtifactUndecided || a.IntentBy != store.IntentByDefault || a.State != store.ArtifactLive {
		t.Fatalf("defaults: %+v", a)
	}
	// A later declaration by another run sets the intent and title, and keeps run 3.
	b, created, err := db.UpsertArtifact(ctx, store.Artifact{LaneID: 7, RunID: 9, Path: "docs/plan.md", Title: "Plan",
		Source: store.ArtifactDeclared, Intent: store.ArtifactKeep, IntentBy: store.IntentByAgent})
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("second: %+v %v %v", b, created, err)
	}
	if b.RunID != 3 || b.Source != store.ArtifactDiscovered || b.Intent != store.ArtifactKeep || b.Title != "Plan" {
		t.Errorf("merge: %+v", b)
	}
	// A discovery pass afterwards does not undo the agent's word.
	c, _, _ := db.UpsertArtifact(ctx, store.Artifact{LaneID: 7, RunID: 11, Path: "docs/plan.md", Source: store.ArtifactDiscovered})
	if c.Intent != store.ArtifactKeep || c.IntentBy != store.IntentByAgent {
		t.Errorf("default overrode agent: %+v", c)
	}
	// The person's choice, set directly, is never moved by an agent.
	if _, err := db.db.ExecContext(ctx, `UPDATE artifacts SET intent = 'scratch', intent_by = 'person' WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	d, _, _ := db.UpsertArtifact(ctx, store.Artifact{LaneID: 7, Path: "docs/plan.md", Source: store.ArtifactDeclared,
		Intent: store.ArtifactCommit, IntentBy: store.IntentByAgent})
	if d.Intent != store.ArtifactScratch || d.IntentBy != store.IntentByPerson {
		t.Errorf("person's intent moved: %+v", d)
	}
	if _, _, err := db.UpsertArtifact(ctx, store.Artifact{LaneID: 7}); err == nil {
		t.Error("an artifact with no path was accepted")
	}
}

func TestArtifactVersionsAreIdempotentAndCurrent(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	a, _, _ := db.UpsertArtifact(ctx, store.Artifact{LaneID: 1, RunID: 1, Path: "a.md", Source: store.ArtifactDeclared})
	for i, tc := range []struct {
		sha   string
		added bool
	}{{"aaa", true}, {"aaa", false}, {"bbb", true}} {
		got, err := db.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: tc.sha, Size: int64(i + 1), RunID: int64(i + 1)})
		if err != nil || got != tc.added {
			t.Fatalf("version %s: added %v err %v", tc.sha, got, err)
		}
	}
	got, err := db.Artifact(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != "bbb" || len(got.Versions) != 2 {
		t.Fatalf("artifact: sha %q, %d versions", got.SHA256, len(got.Versions))
	}
	// Re-seeing the first content later does not move the current version back.
	db.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: "aaa", Size: 1})
	if got, _ := db.Artifact(ctx, a.ID); got.SHA256 != "bbb" {
		t.Errorf("current moved back to %q", got.SHA256)
	}
	if _, err := db.Artifact(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestArtifactListFiltersAndLinks(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	mk := func(lane, run int64, path, kind string) store.Artifact {
		a, _, err := db.UpsertArtifact(ctx, store.Artifact{WorkspaceID: 1, LaneID: lane, RunID: run, Path: path, Kind: kind, Source: store.ArtifactDeclared})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	plan := mk(1, 10, "plan.md", store.ArtifactMarkdown)
	pic := mk(1, 11, "a.png", store.ArtifactImage)
	other := mk(2, 12, "notes.md", store.ArtifactMarkdown)
	ids := func(f store.ArtifactFilter) map[int64]bool {
		l, err := db.ListArtifacts(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]bool{}
		for _, a := range l {
			m[a.ID] = true
		}
		return m
	}
	if m := ids(store.ArtifactFilter{LaneID: 1}); len(m) != 2 || !m[plan.ID] || !m[pic.ID] {
		t.Errorf("lane: %v", m)
	}
	if m := ids(store.ArtifactFilter{RunID: 12}); len(m) != 1 || !m[other.ID] {
		t.Errorf("run: %v", m)
	}
	if m := ids(store.ArtifactFilter{Kind: store.ArtifactImage}); len(m) != 1 || !m[pic.ID] {
		t.Errorf("kind: %v", m)
	}
	if m := ids(store.ArtifactFilter{State: store.ArtifactGone}); len(m) != 0 {
		t.Errorf("state: %v", m)
	}

	// Links are idempotent, and a decision reaches its artifacts.
	l := store.ArtifactLink{ArtifactID: plan.ID, Kind: store.ArtifactLinkDecision, Ref: "5"}
	for i := 0; i < 2; i++ {
		if err := db.LinkArtifact(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := db.Artifact(ctx, plan.ID); len(got.Links) != 1 {
		t.Errorf("links: %+v", got.Links)
	}
	if m := ids(store.ArtifactFilter{DecisionID: 5}); len(m) != 1 || !m[plan.ID] {
		t.Errorf("decision: %v", m)
	}
	if got, _ := db.DecisionArtifacts(ctx, 5); len(got) != 1 {
		t.Errorf("DecisionArtifacts: %v", got)
	}

	// A topic holds an artifact by its own link, or through its lane when it has none.
	tp, err := db.AddTopic(ctx, store.Topic{Title: "export"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.LinkTopic(ctx, store.TopicLink{TopicID: tp.ID, Kind: store.TopicLane, Ref: "1", By: store.TopicLinkPerson}); err != nil {
		t.Fatal(err)
	}
	if m := ids(store.ArtifactFilter{TopicID: tp.ID}); len(m) != 2 || !m[plan.ID] || !m[pic.ID] {
		t.Errorf("topic via lane: %v", m)
	}
	tp2, _ := db.AddTopic(ctx, store.Topic{Title: "other"})
	if _, err := db.LinkTopic(ctx, store.TopicLink{TopicID: tp2.ID, Kind: store.TopicArtifact, Ref: "1", By: store.TopicLinkPerson}); err != nil {
		t.Fatal(err)
	}
	if m := ids(store.ArtifactFilter{TopicID: tp.ID}); len(m) != 1 || !m[pic.ID] {
		t.Errorf("topic after own link: %v", m)
	}
	if m := ids(store.ArtifactFilter{TopicID: tp2.ID}); len(m) != 1 || !m[plan.ID] {
		t.Errorf("topic own link: %v", m)
	}
}
