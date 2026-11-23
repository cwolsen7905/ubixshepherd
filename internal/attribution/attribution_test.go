package attribution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyIsIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	f := filepath.Join(t.TempDir(), "MSG")
	if err := os.WriteFile(f, []byte("feat: x\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := Who{Run: 12, Agent: "claude", Model: "opus"}
	for i := 0; i < 3; i++ {
		if err := Apply(context.Background(), f, w); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(f)
	s := string(b)
	for _, want := range []string{"Co-Authored-By: Claude <noreply@anthropic.com>", "Shepherd-Run: 12", "Shepherd-Model: opus"} {
		if n := strings.Count(s, want); n != 1 {
			t.Errorf("%q appears %d times in %q", want, n, s)
		}
	}
}

func TestNoRunNoTrailers(t *testing.T) {
	f := filepath.Join(t.TempDir(), "MSG")
	os.WriteFile(f, []byte("feat: x\n"), 0o644)
	if err := Apply(context.Background(), f, Who{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != "feat: x\n" {
		t.Errorf("changed: %q", b)
	}
}

func TestFooter(t *testing.T) {
	w := Who{Run: 7, Agent: "claude", Model: "sonnet"}
	got := AddFooter("Does a thing.", w)
	if want := "Does a thing.\n\nMade by claude (sonnet) in Shepherd run 7.\n"; got != want {
		t.Errorf("got %q", got)
	}
	if AddFooter(got, w) != got {
		t.Error("footer added twice")
	}
	if AddFooter("x", Who{}) != "x" {
		t.Error("footer without a run")
	}
}

func TestApplySkipsAnAgentsOwnCoauthor(t *testing.T) {
	f := filepath.Join(t.TempDir(), "MSG")
	os.WriteFile(f, []byte("feat: x\n\nCo-Authored-By: Claude Opus 4 <noreply@anthropic.com>\n"), 0o644)
	if err := Apply(context.Background(), f, Who{Run: 3, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	if n := strings.Count(string(b), "Co-Authored-By"); n != 1 || !strings.Contains(string(b), "Shepherd-Run: 3") {
		t.Errorf("message = %q", b)
	}
}

// A later run that amends the commit adds its own run and model next to the first run's,
// and running the hook again for the same run adds nothing.
func TestApplyKeepsEveryRun(t *testing.T) {
	f := filepath.Join(t.TempDir(), "MSG")
	os.WriteFile(f, []byte("feat: x\n"), 0o644)
	ctx := context.Background()
	first := Who{Run: 7, Agent: "claude", Model: "opus"}
	later := Who{Run: 9, Agent: "codex", Model: "gpt-5.5"}
	for _, w := range []Who{first, later, later, first} {
		if err := Apply(ctx, f, w); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(f)
	got := string(b)
	for _, want := range []string{"Shepherd-Run: 7", "Shepherd-Run: 9", "Shepherd-Model: opus", "Shepherd-Model: gpt-5.5"} {
		if n := strings.Count(got, want); n != 1 {
			t.Errorf("%q appears %d times in %q", want, n, got)
		}
	}
	if n := strings.Count(got, "Co-Authored-By:"); n != 2 {
		t.Errorf("co-authors = %d in %q", n, got)
	}
}

func TestOwn(t *testing.T) {
	for line, want := range map[string]bool{
		"Shepherd-Run: 4": true,
		"Co-Authored-By: Claude <noreply@anthropic.com>": true,
		"Co-Authored-By: Ann <ann@example.com>":          false,
		"fix: co-authored-by in a subject":               false,
	} {
		if got := Own(line); got != want {
			t.Errorf("Own(%q) = %v", line, got)
		}
	}
}
