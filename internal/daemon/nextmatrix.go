package daemon

import (
	"context"
	"fmt"
	"sort"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/work"
)

// matrixRows reads the rows with status todo or blocked from every repo's docs/WORK.md
// on its base ref (the work page's read and cache) and shows them as read-only items.
// Nothing is stored. A row is left out when a stored item that is still live names the
// same lane in the same repo, by link or by hint, so the work shows once, as the item.
// A repo with no file, or that cannot be read, adds nothing.
func (s *Server) matrixRows(ctx context.Context, items []store.NextItem) []api.NextItemView {
	cfg := s.LiveConfig()
	named := map[string]bool{} // repo + "\x00" + lane name
	for _, it := range items {
		if it.State == store.NextDropped {
			continue
		}
		if it.Lane != "" {
			named[it.Repo+"\x00"+it.Lane] = true
		}
		if it.LaneID != 0 {
			if l, err := s.Store.Lane(ctx, it.LaneID); err == nil {
				named[it.Repo+"\x00"+l.Name] = true
				named[it.Repo+"\x00"+l.Branch] = true
			}
		}
	}
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return nil
	}
	var repos []store.Repo
	for _, ws := range wss {
		rs, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			continue
		}
		repos = append(repos, rs...)
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	var out []api.NextItemView
	seen := map[string]bool{}
	for _, rp := range repos {
		if seen[rp.Name] {
			continue
		}
		seen[rp.Name] = true
		_, _, res, found := readWork(ctx, rp.Path, cfg.Profile(rp.Name).BaseBranch)
		if !found {
			continue
		}
		remote := rp.Remote
		if remote == "" {
			remote, _ = git.Run(ctx, rp.Path, "remote", "get-url", "origin")
		}
		for _, r := range res.Rows {
			if r.Status != work.Todo && r.Status != work.Blocked {
				continue
			}
			if r.Lane != "" && named[rp.Name+"\x00"+r.Lane] {
				continue
			}
			v := api.NextItemView{
				Title: r.Item, Repo: rp.Name, Lane: r.Lane, Note: r.Notes,
				State: store.NextPlanned, Stored: store.NextPlanned, Source: api.NextSourceMatrix,
				Key: fmt.Sprintf("matrix:%s:%s", rp.Name, r.ID), ReadOnly: true,
				Area: r.Area, RowID: r.ID, PRURL: prURL(remote, r.PR),
			}
			if r.Status == work.Blocked {
				v.State, v.Stored = work.Blocked, work.Blocked
			}
			if r.PR != 0 {
				v.MR = fmt.Sprintf("#%d", r.PR)
			}
			out = append(out, v)
		}
	}
	return out
}
