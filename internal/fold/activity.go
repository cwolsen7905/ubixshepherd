package fold

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// CommitClock tells when each lane last committed to its branch. It reads the repo's
// refs once per call and runs git log only for a lane whose branch or base moved since
// it last looked, so listing lanes stays cheap. The zero value is ready; it keeps
// nothing on disk, so a restart looks again.
type CommitClock struct {
	mu    sync.Mutex
	known map[int64]commitSeen
}

type commitSeen struct {
	tip, base string
	at        time.Time
}

// Times returns the committer time of each lane's newest commit beyond its base, for the
// lanes that have one. A lane with none (a fresh branch, one fully merged into its base,
// a ref git cannot find) is left out: it is not active because its base moved.
func (c *CommitClock) Times(ctx context.Context, repoPath string, lanes []store.Lane) map[int64]time.Time {
	out := map[int64]time.Time{}
	if len(lanes) == 0 {
		return out
	}
	refs, err := git.Run(ctx, repoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/remotes")
	if err != nil {
		return out
	}
	sha := map[string]string{}
	for _, line := range strings.Split(refs, "\n") {
		if name, id, ok := strings.Cut(line, " "); ok {
			sha[name] = id
		}
	}
	// A lane's refs may be local or only on the remote: the local one wins.
	find := func(branch string) (ref, id string) {
		for _, r := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
			if v, ok := sha[r]; ok {
				return r, v
			}
		}
		return "", ""
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.known == nil {
		c.known = map[int64]commitSeen{}
	}
	for _, l := range lanes {
		tipRef, tip := find(l.Branch)
		baseRef, base := find(l.Base)
		if tip == "" || base == "" {
			continue
		}
		seen, ok := c.known[l.ID]
		if !ok || seen.tip != tip || seen.base != base {
			seen = commitSeen{tip: tip, base: base}
			if tip != base {
				if v, err := git.Run(ctx, repoPath, "log", "-1", "--format=%cI", baseRef+".."+tipRef); err == nil && v != "" {
					if t, err := time.Parse(time.RFC3339, v); err == nil {
						seen.at = t.UTC()
					}
				}
			}
			c.known[l.ID] = seen
		}
		if !seen.at.IsZero() {
			out[l.ID] = seen.at
		}
	}
	return out
}
