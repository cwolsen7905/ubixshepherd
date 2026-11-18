// Package project reads a project's approved brief from a file kept in a repo.
package project

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
)

// ErrMissing means there is no brief to read: the repo, the ref or the file is not
// there. The caller falls back to the store's brief.
var ErrMissing = errors.New("no brief file")

// Brief is a brief read from a file.
type Brief struct {
	Text string
	// Approved is when the commit that last changed the file landed.
	Approved time.Time
	// Commit is the commit that last changed the file.
	Commit string
	// Ref is the ref it was read at, such as origin/main.
	Ref string
}

// Reader reads brief files and remembers each for a short time. Reads are of the ref's
// committed tip, never the working tree, so an edit not yet merged is never approved.
type Reader struct {
	// TTL is how long a read is reused; zero is DefaultTTL.
	TTL time.Duration
	Now func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

// DefaultTTL is how long a read is reused.
const DefaultTTL = 60 * time.Second

type cached struct {
	at  time.Time
	b   Brief
	err error
}

// Read returns the brief at file in the repo at dir, at ref ("" is the default branch).
func (r *Reader) Read(ctx context.Context, dir, ref, file string) (Brief, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	ttl := r.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	key := dir + "\x00" + ref + "\x00" + file
	r.mu.Lock()
	c, ok := r.cache[key]
	r.mu.Unlock()
	if ok && now().Sub(c.at) < ttl {
		return c.b, c.err
	}
	b, err := read(ctx, dir, ref, file)
	if err != nil && !errors.Is(err, ErrMissing) {
		return Brief{}, err // not cached: a hiccup should not stick
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[key] = cached{at: now(), b: b, err: err}
	r.mu.Unlock()
	return b, err
}

// DefaultRef finds the tip of the repo's default branch: the remote's HEAD when known,
// else main or master, remote-tracking first.
func DefaultRef(ctx context.Context, dir string) (string, error) {
	if out, err := git.Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out, nil
	}
	for _, c := range []string{"origin/main", "origin/master", "main", "master"} {
		if git.RefExists(ctx, dir, c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w: no default branch in %s", ErrMissing, dir)
}

func read(ctx context.Context, dir, ref, file string) (Brief, error) {
	if dir == "" || !git.Ok(ctx, dir, "rev-parse", "--git-dir") {
		return Brief{}, fmt.Errorf("%w: %s is not a git repo", ErrMissing, dir)
	}
	if ref == "" {
		var err error
		if ref, err = DefaultRef(ctx, dir); err != nil {
			return Brief{}, err
		}
	} else if !git.RefExists(ctx, dir, ref) {
		return Brief{}, fmt.Errorf("%w: no ref %s", ErrMissing, ref)
	}
	if _, err := git.Run(ctx, dir, "cat-file", "-e", ref+":"+file); err != nil {
		return Brief{}, fmt.Errorf("%w: %s at %s", ErrMissing, file, ref)
	}
	text, err := git.Run(ctx, dir, "show", ref+":"+file)
	if err != nil {
		return Brief{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Brief{}, fmt.Errorf("%w: %s is empty", ErrMissing, file)
	}
	out, err := git.Run(ctx, dir, "log", "-1", "--format=%H %ct", ref, "--", file)
	if err != nil {
		return Brief{}, err
	}
	sha, ts, _ := strings.Cut(out, " ")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sha == "" {
		return Brief{}, fmt.Errorf("%w: no history for %s at %s", ErrMissing, file, ref)
	}
	return Brief{Text: text, Approved: time.Unix(sec, 0).UTC(), Commit: sha, Ref: ref}, nil
}

// ForProject reads the brief a project's brief_source names, in the repo at dir.
func (r *Reader) ForProject(ctx context.Context, name string, src config.BriefSource, dir string) (Brief, error) {
	return r.Read(ctx, dir, src.Ref, src.FilePath(name))
}
