package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnsureExcludeIsIdempotentAndKeepsOtherLines(t *testing.T) {
	dir := t.TempDir()
	if _, err := Run(context.Background(), dir, "init", "-q"); err != nil {
		t.Skip("git init:", err)
	}
	file := filepath.Join(dir, ".git", "info", "exclude")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("*.swp"), 0o644) // no trailing newline
	added, got, err := EnsureExclude(context.Background(), dir, "/.shepherd/artifacts/")
	if err != nil || !added {
		t.Fatalf("first = %v, %v", added, err)
	}
	if a, _, err := EnsureExclude(context.Background(), dir, "/.shepherd/artifacts/"); err != nil || a {
		t.Errorf("second = %v, %v", a, err)
	}
	b, _ := os.ReadFile(got)
	if string(b) != "*.swp\n/.shepherd/artifacts/\n" {
		t.Errorf("exclude = %q", b)
	}
	if !strings.HasSuffix(got, filepath.Join(".git", "info", "exclude")) {
		t.Errorf("file = %s", got)
	}
}

func TestDirtyPathsSurvivesTrimmedFirstLine(t *testing.T) {
	// Run trims its output, so a first line of " M a" arrives as "M a".
	got := DirtyPaths("M a.md\n?? b/c.md\nR  old -> new.md\nA  \"q q.md\"")
	want := map[string]string{"a.md": " M", "b/c.md": "??", "new.md": "R ", "q q.md": "A "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
