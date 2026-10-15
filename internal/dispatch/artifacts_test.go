package dispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func writeIn(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func feedCount(f *fixture, kind, contains string) int {
	items, _ := f.st.Feed(context.Background(), 0, 500)
	n := 0
	for _, it := range items {
		if it.Kind == kind && strings.Contains(it.Text, contains) {
			n++
		}
	}
	return n
}

func TestDeclareArtifactRecordsVersionsAndFeed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	f.wait(t, run.ID)

	writeIn(t, f.lane.Worktree, ".shepherd/artifacts/plan.md", "# plan\n")
	a, created, added, err := f.runner.DeclareArtifact(ctx, run.ID, ".shepherd/artifacts/plan.md", "The plan", "keep")
	if err != nil || !created || !added {
		t.Fatalf("declare: %v %v %v", created, added, err)
	}
	if a.RunID != run.ID || a.LaneID != f.lane.ID || a.Kind != store.ArtifactMarkdown || a.Intent != store.ArtifactKeep ||
		a.IntentBy != store.IntentByAgent || a.Title != "The plan" || a.Size != 7 || a.WorkspaceID == 0 {
		t.Errorf("artifact = %+v", a)
	}
	if k := feedKind(t, f, "produced .shepherd/artifacts/plan.md (kept local)"); k != store.FeedArtifact {
		t.Errorf("feed kind = %q", k)
	}

	// The same content again says nothing; changed content is a version and a line.
	if _, created, added, err := f.runner.DeclareArtifact(ctx, run.ID, "./.shepherd/artifacts/plan.md", "", ""); err != nil || created || added {
		t.Errorf("repeat: %v %v %v", created, added, err)
	}
	writeIn(t, f.lane.Worktree, ".shepherd/artifacts/plan.md", "# plan v2\n")
	// An absolute path inside the worktree is accepted.
	got, created, added, err := f.runner.DeclareArtifact(ctx, run.ID, filepath.Join(f.lane.Worktree, ".shepherd", "artifacts", "plan.md"), "", "")
	if err != nil || created || !added {
		t.Fatalf("changed: %v %v %v", created, added, err)
	}
	// An empty intent from a later declaration leaves the agent's.
	if got.Intent != store.ArtifactKeep {
		t.Errorf("intent = %q", got.Intent)
	}
	full, _ := f.st.Artifact(ctx, a.ID)
	if len(full.Versions) != 2 {
		t.Errorf("versions = %+v", full.Versions)
	}
	if n := feedCount(f, store.FeedArtifact, "plan.md"); n != 2 {
		t.Errorf("feed lines = %d, want 2 (produced, changed)", n)
	}
}

func TestDeclareArtifactRefusals(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	run, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	f.wait(t, run.ID)
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("s"), 0o644)
	writeIn(t, f.lane.Worktree, "docs/ok.md", "ok")
	big := filepath.Join(f.lane.Worktree, "docs", "dir")
	os.MkdirAll(big, 0o755)

	cases := map[string]string{
		"../secret.md":     "not a path inside",
		outside:            "outside this lane's worktree",
		"":                 "needs a path",
		"nope.md":          "does not exist",
		".git/config":      "git's own",
		"docs/dir":         "not a regular file",
		"docs/ok.md/../..": "not a path inside",
	}
	if runtime.GOOS != "windows" {
		os.Symlink(outside, filepath.Join(f.lane.Worktree, "docs", "link.md"))
		os.Symlink(filepath.Dir(outside), filepath.Join(f.lane.Worktree, "docs", "linkdir"))
		cases["docs/link.md"] = "symlink"
		cases["docs/linkdir/secret.md"] = "symlink"
	}
	for p, want := range cases {
		_, _, _, err := f.runner.DeclareArtifact(ctx, run.ID, p, "", "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("declare %q: err = %v, want it to contain %q", p, err, want)
		}
	}
	if _, _, _, err := f.runner.DeclareArtifact(ctx, run.ID, "docs/ok.md", "", "publish"); err == nil || !strings.Contains(err.Error(), "intent") {
		t.Errorf("bad intent: %v", err)
	}
	if _, _, _, err := f.runner.DeclareArtifact(ctx, 9999, "docs/ok.md", "", ""); err == nil {
		t.Error("declared on a run that does not exist")
	}
	if list, _ := f.st.ListArtifacts(ctx, store.ArtifactFilter{}); len(list) != 0 {
		t.Errorf("refused declarations left rows: %+v", list)
	}
}

