package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoWithChanges(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	run("init", "-q")
	write("a.txt", "one\ntwo\nthree\nfour\nfive\n")
	write("old name.txt", "keep\nkeep\nkeep\nkeep\nkeep\n")
	write("gone.txt", "q\nw\ne\nr\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("tag", "base")
	write("a.txt", "one\nTWO\nthree\nfour\nfive\nsix\n")
	run("mv", "old name.txt", "new name.txt")
	run("rm", "-q", "gone.txt")
	write("bin.dat", "a\x00b")
	write("added.txt", "completely\ndifferent\n")
	run("add", ".")
	run("commit", "-q", "-m", "work: a subject")
	write("untracked.txt", "u\n")
	return dir
}

func TestDiffStat(t *testing.T) {
	dir := repoWithChanges(t)
	ctx := context.Background()
	stats, err := DiffStat(ctx, dir, "base..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]FileStat{}
	for _, s := range stats {
		by[s.Path] = s
	}
	if s := by["a.txt"]; s.Status != "M" || s.Added != 2 || s.Removed != 1 {
		t.Errorf("a.txt = %+v", s)
	}
	if s := by["new name.txt"]; s.Status != "R" || s.OldPath != "old name.txt" {
		t.Errorf("rename = %+v", s)
	}
	if s := by["gone.txt"]; s.Status != "D" || s.Removed != 4 {
		t.Errorf("gone.txt = %+v", s)
	}
	if s := by["bin.dat"]; !s.Binary || s.Status != "A" {
		t.Errorf("bin.dat = %+v", s)
	}
}

func TestLogRangeAndHunks(t *testing.T) {
	dir := repoWithChanges(t)
	ctx := context.Background()
	cs, total, err := LogRange(ctx, dir, "base..HEAD", 30)
	if err != nil || total != 1 || len(cs) != 1 {
		t.Fatalf("log = %v %d %v", cs, total, err)
	}
	if cs[0].Subject != "work: a subject" || cs[0].Files != 5 {
		t.Errorf("commit = %+v", cs[0])
	}
	h, err := DiffHunks(ctx, dir, 5, "base..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(h["a.txt"]) != 2 || !strings.HasPrefix(h["a.txt"][0], "@@ -2") {
		t.Errorf("hunks = %v", h["a.txt"])
	}
}

func TestUntrackedFilesStopsAtLimit(t *testing.T) {
	dir := repoWithChanges(t)
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "u"+string(rune('a'+i))), nil, 0o644)
	}
	got, more, err := UntrackedFiles(context.Background(), dir, 3)
	if err != nil || len(got) != 3 || !more {
		t.Errorf("got %v more=%v err=%v", got, more, err)
	}
	got, more, err = UntrackedFiles(context.Background(), dir, 50)
	if err != nil || more || len(got) != 6 {
		t.Errorf("got %v more=%v err=%v", got, more, err)
	}
}

func TestPatchBounded(t *testing.T) {
	dir := repoWithChanges(t)
	p, cut, err := Patch(context.Background(), dir, 40, "base", "--", "a.txt")
	if err != nil || !cut || len(p) > 40 || !strings.HasSuffix(p, "\n") {
		t.Errorf("patch = %q cut=%v err=%v", p, cut, err)
	}
	p, cut, err = Patch(context.Background(), dir, 10000, "base", "--", "a.txt")
	if err != nil || cut || !strings.Contains(p, "+six") {
		t.Errorf("patch = %q cut=%v err=%v", p, cut, err)
	}
}
