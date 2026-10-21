package fold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func (f *fixture) withSnapshots() string {
	dir := filepath.Join(f.t.TempDir(), "artifacts")
	f.fold.ArtifactDir = dir
	return dir
}

func (f *fixture) merge(l store.Lane) {
	f.t.Helper()
	gitT(f.t, l.Worktree, "push", "-q", "origin", "HEAD:main")
}

func (f *fixture) artifact(laneID int64, p string) store.Artifact {
	f.t.Helper()
	a, err := f.fold.Store.LaneArtifact(context.Background(), laneID, p)
	if err != nil {
		f.t.Fatalf("artifact %s: %v", p, err)
	}
	return a
}

func TestOpenExcludesShepherdDirOnce(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/ex")
	ex, err := os.ReadFile(filepath.Join(f.repo.Path, ".git", "info", "exclude"))
	if err != nil || strings.Count(string(ex), ExcludeLine) != 1 {
		t.Fatalf("exclude = %q, %v", ex, err)
	}
	writeFile(t, l.Worktree, ".shepherd/artifacts/plan.md", "# plan\n")
	if out := gitT(t, l.Worktree, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty: %q", out)
	}
	if _, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: "feat/ex2", Scope: []string{"docs/**"}}); err != nil {
		t.Fatal(err)
	}
	ex, _ = os.ReadFile(filepath.Join(f.repo.Path, ".git", "info", "exclude"))
	if strings.Count(string(ex), ExcludeLine) != 1 {
		t.Errorf("exclude written twice: %q", ex)
	}
}

func TestOpenRespectsRefusedExclude(t *testing.T) {
	f := newFixture(t)
	f.fold.Store.SetSetting(context.Background(), ExcludeSetting("app"), "off")
	f.open("feat/noex")
	ex, _ := os.ReadFile(filepath.Join(f.repo.Path, ".git", "info", "exclude"))
	if strings.Contains(string(ex), ExcludeLine) {
		t.Errorf("exclude written despite refusal: %q", ex)
	}
}

func TestCloseSnapshotsKeptArtifactAndRemovesWorktree(t *testing.T) {
	f := newFixture(t)
	dir := f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/keep")
	f.commit(l.Worktree, "src/a.go")
	writeFile(t, l.Worktree, ".shepherd/artifacts/plan.md", "# plan\napi_key=abcd1234efgh\n")
	f.merge(l)

	// No clean-up needed by the person: the Shepherd directory never blocks.
	res, err := f.fold.Close(ctx, l.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Lane.State != store.LaneClosed {
		t.Fatalf("state = %s", res.Lane.State)
	}
	if _, err := os.Stat(l.Worktree); !os.IsNotExist(err) {
		t.Error("worktree left behind")
	}
	a := f.artifact(l.ID, ".shepherd/artifacts/plan.md")
	if a.State != store.ArtifactKept || a.Closed == nil || a.Intent != store.ArtifactKeep {
		t.Fatalf("artifact = %+v", a)
	}
	c, err := f.fold.ArtifactSnapshot(ctx, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c.Data), "abcd1234efgh") || !strings.Contains(string(c.Data), "# plan") {
		t.Errorf("snapshot = %q", c.Data)
	}
	if got, _ := f.fold.Store.Artifact(ctx, a.ID); len(got.Versions) != 1 || got.Versions[0].Snapshotted == nil || got.Versions[0].RunID != 0 {
		t.Errorf("versions = %+v", got.Versions)
	}
	if _, err := os.Stat(filepath.Join(dir, "1")); err != nil && !os.IsNotExist(err) {
		t.Error(err)
	}
}

func TestCloseAsksAboutUndecidedThenLetsGoOfKept(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/undecided")
	f.commit(l.Worktree, "src/a.go")
	f.merge(l)
	writeFile(t, l.Worktree, "docs/plan.md", "# plan\n")

	_, err := f.fold.Close(ctx, l.ID, false)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "1 file(s) to keep or discard") || !strings.Contains(err.Error(), "docs/plan.md") {
		t.Fatalf("close = %v", err)
	}
	a := f.artifact(l.ID, "docs/plan.md") // recorded by the attempt, outside a run
	if a.Intent != store.ArtifactUndecided || a.RunID != 0 {
		t.Fatalf("artifact = %+v", a)
	}
	keep := store.ArtifactKeep
	if _, err := f.fold.PatchArtifact(ctx, a.ID, ArtifactPatch{Intent: &keep}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}
	a = f.artifact(l.ID, "docs/plan.md")
	if a.State != store.ArtifactKept || a.IntentBy != store.IntentByPerson {
		t.Errorf("artifact = %+v", a)
	}
}

