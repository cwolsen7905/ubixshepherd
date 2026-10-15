package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// pushTags pushes the release tags a lane's agent reserved and created, for repos where
// agents cannot push (autonomy.push: shepherd) and autonomy.tag is agent. Agents never
// tag unasked, so it pushes only tags that are reserved for this lane, exist locally,
// sit on the base branch at origin (and contain the lane's merge, once it has merged),
// and are not already on the remote at another commit. It never forces and never
// creates a tag. Each refusal and failure is told to the feed; the reservation stays
// reserved so a later ship retries. It returns what it said, for Ship.
func (r *Runner) pushTags(ctx context.Context, lane store.Lane, repo store.Repo, prof config.Profile) []string {
	if prof.Autonomy.Push != config.Shepherd || prof.Autonomy.Tag != config.Agent || prof.Tags != config.TagsReserved {
		return nil
	}
	live, err := r.Store.Reservations(ctx, repo.ID)
	if err != nil {
		return nil
	}
	var said []string
	say := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		said = append(said, msg)
		r.feed(ctx, store.FeedGate, lane.ID, "%s", msg)
	}
	for _, res := range live {
		if res.LaneID != lane.ID || res.State != store.TagReserved {
			continue
		}
		if _, ok := fold.ParseTag(prof.TagPrefix, res.Tag); !ok {
			continue
		}
		ref := "refs/tags/" + res.Tag
		sha, err := git.Run(ctx, lane.Worktree, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err != nil || sha == "" {
			continue // not created yet: the agent's to do
		}
		if why := r.tagProblem(ctx, lane, sha); why != "" {
			say("not pushing tag %s for lane %s: %s", res.Tag, lane.Name, why)
			continue
		}
		have, err := remoteTag(ctx, lane.Worktree, ref)
		if err != nil {
			say("not pushing tag %s for lane %s: could not read origin's tags: %s", res.Tag, lane.Name, clip(err.Error(), 200))
			continue
		}
		if len(have) > 0 {
			obj, _ := git.Run(ctx, lane.Worktree, "rev-parse", "--verify", ref)
			same := false
			for _, h := range have {
				same = same || h == sha || h == obj
			}
			if !same {
				say("not pushing tag %s for lane %s: origin already has it at a different commit", res.Tag, lane.Name)
				continue
			}
		} else if _, err := git.Run(ctx, lane.Worktree, "push", "--quiet", "origin", ref+":"+ref); err != nil {
			r.diagnoseLane(ctx, lane, store.Diagnosis{Kind: store.DiagPushRefused, Evidence: err.Error(),
				Summary: "tag " + res.Tag + " was not pushed: " + clip(err.Error(), 160)})
			say("pushing tag %s for lane %s failed: %s", res.Tag, lane.Name, clip(err.Error(), 300))
			continue
		}
		state := store.TagPushed
		if lf, err := r.Store.LaneForge(ctx, lane.ID); err == nil && lf.MergeSHA != "" {
			state = store.TagVerified
		}
		r.Store.SetReservation(ctx, res.ID, state, sha)
		say("pushed tag %s (%s) for lane %s", res.Tag, shortSHA(sha), lane.Name)
	}
	return said
}

// tagProblem says why a tag at sha may not be pushed for the lane, or "".
func (r *Runner) tagProblem(ctx context.Context, lane store.Lane, sha string) string {
	git.Run(ctx, lane.Worktree, "fetch", "--quiet", "origin")
	base := "origin/" + lane.Base
	if !git.RefExists(ctx, lane.Worktree, "refs/remotes/"+base) {
		return "origin has no " + lane.Base + " branch to check it against"
	}
	if !git.Ok(ctx, lane.Worktree, "merge-base", "--is-ancestor", sha, base) {
		return fmt.Sprintf("%s is not on %s at origin yet; tag the commit once the lane's work has merged", shortSHA(sha), base)
	}
	if lf, err := r.Store.LaneForge(ctx, lane.ID); err == nil && lf.MergeSHA != "" {
		if !git.Ok(ctx, lane.Worktree, "merge-base", "--is-ancestor", lf.MergeSHA, sha) {
			return fmt.Sprintf("it does not contain the lane's merge (%s): a release cut there would ship without the work", shortSHA(lf.MergeSHA))
		}
	}
	return ""
}

// remoteTag lists the object ids origin has for a tag ref (the tag and its peeled commit).
func remoteTag(ctx context.Context, dir, ref string) ([]string, error) {
	out, err := git.Run(ctx, dir, "ls-remote", "origin", ref, ref+"^{}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if id, _, ok := strings.Cut(line, "\t"); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
