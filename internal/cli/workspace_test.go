package cli

import (
	"strings"
	"testing"
)

func TestWorkspaceRename(t *testing.T) {
	h := newHarness(t, "", false)
	root := makeWorkspace(t)
	if code := h.run("init", root, "--yes", "--name", "alpha"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	// By name.
	if code := h.run("workspace", "rename", "alpha", "beta"); code != 0 {
		t.Fatalf("rename: %s", h.err)
	}
	if !strings.Contains(h.out.String(), "Renamed workspace alpha to beta") || !strings.Contains(h.out.String(), "hosts.yaml") {
		t.Errorf("output = %q", h.out)
	}
	wss, err := h.srv.Store.Workspaces(t.Context())
	if err != nil || len(wss) != 1 || wss[0].Name != "beta" || wss[0].Path != root {
		t.Fatalf("workspaces = %+v, %v", wss, err)
	}
	repos, _ := h.srv.Store.Repos(t.Context(), wss[0].ID)
	if len(repos) == 0 {
		t.Error("repos lost on rename")
	}
	// By path, to the same name: nothing to do.
	if code := h.run("workspace", "rename", root, "beta"); code != 0 || !strings.Contains(h.out.String(), "already named") {
		t.Errorf("same name: %d %s %s", code, h.out, h.err)
	}
	// By path.
	if code := h.run("workspace", "rename", root, "gamma"); code != 0 {
		t.Fatalf("rename by path: %s", h.err)
	}
	// Refusals.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"nope", "x"}, `no workspace "nope"`},
		{[]string{"gamma", " "}, "non-empty"},
		{[]string{"gamma", "a/b"}, "no slashes"},
		{[]string{"gamma"}, ""},
	} {
		code := h.run(append([]string{"workspace", "rename"}, c.args...)...)
		if code == 0 || !strings.Contains(h.err.String(), c.want) {
			t.Errorf("%v: code %d, stderr %q", c.args, code, h.err)
		}
	}
}

func TestWorkspaceRenameTakenName(t *testing.T) {
	h := newHarness(t, "", false)
	a, b := makeWorkspace(t), makeWorkspace(t)
	if code := h.run("init", a, "--yes", "--name", "one"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	if code := h.run("init", b, "--yes", "--name", "two"); code != 0 {
		t.Fatalf("init: %s", h.err)
	}
	if code := h.run("workspace", "rename", "two", "one"); code == 0 || !strings.Contains(h.err.String(), "already used") {
		t.Errorf("taken name: %d %s", code, h.err)
	}
}