func TestCloseDoesNotLetGoOfTrackedChange(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	l := f.open("feat/tracked")
	f.commit(l.Worktree, "src/a.go")
	f.merge(l)
	writeFile(t, l.Worktree, "README.md", "changed\n")
	_, err := f.fold.Close(context.Background(), l.ID, false)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("close = %v", err)
	}
}

func TestScratchIsNotSnapshottedUnlessPinned(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/scratch")
	f.commit(l.Worktree, "src/a.go")
	f.merge(l)
	writeFile(t, l.Worktree, "notes.md", "n\n")
	writeFile(t, l.Worktree, "pinned.md", "p\n")
	if err := f.fold.SnapshotLane(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	scratch, pin := store.ArtifactScratch, true
	for _, p := range []string{"notes.md", "pinned.md"} {
		a := f.artifact(l.ID, p)
		patch := ArtifactPatch{Intent: &scratch}
		if p == "pinned.md" {
			patch.Pinned = &pin
		}
		if _, err := f.fold.PatchArtifact(ctx, a.ID, patch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}
	if a := f.artifact(l.ID, "notes.md"); a.State != store.ArtifactGone {
		t.Errorf("scratch = %+v", a)
	}
	if a := f.artifact(l.ID, "pinned.md"); a.State != store.ArtifactKept {
		t.Errorf("pinned scratch = %+v", a)
	} else if _, err := f.fold.ArtifactSnapshot(ctx, a.ID, ""); err != nil {
		t.Error(err)
	}
}

func TestCloseMergedAsksAboutUndecided(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/merged")
	f.commit(l.Worktree, "src/a.go")
	head := gitT(t, l.Worktree, "rev-parse", "HEAD")
	writeFile(t, l.Worktree, ".shepherd/artifacts/a.md", "a\n")
	writeFile(t, l.Worktree, "docs/b.md", "b\n")
	_, err := f.fold.CloseMerged(ctx, l.ID, head, head)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "1 file(s) to keep or discard") || strings.Contains(err.Error(), ".shepherd/artifacts") {
		t.Fatalf("close merged = %v", err)
	}
	disc := store.ArtifactDiscarded
	_ = disc
	b := f.artifact(l.ID, "docs/b.md")
	if _, err := f.fold.DiscardArtifact(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.Worktree, "docs/b.md")); !os.IsNotExist(err) {
		t.Error("discarded file still there")
	}
	if _, err := f.fold.CloseMerged(ctx, l.ID, head, head); err != nil {
		t.Fatal(err)
	}
	if a := f.artifact(l.ID, ".shepherd/artifacts/a.md"); a.State != store.ArtifactKept {
		t.Errorf("kept = %+v", a)
	}
	if a := f.artifact(l.ID, "docs/b.md"); a.State != store.ArtifactDiscarded {
		t.Errorf("discarded = %+v", a)
	}
}

