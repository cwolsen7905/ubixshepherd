package watch

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// mergeTry is the head a merge was last attempted or refused at, so one head is not
// retried every poll.
type mergeTry struct {
	iid int
	sha string
	// retried is the pipeline whose require-approval job was retried at this head, or 0
	// for a merge attempt.
	retried int64
}

// mergeAfterApproval merges the lane's open request for a repo set to
// autonomy.merge: after_approval, once every condition holds on the request's current
// head: a person other than the author (and not a bot) approved after the head was
// pushed, the head pipeline passed on that head, no review thread is open, the request is
// not a draft and the forge calls it mergeable. Anything unmet is waited on and logged at
// debug. It never approves. The forge may keep approvals across pushes, so the approval's
// time is checked against the head's push; the merge call also carries the head it
// checked, so the forge refuses if the head moved. The merge is seen on the next poll,
// which closes the lane as for any merge. When the only red is a require-approval job that
// ran before the approval, it first retries that job (retryRequireApproval) and merges on
// a later poll, once the pipeline is green. The merge squashes only if the forge implements SquashMerger; GitLab's does not yet.
func (w *Watcher) mergeAfterApproval(ctx context.Context, f forge.Forge, repo string, lane store.Lane, mr *forge.MR) {
	prof := w.conf().Profile(repo)
	if prof.Autonomy.Merge != config.AfterApproval || mr.State != "opened" {
		return
	}
	if w.mergeTried[lane.ID] == (mergeTry{iid: mr.IID, sha: mr.SHA}) {
		return
	}
	who, why := w.mergeWait(ctx, f, prof, mr)
	if why == waitRequireApproval {
		w.retryRequireApproval(ctx, f, lane, mr, who)
		return
	}
	if why != "" {
		w.Log.Debug("watch: merge waits", "lane", lane.Name, "mr", mr.IID, "why", why)
		return
	}
	m := f.(forge.Merger)
	// Record the try first: a merge the forge accepts but still shows open (it merges
	// asynchronously) must not be sent again every poll. Only an unreachable forge
	// clears it.
	if w.mergeTried == nil {
		w.mergeTried = map[int64]mergeTry{}
	}
	w.mergeTried[lane.ID] = mergeTry{iid: mr.IID, sha: mr.SHA}
	if err := mergeSquashed(ctx, m, mr.IID, mr.SHA); err != nil {
		if forge.Unreachable(err) {
			delete(w.mergeTried, lane.ID)
			return
		}
		w.feed(ctx, store.FeedMR, lane.ID, "!%d is approved by %s but the forge refused the merge at %s: %s; lane %s waits for the person",
			mr.IID, strings.Join(who, ", "), short(mr.SHA), firstLine(err.Error()), lane.Name)
		return
	}
	w.feed(ctx, store.FeedMR, lane.ID, "!%d merged by autonomy.merge: after_approval, approved by %s at %s (lane %s)",
		mr.IID, strings.Join(who, ", "), short(mr.SHA), lane.Name)
}

// mergeWait says why the request cannot be merged yet, or "" when it can, with the
// people whose approval counts. It reads the forge only for what the request itself does
// not carry: approvals and open threads.
func (w *Watcher) mergeWait(ctx context.Context, f forge.Forge, prof config.Profile, mr *forge.MR) (who []string, why string) {
	ap, canApprove := f.(forge.Approver)
	_, canMerge := f.(forge.Merger)
	ha, canHead := f.(forge.HeadApprover)
	tr, canThreads := f.(forge.ThreadReader)
	switch {
	case !canApprove || !canMerge || !canThreads || !canHead:
		return nil, "the forge cannot read approvals and threads or merge"
	case mr.SHA == "":
		return nil, "the forge did not name the head"
	case mr.Draft:
		return nil, "draft"
	case mr.Author == "":
		return nil, "the forge did not name the author"
	case mr.Pipeline == nil:
		return nil, "no passed head pipeline"
	case mr.Pipeline.SHA != mr.SHA:
		return nil, "the pipeline is not for the head"
	}
	a, err := ap.Approvals(ctx, mr.IID)
	if err != nil {
		return nil, fmt.Sprintf("approvals unreadable: %v", err)
	}
	who = a.PersonApprovers(mr.Author, prof.Autonomy.MergeBots)
	if len(who) == 0 {
		return nil, "no approval by a person other than the author"
	}
	// An approval counts only if given after the head was pushed: the forge may keep
	// approvals across pushes, and the person never saw code pushed after theirs.
	if who, err = ha.ApprovedHead(ctx, mr.IID, mr.SHA, who); err != nil {
		return nil, fmt.Sprintf("approval time unreadable: %v", err)
	}
	if len(who) == 0 {
		return nil, "no person approved the current head"
	}
	ts, err := tr.Threads(ctx, mr.IID)
	if err != nil {
		return nil, fmt.Sprintf("threads unreadable: %v", err)
	}
	if len(ts) > 0 {
		return nil, fmt.Sprintf("%d review threads open", len(ts))
	}
	// A pipeline that failed only because require-approval ran before the approval is
	// the one red the person's approval clears: the caller retries that job. The forge
	// words its status for a red pipeline in several ways (ci_must_pass, blocked), so only
	// the statuses a retry cannot help are refused here; the job checks do the rest.
	if mr.Pipeline.Status == "failed" {
		switch st := mr.MergeStatus; st {
		case "need_rebase", "conflict", "draft_status":
			return nil, "the forge says " + st
		}
		return who, waitRequireApproval
	}
	switch {
	case mr.MergeStatus != "" && mr.MergeStatus != "mergeable":
		return nil, "the forge says " + mr.MergeStatus
	case mr.Pipeline.Status != "success":
		return nil, "no passed head pipeline"
	}
	return who, ""
}

