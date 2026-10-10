package dispatch

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/standards"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// StandardsDir is where Shepherd's own packs are: next to the run logs, under the home.
func (r *Runner) StandardsDir() string {
	if r.Standards != "" {
		return r.Standards
	}
	return filepath.Join(filepath.Dir(r.Dir), "standards")
}

// resolveStandards is the repo's effective rules in a lane: Shepherd's packs its profile
// names, under the repo's own pack directory, read from the lane's worktree.
func (r *Runner) resolveStandards(prof config.Profile, lane store.Lane) standards.Set {
	if len(prof.Standards) == 0 && prof.StandardsPackDir == "" {
		return standards.Set{}
	}
	repoDir := ""
	if prof.StandardsPackDir != "" {
		repoDir = filepath.Join(lane.Worktree, filepath.FromSlash(prof.StandardsPackDir))
	}
	return standards.Resolve(r.StandardsDir(), prof.Standards, repoDir)
}

// standardsDigest is the section of the brief that carries the repo's standards: "" when
// the repo uses no pack and names no docs of its own.
func (r *Runner) standardsDigest(ctx context.Context, prof config.Profile, lane store.Lane) string {
	set := r.resolveStandards(prof, lane)
	for _, p := range set.Problems {
		r.Log.Warn("standards", "lane", lane.Name, "problem", p)
	}
	docs := standards.ExistingDocs(lane.Worktree, prof.StandardsDocs)
	files := trackedFiles(ctx, lane)
	l := standards.Lane{Scope: lane.Scope, Files: files, Tags: prof.StandardsWhen}
	return standards.Digest(set, l, standards.DigestOptions{Docs: docs, RepoPackDir: prof.StandardsPackDir})
}

func trackedFiles(ctx context.Context, lane store.Lane) []string {
	out, err := git.Run(ctx, lane.Worktree, "ls-files")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// standardsChecks runs the checks of the lane's rules whose files the lane's diff
// touches. It reports whether a must rule's check failed, and the failures by rule id.
// A should rule's failure goes to the feed and does not block.
func (r *Runner) standardsChecks(ctx context.Context, lane store.Lane, prof config.Profile) (bool, string) {
	set := r.resolveStandards(prof, lane)
	rules := standards.Checked(set, changedFiles(ctx, lane))
	if len(rules) == 0 {
		return false, ""
	}
	r.feed(ctx, store.FeedGate, lane.ID, "running %d standards check(s) in lane %s", len(rules), lane.Name)
	var blocking []string
	for _, res := range standards.RunChecks(ctx, lane.Worktree, baseRef(ctx, lane), rules, GateTimeout) {
		if res.OK {
			continue
		}
		msg := fmt.Sprintf("standard %s (%s) failed: `%s`\n%s", res.Rule.ID, res.Rule.Level, res.Rule.Check, res.Output)
		if res.Blocks() {
			blocking = append(blocking, msg)
			continue
		}
		r.feed(ctx, store.FeedGate, lane.ID, "should-rule %s: check failed in lane %s (not blocking): %s", res.Rule.ID, lane.Name, clip(res.Output, 200))
	}
	if len(blocking) == 0 {
		return false, ""
	}
	return true, strings.Join(blocking, "\n\n")
}