func TestMoveToKeepAndDiscardRules(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/move")
	writeFile(t, l.Worktree, "docs/plan.md", "# plan\n")
	writeFile(t, l.Worktree, "README.md", "changed\n")
	if err := f.fold.SnapshotLane(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	a := f.artifact(l.ID, "docs/plan.md")
	moved, err := f.fold.PatchArtifact(ctx, a.ID, ArtifactPatch{Move: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Path != ".shepherd/artifacts/docs/plan.md" || moved.Intent != store.ArtifactKeep {
		t.Errorf("moved = %+v", moved)
	}
	if _, err := os.Stat(filepath.Join(l.Worktree, "docs/plan.md")); !os.IsNotExist(err) {
		t.Error("original left")
	}
	if out := gitT(t, l.Worktree, "status", "--porcelain"); strings.Contains(out, "plan.md") {
		t.Errorf("moved file still shows in git: %q", out)
	}
	// A tracked file is not moved or discarded here.
	r := f.artifact(l.ID, "README.md")
	if _, err := f.fold.PatchArtifact(ctx, r.ID, ArtifactPatch{Move: "keep"}); !errors.Is(err, ErrRefused) {
		t.Errorf("move tracked: %v", err)
	}
	if _, err := f.fold.DiscardArtifact(ctx, r.ID); !errors.Is(err, ErrRefused) {
		t.Errorf("discard tracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Worktree, "README.md")); err != nil {
		t.Error(err)
	}
}

func TestArtifactGC(t *testing.T) {
	f := newFixture(t)
	dir := f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/gc")
	f.commit(l.Worktree, "src/a.go")
	f.merge(l)
	for i, name := range []string{"old", "pinned", "recent"} {
		writeFile(t, l.Worktree, ".shepherd/artifacts/"+name+".md", strings.Repeat(name, 10))
		_ = i
	}
	if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}
	old, pinned, recent := f.artifact(l.ID, ".shepherd/artifacts/old.md"), f.artifact(l.ID, ".shepherd/artifacts/pinned.md"), f.artifact(l.ID, ".shepherd/artifacts/recent.md")
	f.fold.Store.SetArtifactPinned(ctx, pinned.ID, true)
	files := func() int { e, _ := os.ReadDir(dir); return len(e) }
	if files() != 3 {
		t.Fatalf("snapshots = %d", files())
	}
	// Everything closed now: nothing is old enough.
	if res, err := f.fold.ArtifactGC(ctx, 30, 0, false, time.Now()); err != nil || res.Expired != 0 {
		t.Fatalf("gc now = %+v, %v", res, err)
	}
	// Forty days on, only the pin stays. A dry run removes nothing.
	later := time.Now().AddDate(0, 0, 40)
	if res, _ := f.fold.ArtifactGC(ctx, 30, 0, true, later); res.Expired != 2 || files() != 3 {
		t.Fatalf("dry gc = %+v, files %d", res, files())
	}
	res, err := f.fold.ArtifactGC(ctx, 30, 0, false, later)
	if err != nil || res.Expired != 2 || files() != 1 {
		t.Fatalf("gc = %+v, %v, files %d", res, err, files())
	}
	if a, _ := f.fold.Store.Artifact(ctx, old.ID); a.State != store.ArtifactGone {
		t.Errorf("old = %+v", a)
	}
	if a, _ := f.fold.Store.Artifact(ctx, recent.ID); a.State != store.ArtifactGone {
		t.Errorf("recent = %+v", a)
	}
	if a, _ := f.fold.Store.Artifact(ctx, pinned.ID); a.State != store.ArtifactKept {
		t.Errorf("pinned = %+v", a)
	}
	// An orphan directory goes.
	os.MkdirAll(filepath.Join(dir, "9999"), 0o700)
	if res, _ := f.fold.ArtifactGC(ctx, 30, 0, false, later); res.Orphans != 1 {
		t.Errorf("orphans = %+v", res)
	}
}

func TestArtifactGCCapEvictsOldestFirst(t *testing.T) {
	f := newFixture(t)
	dir := f.withSnapshots()
	ctx := context.Background()
	var ids []int64
	for _, n := range []string{"first", "second"} {
		l := f.open("feat/cap-" + n)
		f.commit(l.Worktree, "src/"+n+".go")
		f.merge(l)
		writeFile(t, l.Worktree, ".shepherd/artifacts/"+n+".md", strings.Repeat("x", 1000))
		if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.artifact(l.ID, ".shepherd/artifacts/"+n+".md").ID)
		time.Sleep(5 * time.Millisecond)
	}
	res, err := f.fold.ArtifactGC(ctx, 30, 1500, false, time.Now())
	if err != nil || res.Evicted != 1 {
		t.Fatalf("gc = %+v, %v", res, err)
	}
	if e, _ := os.ReadDir(dir); len(e) != 1 || e[0].Name() != "2" {
		t.Errorf("left = %v (ids %v)", e, ids)
	}
}

// declaredImage records a screenshot an agent declared, the way a run's end does.
func (f *fixture) declaredImage(l store.Lane, p, intent string, size int) store.Artifact {
	f.t.Helper()
	ctx := context.Background()
	writeFile(f.t, l.Worktree, p, strings.Repeat("x", size))
	_, sum, err := readLive(l.Worktree, p, 64<<20)
	if err != nil {
		f.t.Fatal(err)
	}
	a, _, err := f.fold.Store.UpsertArtifact(ctx, store.Artifact{WorkspaceID: f.repo.WorkspaceID, RepoID: f.repo.ID, LaneID: l.ID,
		Path: p, Kind: store.ArtifactImage, Source: store.ArtifactDeclared, Intent: intent, IntentBy: store.IntentByAgent, SHA256: sum, Size: int64(size)})
	if err != nil {
		f.t.Fatal(err)
	}
	f.fold.Store.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: sum, Size: int64(size), RunID: 1})
	f.fold.SnapshotKnown(ctx, l.ID)
	return a
}

