package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoRepo is returned by Resolve for a name that is no git repo under the root.
var ErrNoRepo = errors.New("no git repo by that name")

// AmbiguousError is returned by Resolve when a name matches more than one repo.
type AmbiguousError struct {
	Name       string
	Candidates []Found
}

func (e *AmbiguousError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		names[i] = c.Name
	}
	return fmt.Sprintf("%q is ambiguous: could be %s; name one by its path in the workspace", e.Name, strings.Join(names, ", "))
}

// Resolve finds the repo a name refers to under root. The name is a repo's path
// relative to the root ("group/lib"), or its last element ("lib") when only one repo
// is called that. The scan's findings come first; a repo that is below the scan's
// depth, or whose directory is named exactly, is read from disk. It never matches a
// linked worktree (a .git file) or a directory with no .git.
func Resolve(ctx context.Context, root, name string) (Found, error) {
	name = filepath.ToSlash(strings.Trim(filepath.ToSlash(name), "/"))
	if name == "" || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || name == ".." {
		return Found{}, ErrNoRepo
	}
	found, err := Scan(ctx, root)
	if err != nil {
		return Found{}, err
	}
	for _, f := range found {
		if f.Name == name {
			return f, nil
		}
	}
	if !strings.Contains(name, "/") {
		var hits []Found
		for _, f := range found {
			if filepath.Base(f.Name) == name {
				hits = append(hits, f)
			}
		}
		switch len(hits) {
		case 1:
			return hits[0], nil
		case 0:
		default:
			return Found{}, &AmbiguousError{Name: name, Candidates: hits}
		}
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	if fi, err := os.Lstat(filepath.Join(p, ".git")); err == nil && fi.IsDir() {
		f := inspect(ctx, filepath.FromSlash(name), p)
		f.Suggest = f.Remote != ""
		if !f.Suggest {
			f.Reason = "no origin remote"
		}
		return f, nil
	}
	return Found{}, ErrNoRepo
}
