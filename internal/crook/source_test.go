package crook

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const guard = "---\nkind: guardrail\nactions: [push]\nthen: %s\n---\nSays %s.\n"

func guardText(then string) string {
	return strings.ReplaceAll(guard, "%s", then)
}

// repoWith makes a repo whose base branch commits the given files.
func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "checkout", "-q", "-b", "base")
	for k, v := range files {
		write(t, dir, k, v)
	}
	write(t, dir, "README", "x")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestRepoLayerComesFromBaseBranchNotWorktree(t *testing.T) {
	dir := repoWith(t, map[string]string{".shepherd/rules/hold-push.md": guardText("refuse")})
	// A lane edits its own rule in the worktree, committed on its branch and also
	// uncommitted: neither may count.
	gitIn(t, dir, "checkout", "-q", "-b", "lane")
	write(t, dir, ".shepherd/rules/hold-push.md", guardText("ask"))
	gitIn(t, dir, "commit", "-q", "-am", "loosen")
	write(t, dir, ".shepherd/rules/hold-push.md", "---\ndisabled: true\n---\n")
	write(t, dir, ".shepherd/rules/extra.md", guardText("ask"))

	set := Load(context.Background(), Sources{Repo: &RepoSource{Dir: dir, Ref: "base"}})
	if len(set.Errors) != 0 {
		t.Fatal(set.Errors)
	}
	if len(set.Rules) != 1 || set.Rules[0].ID != "hold-push" || set.Rules[0].Then != Refuse || set.Rules[0].Layer != LayerRepo {
		t.Fatalf("got %+v", set.Rules)
	}
}

func TestRepoFolderIsConfigurable(t *testing.T) {
	dir := repoWith(t, map[string]string{
		".shepherd/rules/a.md": guardText("ask"),
		"policy/rules/b.md":    guardText("hold"),
	})
	l, err := ReadLayerGit(context.Background(), LayerRepo, dir, "base", "policy")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Files["b.md"]; !ok || len(l.Files) != 1 {
		t.Fatalf("got %v", l.Files)
	}
	l, err = ReadLayerGit(context.Background(), LayerRepo, dir, "base", "")
	if err != nil || len(l.Files) != 1 || l.Files["a.md"] == nil {
		t.Fatalf("default folder: %v %v", l.Files, err)
	}
	l, err = ReadLayerGit(context.Background(), LayerRepo, dir, "base", "nope/here")
	if err != nil || len(l.Files) != 0 {
		t.Fatalf("absent folder has no rules: %v %v", l.Files, err)
	}
}

func TestReadLayerGitRefusals(t *testing.T) {
	ctx := context.Background()
	dir := repoWith(t, map[string]string{"other/real.md": guardText("ask")})
	for _, folder := range []string{"/abs", "../up", "a//b", "a/./b", ".git", `a\b`, "x/.GIT"} {
		if _, err := ReadLayerGit(ctx, LayerRepo, dir, "base", folder); err == nil {
			t.Errorf("folder %q accepted", folder)
		}
	}
	for _, ref := range []string{"", "missing", "--output=x"} {
		if _, err := ReadLayerGit(ctx, LayerRepo, dir, ref, ""); err == nil {
			t.Errorf("ref %q accepted", ref)
		}
	}
	// A symlinked rules folder and symlinked rule files are not followed.
	if err := os.MkdirAll(filepath.Join(dir, ".shepherd"), 0o755); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("../other", filepath.Join(dir, ".shepherd", "rules")); err != nil {
		t.Skip("no symlinks here")
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "link")
	if _, err := ReadLayerGit(ctx, LayerRepo, dir, "HEAD", ""); err == nil {
		t.Error("symlinked rules folder accepted")
	}
}

func TestReadLayerGitSkipsSymlinkedFilesAndOtherFiles(t *testing.T) {
	dir := repoWith(t, map[string]string{
		".shepherd/rules/ok.md":    guardText("ask"),
		".shepherd/rules/note.txt": "x",
		"outside.md":               guardText("ask"),
	})
	if err := os.Symlink("../../outside.md", filepath.Join(dir, ".shepherd/rules/link.md")); err != nil {
		t.Skip("no symlinks here")
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "more")
	l, err := ReadLayerGit(context.Background(), LayerRepo, dir, "HEAD", "")
	if err != nil || len(l.Files) != 1 || l.Files["ok.md"] == nil {
		t.Fatalf("got %v %v", l.Files, err)
	}
}

func TestLoadLayersAndFailedLayerIsReported(t *testing.T) {
	ws, proj := t.TempDir(), t.TempDir()
	write(t, ws, "rules/a.md", guardText("refuse"))
	write(t, proj, "rules/a.md", guardText("ask"))
	repo := repoWith(t, map[string]string{".shepherd/rules/b.md": guardText("hold")})
	set := Load(context.Background(), Sources{Workspace: ws, Project: proj, Repo: &RepoSource{Dir: repo, Ref: "base"}})
	if len(set.Errors) != 0 || len(set.Rules) != 2 {
		t.Fatalf("%+v %v", set.Rules, set.Errors)
	}
	if len(set.Overrides) != 1 || !set.Overrides[0].Loosening {
		t.Fatalf("overrides %+v", set.Overrides)
	}
	set = Load(context.Background(), Sources{Workspace: ws, Repo: &RepoSource{Dir: repo, Ref: "gone"}})
	if len(set.Errors) != 1 || set.Errors[0].Layer != LayerRepo || len(set.Rules) != 1 {
		t.Fatalf("%+v %v", set.Rules, set.Errors)
	}
}

func TestViewByLayer(t *testing.T) {
	set := Merge(
		layer(LayerWorkspace, map[string]string{"a.md": guardText("refuse"), "b.md": guardText("ask")}),
		layer(LayerRepo, map[string]string{"a.md": guardText("ask"), "b.md": "---\ndisabled: true\n---\n", "bad.md": "nope"}),
	)
	v := set.View()
	if len(v.Layers) != len(LayerOrder) {
		t.Fatalf("layers %d", len(v.Layers))
	}
	if len(v.Layers[0].Rules) != 0 || len(v.Layers[1].Rules) != 0 {
		t.Fatalf("%+v", v.Layers)
	}
	r := v.Layers[3].Rules
	if len(r) != 1 || r[0].ID != "a" || r[0].Replaces != LayerWorkspace || !r[0].Loosening {
		t.Fatalf("%+v", r)
	}
	if len(v.Disabled) != 1 || len(v.Errors) != 1 {
		t.Fatalf("%+v", v)
	}
	txt := v.Text()
	for _, want := range []string{"workspace (0)", "  none", "repo (1)", "replaces workspace (loosens)", "switched off: b (workspace) by repo", "not used: repo/rules/bad.md"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text lacks %q:\n%s", want, txt)
		}
	}
}