func TestCloseKeepsDeclaredScreenshots(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		size       int
		want       string
	}{
		{"in the Shepherd directory", ".shepherd/artifacts/shot.png", 100, store.ArtifactKept},
		{"elsewhere in the worktree", "docs/shot.png", 100, store.ArtifactKept},
		{"over the live view's cap", ".shepherd/artifacts/full-page.png", ArtifactMaxImage + 1, store.ArtifactKept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.withSnapshots()
			ctx := context.Background()
			l := f.open("feat/shot")
			f.commit(l.Worktree, "src/a.go")
			f.merge(l)
			a := f.declaredImage(l, tc.path, store.ArtifactKeep, tc.size)
			if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
				t.Fatal(err)
			}
			if got := f.artifact(l.ID, tc.path); got.State != tc.want {
				t.Fatalf("state = %s, want %s", got.State, tc.want)
			}
			c, err := f.fold.ArtifactSnapshot(ctx, a.ID, "")
			if err != nil || len(c.Data) != tc.size || !strings.HasPrefix(c.Mime, "image/") {
				t.Fatalf("snapshot = %d bytes, %q, %v", len(c.Data), c.Mime, err)
			}
		})
	}
}

func TestCloseSaysWhenAWantedArtifactHasNoCopy(t *testing.T) {
	f := newFixture(t)
	f.withSnapshots()
	ctx := context.Background()
	l := f.open("feat/nocopy")
	f.commit(l.Worktree, "src/a.go")
	f.merge(l)
	// A declared image whose file went before any copy was taken, and an unpinned
	// scratch file in the same state: only the first was worth a copy.
	// Also: a declared image just over the snapshot cap (a sparse file), and a
	// discovered text file that is not valid UTF-8, which is never copied.
	cases := []struct{ p, intent, source string }{
		{"shots/lost.png", store.ArtifactKeep, store.ArtifactDeclared},
		{"shots/scratch.png", store.ArtifactScratch, store.ArtifactDeclared},
		{"shots/huge.png", store.ArtifactKeep, store.ArtifactDeclared},
		{"notes/binary.txt", store.ArtifactKeep, store.ArtifactDiscovered},
	}
	for _, c := range cases {
		p := c.p
		kind := store.ArtifactKindOf(p)
		sum := strings.Repeat("a", 64)
		switch p {
		case "shots/huge.png":
			writeFile(t, l.Worktree, p, "")
			if err := os.Truncate(filepath.Join(l.Worktree, p), MaxSnapshotImage+1); err != nil {
				t.Fatal(err)
			}
		case "notes/binary.txt":
			writeFile(t, l.Worktree, p, "\xff\xfe not utf-8")
			_, sum, _ = readLive(l.Worktree, p, 64<<20)
		default:
			writeFile(t, l.Worktree, p, "x")
			_, sum, _ = readLive(l.Worktree, p, 64<<20)
		}
		a, _, err := f.fold.Store.UpsertArtifact(ctx, store.Artifact{WorkspaceID: f.repo.WorkspaceID, RepoID: f.repo.ID, LaneID: l.ID,
			Path: p, Kind: kind, Source: c.source, Intent: c.intent, IntentBy: store.IntentByAgent, SHA256: sum, Size: 1})
		if err != nil {
			t.Fatal(err)
		}
		f.fold.Store.AddArtifactVersion(ctx, store.ArtifactVersion{ArtifactID: a.ID, SHA256: sum, Size: 1, RunID: 1})
		if p == "shots/lost.png" || p == "shots/scratch.png" {
			if err := os.Remove(filepath.Join(l.Worktree, p)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.fold.Close(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"shots/lost.png", "shots/scratch.png", "shots/huge.png", "notes/binary.txt"} {
		if a := f.artifact(l.ID, p); a.State != store.ArtifactGone {
			t.Fatalf("%s state = %s", p, a.State)
		}
	}
	feed, err := f.fold.Store.Feed(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, it := range feed {
		if it.Kind == store.FeedArtifact && strings.Contains(it.Text, "no stored copy") {
			lines = append(lines, it.Text)
		}
	}
	// One line each for the two declared images; none for the scratch file or the
	// discovered text file.
	if len(lines) != 2 || !strings.Contains(strings.Join(lines, "\n"), "shots/lost.png") || !strings.Contains(strings.Join(lines, "\n"), "shots/huge.png") ||
		strings.Contains(strings.Join(lines, "\n"), "scratch") || strings.Contains(strings.Join(lines, "\n"), "binary") || !strings.Contains(lines[0], "feat/nocopy") {
		t.Errorf("feed lines = %q", lines)
	}
}
