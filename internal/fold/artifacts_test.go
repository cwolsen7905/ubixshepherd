package fold

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

func findArtifact(as []ArtifactInfo, p string) *ArtifactInfo {
	for i := range as {
		if as[i].Path == p {
			return &as[i]
		}
	}
	return nil
}

func TestArtifactsDiscovery(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/art")
	ctx := context.Background()
	wt := l.Worktree

	writeFile(t, wt, "docs/committed.md", "# committed\n")
	writeFile(t, wt, "src/a.go", "package a\n")
	gitT(t, wt, "add", ".")
	gitT(t, wt, "commit", "-q", "-m", "work")
	writeFile(t, wt, "README.md", "hi\n\nmore\n")      // uncommitted edit of a tracked file
	writeFile(t, wt, "docs/plan.md", "# plan\n")       // untracked
	writeFile(t, wt, "src/b.go", "package a\n")        // untracked, not an artifact
	writeFile(t, wt, "shot.png", "\x89PNG\r\n")        // untracked image
	writeFile(t, wt, ".shepherd/artifacts/n.txt", "x") // untracked, by the default globs

	got, err := f.fold.Artifacts(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	for p, src := range map[string]string{"docs/committed.md": "committed", "README.md": "uncommitted",
		"docs/plan.md": "untracked", "shot.png": "untracked", ".shepherd/artifacts/n.txt": "untracked"} {
		if a := findArtifact(got.Artifacts, p); a == nil || a.Source != src || !a.Readable {
			t.Errorf("%s = %+v, want source %s", p, a, src)
		}
	}
	if findArtifact(got.Artifacts, "src/a.go") != nil || findArtifact(got.Artifacts, "src/b.go") != nil {
		t.Errorf("code listed: %+v", got.Artifacts)
	}
	if a := findArtifact(got.Artifacts, "shot.png"); a.Kind != KindImage || a.Mime != "image/png" {
		t.Errorf("image = %+v", a)
	}

	// A repo's own globs replace the defaults.
	c := config.Default()
	c.Repos = map[string]config.Profile{"app": {Artifacts: config.Artifacts{Discover: []string{"docs/plans/**"}}}}
	f.fold.SetConfig(c)
	writeFile(t, wt, "docs/plans/p.md", "# p\n")
	got, err = f.fold.Artifacts(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Path != "docs/plans/p.md" {
		t.Errorf("custom globs = %+v", got.Artifacts)
	}
}

func TestArtifactContentCapsAndPaths(t *testing.T) {
	f := newFixture(t)
	l := f.open("feat/art-content")
	ctx := context.Background()
	wt := l.Worktree

	writeFile(t, wt, "docs/plan.md", "# plan\napi_key=abcd1234efgh\n")
	c, err := f.fold.ArtifactContent(ctx, l.ID, "docs/plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c.Data), "abcd1234efgh") || c.Redacted == 0 || c.Mime != "text/plain; charset=utf-8" {
		t.Errorf("content = %q redacted=%d mime=%s", c.Data, c.Redacted, c.Mime)
	}
	// SVG source is plain text only when read as source; as an image it keeps its type.
	writeFile(t, wt, "d.svg", "<svg/>")
	if c, err := f.fold.ArtifactContent(ctx, l.ID, "d.svg"); err != nil || c.Mime != "image/svg+xml" {
		t.Errorf("svg = %+v %v", c, err)
	}

	// Caps: text 1 MiB, images 10 MiB; the cap itself reads, one byte over does not.
	writeFile(t, wt, "ok.md", strings.Repeat("a", ArtifactMaxText))
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "ok.md"); err != nil {
		t.Errorf("file at the cap refused: %v", err)
	}
	writeFile(t, wt, "big.md", strings.Repeat("a", ArtifactMaxText+1))
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "big.md"); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Errorf("big text: %v", err)
	}
	writeFile(t, wt, "img.png", strings.Repeat("a", ArtifactMaxText+1))
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "img.png"); err != nil {
		t.Errorf("image over the text cap refused: %v", err)
	}
	writeFile(t, wt, "huge.png", strings.Repeat("a", ArtifactMaxImage+1))
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "huge.png"); err == nil {
		t.Error("image over its cap served")
	}
	a, _ := f.fold.Artifacts(ctx, l.ID)
	if x := findArtifact(a.Artifacts, "big.md"); x == nil || x.Readable || x.Size != ArtifactMaxText+1 {
		t.Errorf("big listed = %+v", x)
	}

	// Binary text is refused; an unmatched path is refused.
	writeFile(t, wt, "bin.md", "a\xff\xfe\x00")
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "bin.md"); err == nil {
		t.Error("binary served as text")
	}
	writeFile(t, wt, "src/x.go", "package x\n")
	if _, err := f.fold.ArtifactContent(ctx, l.ID, "src/x.go"); err == nil {
		t.Error("non-artifact served")
	}

	// Paths outside the worktree, and symlinks, are refused.
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("secret\n"), 0o644)
	os.Symlink(outside, filepath.Join(wt, "link.md"))
	os.Symlink(filepath.Dir(outside), filepath.Join(wt, "linkdir"))
	for _, bad := range []string{"../secret.md", "docs/../../x.md", outside, "/etc/hosts.md", "", ".",
		"link.md", "linkdir/secret.md", `a\b.md`, "./docs/plan.md"} {
		if c, err := f.fold.ArtifactContent(ctx, l.ID, bad); err == nil {
			t.Errorf("path %q served: %q", bad, bytes.TrimSpace(c.Data))
		}
	}
}
