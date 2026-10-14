package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/forge"
)

func TestLinksThread(t *testing.T) {
	h := newHarness(t, "", false)
	root, app := laneWorkspace(t)
	if code := h.run("init", root, "--yes"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	h.env.Cwd = app
	old := linkForge
	defer func() { linkForge = old }()
	linkForge = func(string) (forge.Linker, error) {
		return &forge.GitLab{Host: "gl.example.com", Project: "g/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
			if !strings.HasSuffix(args[len(args)-1], "merge_requests/7/discussions/a34c00ff") {
				t.Errorf("args = %v", args)
			}
			return []byte(`{"id":"a34c00ff","notes":[{"id":901}]}`), nil
		}}, nil
	}
	if code := h.run("links", "thread", "7", "a34c00ff"); code != 0 ||
		strings.TrimSpace(h.out.String()) != "https://gl.example.com/g/app/-/merge_requests/7#note_901" {
		t.Errorf("thread: %d %q %s", code, h.out, h.err)
	}
	h.out.Reset()
	if code := h.run("links", "pipeline", "12"); code != 0 ||
		strings.TrimSpace(h.out.String()) != "https://gl.example.com/g/app/-/pipelines/12" {
		t.Errorf("pipeline: %d %q", code, h.out)
	}
	if code := h.run("links", "thread", "7"); code == 0 {
		t.Error("thread without a discussion accepted")
	}
}
