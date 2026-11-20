package fold

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// itemScopeWarnings names the approved next-up items of the repo that have no lane yet
// and whose plan doc's "Scope:" line overlaps the new lane's scope: opening this lane
// would hold their paths. It warns, never refuses. An item with no plan doc, a plan that
// cannot be read or one with no "Scope:" line carries no scope data and is not judged.
// The item the lane is opened for (forItem, an item whose lane hint is the lane's name,
// or one whose plan scope the lane takes exactly) is not a conflict.
func (f *Fold) itemScopeWarnings(ctx context.Context, repo store.Repo, start, laneName string, forItem int64, laneScope, files []string, protected []string) []string {
	items, err := f.Store.NextItems(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		if it.State != store.NextApproved || it.LaneID != 0 || it.Repo != repo.Name || it.Plan == "" {
			continue
		}
		if it.ID == forItem || (it.Lane != "" && it.Lane == laneName) {
			continue
		}
		planned := planScopeAt(ctx, repo.Path, start, it.Plan)
		if len(planned) == 0 {
			continue
		}
		if sameGlobs(laneScope, planned) {
			// A lane that takes exactly what the plan names is the item's own lane, as
			// the keep-going drive opens it: it is not blocking the item.
			continue
		}
		var pairs []string
		for _, g := range laneScope {
			for _, p := range planned {
				if len(scope.OverlapWithout([]string{g}, []string{p}, files, protected)) > 0 {
					pairs = append(pairs, fmt.Sprintf("%s (item %s)", g, p))
				}
			}
		}
		if len(pairs) > 0 {
			out = append(out, fmt.Sprintf("warning: this scope would block approved next-up item %d %q, which has no lane yet: its plan's scope overlaps on %s. Narrow it (shepherd lane narrow) if the item should start first",
				it.ID, it.Title, firstN(pairs, 5)))
		}
	}
	return out
}

func sameGlobs(a, b []string) bool {
	set := map[string]bool{}
	for _, g := range a {
		set[g] = true
	}
	other := map[string]bool{}
	for _, g := range b {
		if !set[g] {
			return false
		}
		other[g] = true
	}
	return len(set) == len(other)
}

// planScopeAt reads the "Scope:" line of a plan doc as committed at the lane's starting
// point, like everything else the open judges, so a working-copy edit or a symlink in
// the checkout changes nothing. nil when there is none or the doc is not at that ref.
func planScopeAt(ctx context.Context, repoPath, start, plan string) []string {
	clean := path.Clean(filepath.ToSlash(plan))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil
	}
	text, err := git.Run(ctx, repoPath, "show", start+":"+clean)
	if err != nil {
		return nil
	}
	return scope.PlanScope(text)
}
