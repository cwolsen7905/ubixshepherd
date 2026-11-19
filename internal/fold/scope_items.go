package fold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// itemScopeWarnings names the approved next-up items of the repo that have no lane yet
// and whose plan doc's "Scope:" line overlaps the new lane's scope: opening this lane
// would hold their paths. It warns, never refuses. An item with no plan doc, a plan that
// cannot be read or one with no "Scope:" line carries no scope data and is not judged.
// The item the lane is opened for (forItem, or an item whose lane hint is the lane's
// name) is not a conflict.
func (f *Fold) itemScopeWarnings(ctx context.Context, repo store.Repo, laneName string, forItem int64, laneScope, files []string, protected []string) []string {
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
		planned := planScopeOf(repo.Path, it.Plan)
		if len(planned) == 0 {
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

// planScopeOf reads the "Scope:" line of a plan doc under the repo; nil when there is
// none or the doc cannot be read.
func planScopeOf(repoPath, plan string) []string {
	clean := filepath.Clean(plan)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(repoPath, clean))
	if err != nil {
		return nil
	}
	return scope.PlanScope(string(b))
}