// A run's end records the files the repo's globs match and everything under the Shepherd
// directory, attributed to the run that first saw each content, and says so once.
func TestRunEndRecordsDiscoveredArtifacts(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	f.runner.ArtifactScan = (&fold.Fold{Store: f.st, Config: config.Default()}).Artifacts
	writeIn(t, f.lane.Worktree, "docs/plans/export.md", "# export\n")
	writeIn(t, f.lane.Worktree, ".shepherd/artifacts/notes.txt", "notes\n")
	writeIn(t, f.lane.Worktree, "build/out.bin", "binary")

	r1, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	f.wait(t, r1.ID)
	waitFeed(t, f, fmt.Sprintf("run %d produced .shepherd/artifacts/notes.txt", r1.ID))
	list, err := f.st.ListArtifacts(ctx, store.ArtifactFilter{LaneID: f.lane.ID})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]store.Artifact{}
	for _, a := range list {
		by[a.Path] = a
	}
	plan, notes := by["docs/plans/export.md"], by[".shepherd/artifacts/notes.txt"]
	if plan.ID == 0 || notes.ID == 0 {
		t.Fatalf("recorded = %+v", list)
	}
	if _, ok := by["build/out.bin"]; ok {
		t.Error("a file no glob matches was recorded")
	}
	if plan.RunID != r1.ID || plan.Intent != store.ArtifactUndecided || plan.IntentBy != store.IntentByDefault || plan.Source != store.ArtifactDiscovered {
		t.Errorf("plan = %+v", plan)
	}
	if notes.Intent != store.ArtifactKeep || notes.Kind != store.ArtifactText {
		t.Errorf("notes = %+v", notes)
	}

	// The next run changes one file and leaves the other: one new line, attributed to it.
	writeIn(t, f.lane.Worktree, "docs/plans/export.md", "# export v2\n")
	r2, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "again"})
	f.wait(t, r2.ID)
	waitFeed(t, f, fmt.Sprintf("run %d changed docs/plans/export.md", r2.ID))
	full, _ := f.st.Artifact(ctx, plan.ID)
	if full.RunID != r1.ID || len(full.Versions) != 2 || full.Versions[0].RunID != r2.ID || full.Versions[1].RunID != r1.ID {
		t.Errorf("plan after run 2 = %+v", full)
	}
	if n := feedCount(f, store.FeedArtifact, ""); n != 3 {
		t.Errorf("artifact feed lines = %d, want 3", n)
	}
}

// The snapshot hook runs after the run end has recorded versions, so a version first
// seen at the end is already in the store when the copy is taken.
func TestRunEndSnapshotsAfterRecordingVersions(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "ok")
	var seen int
	f.runner.ArtifactSnapshot = func(ctx context.Context, laneID int64) {
		list, _ := f.st.ListArtifacts(ctx, store.ArtifactFilter{LaneID: laneID})
		for _, a := range list {
			if full, err := f.st.Artifact(ctx, a.ID); err == nil && len(full.Versions) > 0 {
				seen += len(full.Versions)
			}
		}
	}
	writeIn(t, f.lane.Worktree, ".shepherd/artifacts/plan.md", "# plan\n")
	r, _ := f.runner.Start(ctx, StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	f.wait(t, r.ID)
	// The outcome line is written after the snapshot hook has run.
	waitFeed(t, f, fmt.Sprintf("run %d: claude in lane", r.ID))
	if seen != 1 {
		t.Errorf("the snapshot hook saw %d recorded versions, want 1", seen)
	}
}
