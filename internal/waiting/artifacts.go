package waiting

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// MergedArtifactItems are the open lanes whose merge request merged but whose
// uncommitted files nobody decided: the automatic close
// refuses them, so the lane waits for the person to keep, commit or discard. Files in
// the Shepherd directory never hold a lane. workspaceID 0 is every workspace.
func MergedArtifactItems(ctx context.Context, st store.Store, workspaceID int64) ([]api.WaitingItem, error) {
	list, err := st.ListArtifacts(ctx, store.ArtifactFilter{WorkspaceID: workspaceID, State: store.ArtifactLive, Limit: 1000})
	if err != nil {
		return nil, err
	}
	byLane := map[int64][]store.Artifact{}
	var order []int64
	for _, a := range list {
		if a.Intent != store.ArtifactUndecided || a.Source == store.ArtifactCommitted || strings.HasPrefix(a.Path, store.ArtifactDir) {
			continue
		}
		if _, ok := byLane[a.LaneID]; !ok {
			order = append(order, a.LaneID)
		}
		byLane[a.LaneID] = append(byLane[a.LaneID], a)
	}
	var items []api.WaitingItem
	for _, id := range order {
		lane, err := st.Lane(ctx, id)
		if err != nil || lane.State != store.LaneOpen {
			continue
		}
		lf, err := st.LaneForge(ctx, id)
		if err != nil || !(strings.EqualFold(lf.MRState, "merged") || lf.MergeSHA != "") {
			continue
		}
		var repo string
		if rp, err := st.Repo(ctx, lane.RepoID); err == nil {
			repo = rp.Name
		}
		files := byLane[id]
		paths := make([]string, 0, len(files))
		for _, a := range files {
			paths = append(paths, a.Path)
		}
		if len(paths) > 5 {
			paths = append(paths[:5], fmt.Sprintf("and %d more", len(paths)-5))
		}
		since := lf.MRUpdated
		if since.IsZero() {
			since = files[0].Created
		}
		noun := "files"
		if len(files) == 1 {
			noun = "file"
		}
		items = append(items, api.WaitingItem{
			Kind: api.WaitingArtifact, Action: "open", Repo: repo, Lane: lane.Name, LaneID: lane.ID, MR: lf.MR,
			Title:  fmt.Sprintf("lane merged, %d %s to keep or discard", len(files), noun),
			Detail: fmt.Sprintf("lane %s merged but stays open for %d undecided %s: %s", lane.Name, len(files), noun, strings.Join(paths, ", ")),
			Since:  since,
		})
	}
	return items, nil
}
