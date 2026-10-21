package mergerule

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/git"
)

// RulesDir is where a repo keeps its rule files.
const RulesDir = ".shepherd/rules"

// Rules is a rule set read from one commit of a base branch.
type Rules struct {
	// Commit is the base branch commit the files were read from.
	Commit string
	Set    crook.RuleSet
}

// Loader reads a repo's rules from its base branch, never from a worktree, so a lane
// cannot change the rules it is judged by. It keeps the last read per repo directory
// and reads again only when the base branch moves.
type Loader struct {
	mu    sync.Mutex
	cache map[string]*Rules
}

// Load returns the repo layer's rules at base (a ref such as origin/dev) in the repo at
// dir. A base with no rules folder gives an empty set. An unreadable ref is an error and
// means no rules: the repo stays person-merged.
func (l *Loader) Load(ctx context.Context, dir, base string) (*Rules, error) {
	commit, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("base branch %s: %w", base, err)
	}
	key := dir + "\x00" + base
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.cache[key]; r != nil && r.Commit == commit {
		return r, nil
	}
	files, err := readTree(ctx, dir, commit)
	if err != nil {
		return nil, err
	}
	layer, err := crook.ReadLayerFS(crook.LayerRepo, files)
	if err != nil {
		return nil, err
	}
	r := &Rules{Commit: commit, Set: crook.Merge(layer)}
	if l.cache == nil {
		l.cache = map[string]*Rules{}
	}
	l.cache[key] = r
	return r, nil
}

// readTree returns the regular files directly under the rules folder at commit as a
// layer folder (rules/<name>). Symlinks and nested folders are left out.
func readTree(ctx context.Context, dir, commit string) (fstest.MapFS, error) {
	out, err := git.Run(ctx, dir, "ls-tree", "-z", commit+":"+RulesDir)
	if err != nil {
		// No rules folder at this commit is not an error.
		if !git.Ok(ctx, dir, "cat-file", "-e", commit+":"+RulesDir) {
			return fstest.MapFS{}, nil
		}
		return nil, err
	}
	fsys := fstest.MapFS{}
	for _, ent := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(ent, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 3 || f[1] != "blob" || f[0] == "120000" || !strings.HasSuffix(name, ".md") {
			continue
		}
		body, err := git.Run(ctx, dir, "show", commit+":"+RulesDir+"/"+name)
		if err != nil {
			return nil, err
		}
		fsys["rules/"+name] = &fstest.MapFile{Data: []byte(body + "\n")}
	}
	return fsys, nil
}