// waitRequireApproval is mergeWait's answer when everything but a red head pipeline holds.
const waitRequireApproval = "the head pipeline failed"

// retryRequireApproval retries the require-approval job of a red head pipeline, when that
// job is the only one that failed and the person's approval of this head is in hand (the
// caller has checked approval and threads). It is Shepherd's own act on the person's
// approval, not an agent arming a merge. Once per pipeline; the next poll merges if the
// pipeline turns green.
func (w *Watcher) retryRequireApproval(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, who []string) {
	ap := f.(forge.Approver)
	key := mergeTry{iid: mr.IID, sha: mr.SHA, retried: mr.Pipeline.ID}
	if w.mergeTried[lane.ID] == key {
		return
	}
	failed, err := f.FailedJobs(ctx, mr.Pipeline.ID)
	if err != nil {
		w.Log.Debug("watch: merge waits", "lane", lane.Name, "mr", mr.IID, "why", fmt.Sprintf("failed jobs unreadable: %v", err))
		return
	}
	if len(failed) != 1 || failed[0].Name != forge.RequireApprovalJob {
		w.Log.Debug("watch: merge waits", "lane", lane.Name, "mr", mr.IID, "why", "a job other than "+forge.RequireApprovalJob+" failed")
		return
	}
	job, err := ap.JobByName(ctx, mr.Pipeline.ID, forge.RequireApprovalJob)
	if err != nil || job == nil || job.Status != "failed" {
		w.Log.Debug("watch: merge waits", "lane", lane.Name, "mr", mr.IID, "why", "no failed "+forge.RequireApprovalJob+" job to retry", "err", err)
		return
	}
	if w.mergeTried == nil {
		w.mergeTried = map[int64]mergeTry{}
	}
	w.mergeTried[lane.ID] = key
	if err := ap.RetryJob(ctx, job.ID); err != nil {
		if forge.Unreachable(err) {
			delete(w.mergeTried, lane.ID)
			return
		}
		w.feed(ctx, store.FeedMR, lane.ID, "!%d is approved by %s but retrying %s failed: %s", mr.IID, strings.Join(who, ", "), forge.RequireApprovalJob, firstLine(err.Error()))
		return
	}
	w.feed(ctx, store.FeedMR, lane.ID, "!%d is approved by %s: retried %s; Shepherd merges once the pipeline is green (lane %s)",
		mr.IID, strings.Join(who, ", "), forge.RequireApprovalJob, lane.Name)
}

// SquashMerger is a forge that can squash-merge a request, so commits the person dropped
// from review never reach the base branch. No forge implements it yet.
type SquashMerger interface {
	MergeSquash(ctx context.Context, iid int, sha string) error
}

// mergeSquashed squash-merges where the forge can, else merges as the project is set to.
func mergeSquashed(ctx context.Context, m forge.Merger, iid int, sha string) error {
	if s, ok := m.(SquashMerger); ok {
		return s.MergeSquash(ctx, iid, sha)
	}
	return m.Merge(ctx, iid, sha)
}
