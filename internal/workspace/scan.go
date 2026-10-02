// Package workspace finds the git repos under a directory and suggests which ones
// Shepherd should manage. Repos are opt-in: a directory of projects usually holds
// scratch repos and deliberate duplicate working copies, so the scan suggests and the
// human decides.
package workspace

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// MaxDepth is how far below the workspace root the scan looks for repos.
const MaxDepth = 3

// Found is a repo the scan found.
type Found struct {
	// Name is the path relative to the root, with forward slashes.
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Remote string   `json:"remote,omitempty"`
	Stacks []string `json:"stacks,omitempty"`
	// Suggest is the scan's recommendation; Reason says why when it is false.
	Suggest bool   `json:"suggest"`
	Reason  string `json:"reason,omitempty"`
}

// markers map a file in a repo's root to a generic stack name. Packs are chosen from
// these; nothing here knows any product.
var markers = []struct{ file, stack string }{
	{"go.mod", "go"},
	{"package.json", "node"},
	{"composer.json", "php"},
	{"Cargo.toml", "rust"},
	{"pyproject.toml", "python"},
	{"project.godot", "godot"},
	{"Dockerfile", "docker"},
	{"Makefile", "make"},
}

var skipDirs = map[string]bool{"node_modules": true, "vendor": true}

// Scan walks root to MaxDepth and returns the repos below it, sorted by name. A
// directory with a .git directory is a repo and is not descended into; one whose .git
// is a file is a linked worktree or submodule and is skipped.
func Scan(ctx context.Context, root string) ([]Found, error) {
	var found []Found
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return fs.SkipDir // unreadable: leave it
		}
		if !d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if p != root && (strings.HasPrefix(name, ".") || skipDirs[name] || strings.HasSuffix(name, "-worktrees")) {
			return fs.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if fi, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			if fi.IsDir() && p != root {
				found = append(found, inspect(ctx, rel, p))
			}
			if p != root {
				return fs.SkipDir
			}
		}
		if depth >= MaxDepth {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b Found) int { return strings.Compare(a.Name, b.Name) })
	suggest(found)
	return found, nil
}

func inspect(ctx context.Context, rel, p string) Found {
	f := Found{Name: filepath.ToSlash(rel), Path: p}
	out, err := exec.CommandContext(ctx, "git", "-C", p, "config", "--get", "remote.origin.url").Output()
	if err == nil {
		f.Remote = strings.TrimSpace(string(out))
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(p, m.file)); err == nil {
			f.Stacks = append(f.Stacks, m.stack)
		}
	}
	return f
}

// suggest marks repos with an origin remote, except a second working copy of a remote
// already seen.
func suggest(found []Found) {
	first := map[string]string{}
	for i := range found {
		f := &found[i]
		switch key := NormalizeRemote(f.Remote); {
		case key == "":
			f.Reason = "no origin remote"
		case first[key] != "":
			f.Reason = "same remote as " + first[key]
		default:
			first[key] = f.Name
			f.Suggest = true
		}
	}
}

// NormalizeRemote reduces a remote URL to host/path, so the SSH and HTTPS forms of one
// repo compare equal.
func NormalizeRemote(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if at := strings.LastIndex(u, "@"); at >= 0 && at < strings.Index(u+"/", "/") {
			u = u[at+1:]
		}
		if slash := strings.Index(u, "/"); slash >= 0 {
			host := u[:slash]
			if colon := strings.Index(host, ":"); colon >= 0 {
				host = host[:colon] // drop a port
			}
			u = host + u[slash:]
		}
	} else if at := strings.Index(u, "@"); at >= 0 {
		u = strings.Replace(u[at+1:], ":", "/", 1) // scp-like user@host:path
	}
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	return strings.ToLower(u)
}
