package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCommitHook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ok, err := installCommitHook(ctx, f.repo.Path, "/x/shepherd")
	if err != nil || !ok {
		t.Fatalf("install: %v %v", ok, err)
	}
	st, _ := Hook(ctx, f.repo.Path)
	p := filepath.Join(filepath.Dir(st.Path), "prepare-commit-msg")
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "hook prepare-commit-msg") {
		t.Errorf("hook body:\n%s", b)
	}
	os.WriteFile(p, []byte("#!/bin/sh\necho mine\n"), 0o755)
	if ok, err := installCommitHook(ctx, f.repo.Path, "x"); ok || err != nil {
		t.Errorf("replaced a foreign hook: %v %v", ok, err)
	}
}

func TestForbidLeavesShepherdsTrailersAlone(t *testing.T) {
	msg := "feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\nShepherd-Run: 5\nCo-Authored-By: Ann <ann@example.com>"
	got := withoutOwnTrailers(msg)
	if strings.Contains(got, "anthropic") || strings.Contains(got, "Shepherd-Run") || !strings.Contains(got, "Ann") {
		t.Errorf("got %q", got)
	}
}
