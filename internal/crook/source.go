package crook

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

// DefaultRepoFolder is the layer folder a repo keeps its rules in, when config names none.
const DefaultRepoFolder = ".shepherd"

// Sources says where each layer's folder is. Folders come from config; nothing here is a
// hard-coded path except the default for the repo's.
type Sources struct {
	// Workspace and Project are directories on this machine; empty means no such layer.
	Workspace, Project string
	// Repo is the repo layer, read from a branch of a repo.
	Repo *RepoSource
}

// RepoSource is a repo's layer folder, read from a git ref and never from a working tree.
// A lane works in a worktree it can edit, so reading rules there would let a lane loosen
// its own rules; the ref is the repo's base branch.
type RepoSource struct {
	// Dir is any checkout of the repo; only its object store is used.
	Dir string
	// Ref is the base branch as a remote-tracking ref, such as origin/dev. It is
	// required. A local branch, tag or HEAD is refused as too easy to move by accident. A
	// remote-tracking ref shares the same ref store, so this is not a lock against a lane
	// that rewrites refs on purpose: pinning the base is the caller's job.
	Ref string
	// Folder is the layer folder inside the repo; empty means DefaultRepoFolder.
	Folder string
}

// ValidFolder reports whether s can name a layer folder inside a repo: a relative path of
// plain names, with no "..", no backslash and no empty parts.
func ValidFolder(s string) error {
	if s == "" || strings.HasPrefix(s, "/") || strings.Contains(s, `\`) {
		return fmt.Errorf("a layer folder is a relative path with forward slashes, got %q", s)
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return fmt.Errorf("a layer folder has no empty, . , .. or .git parts, got %q", s)
		}
	}
	return nil
}

// Load reads every layer named in src and merges them. A layer that cannot be read is
// reported in the result's Errors and contributes nothing; the others still apply.
func Load(ctx context.Context, src Sources) RuleSet {
	var layers []LayerFiles
	var failed []LoadError
	add := func(l LayerFiles, err error) {
		if err != nil {
			failed = append(failed, LoadError{Layer: l.Layer, File: "rules", Err: err})
			return
		}
		layers = append(layers, l)
	}
	if src.Workspace != "" {
		add(ReadLayerDir(LayerWorkspace, src.Workspace))
	}
	if src.Project != "" {
		add(ReadLayerDir(LayerProject, src.Project))
	}
	if r := src.Repo; r != nil {
		add(ReadLayerGit(ctx, LayerRepo, r.Dir, r.Ref, r.Folder))
	}
	set := Merge(layers...)
	set.Errors = append(failed, set.Errors...)
	return set
}

// ReadLayerGit reads <folder>/rules/*.md as committed at a remote-tracking ref in the repo at dir. It reads
// git objects only, so uncommitted edits and a lane's worktree have no say. A ref that
// does not resolve is an error (the layer is unknown, not empty); a folder that is absent
// at the ref has no rules. A symlink anywhere on the path is refused or skipped, as in
// ReadLayerFS.
func ReadLayerGit(ctx context.Context, layer, dir, ref, folder string) (LayerFiles, error) {
	out := LayerFiles{Layer: layer, Files: map[string][]byte{}}
	if folder == "" {
		folder = DefaultRepoFolder
	}
	if err := ValidFolder(folder); err != nil {
		return out, fmt.Errorf("%s layer: %w", layer, err)
	}
	if ref == "" || strings.HasPrefix(ref, "-") {
		return out, fmt.Errorf("%s layer: no base branch to read the rules from", layer)
	}
	full, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", "--symbolic-full-name", ref)
	if err != nil || full == "" {
		return out, fmt.Errorf("%s layer: cannot read the base branch %q", layer, ref)
	}
	if !strings.HasPrefix(full, "refs/remotes/") {
		return out, fmt.Errorf("%s layer: the base branch must be a remote-tracking ref such as origin/dev, not %q", layer, ref)
	}
	commit, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", full+"^{commit}")
	if err != nil || commit == "" {
		return out, fmt.Errorf("%s layer: cannot read the base branch %q", layer, ref)
	}
	// Every folder on the way to rules must be a real tree, not a symlink.
	parts := append(strings.Split(folder, "/"), "rules")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		ent, err := lsTree(ctx, dir, commit, p)
		if err != nil {
			return out, fmt.Errorf("%s layer: %w", layer, err)
		}
		if len(ent) == 0 {
			return out, nil // not there at that ref: no rules
		}
		if ent[0].mode != "040000" {
			return out, fmt.Errorf("%s layer: %s is not a plain folder (a symlink or a file)", layer, p)
		}
	}
	ents, err := lsTree(ctx, dir, commit, strings.Join(parts, "/")+"/")
	if err != nil {
		return out, fmt.Errorf("%s layer: %w", layer, err)
	}
	for _, e := range ents {
		name := path.Base(e.path)
		// Plain files only, as in ReadLayerFS.
		if (e.mode != "100644" && e.mode != "100755") || path.Ext(name) != ".md" {
			continue
		}
		b, err := git.Run(ctx, dir, "cat-file", "blob", e.sha)
		if err != nil {
			return out, fmt.Errorf("%s layer: %w", layer, err)
		}
		out.Files[name] = []byte(b + "\n")
	}
	return out, nil
}

type treeEntry struct{ mode, sha, path string }

// lsTree lists the entries at p in a commit; a path ending in "/" lists that folder.
func lsTree(ctx context.Context, dir, commit, p string) ([]treeEntry, error) {
	s, err := git.Run(ctx, dir, "ls-tree", "-z", commit, "--", p)
	if err != nil {
		return nil, err
	}
	var out []treeEntry
	for _, rec := range strings.Split(s, "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode, type, sha
		if len(f) != 3 {
			continue
		}
		out = append(out, treeEntry{f[0], f[2], name})
	}
	return out, nil
}
