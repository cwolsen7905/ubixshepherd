package watch

import (
	"context"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// reviewRead is what the watcher has done about a lane's AI review job on one pipeline.
type reviewRead struct {
	pipeline int64
	retried  bool  // the job was retried once
	done     bool  // settled: reviewed, absent, or given up on
	ok       int64 // the pipeline whose review job passed, for the shadow merge rule
}

// aiReview watches the head pipeline's AI review job. When it did not review (its log
// says UNREVIEWED, or it exited 75) the watcher retries it once; if it is still
// unreviewed the request is the person's, with "no automated review". It acts only on a
// forge that can retry a job, only on the open request's head pipeline once the job has
// finished, and reads the forge again only until the job is settled for that pipeline.
// Retrying a CI job is not approving: the watcher still never approves.
func (w *Watcher) aiReview(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR) {
	ap, ok := f.(forge.Approver)
	p := mr.Pipeline
	if !ok || mr.State != "opened" || p == nil {
		delete(w.reviewRead, lane.ID)
		return
	}
	if p.Status != "success" && p.Status != "failed" {
		return // still going; a retried job keeps its pipeline running until it ends
	}
	cur := w.reviewRead[lane.ID]
	if cur.pipeline != p.ID {
		cur = reviewRead{pipeline: p.ID}
	}
	if cur.done {
		return
	}
	job, err := ap.JobByName(ctx, p.ID, forge.AIReviewJob)
	if err != nil {
		w.Log.Debug("watch: ai review job", "lane", lane.Name, "err", err)
		return
	}
	switch {
	case job == nil || job.Name != forge.AIReviewJob:
		cur.done = true
	case job.Status == "failed":
		log, err := f.JobLog(ctx, job.ID, 60)
		if err != nil {
			w.Log.Debug("watch: ai review log", "lane", lane.Name, "err", err)
			return
		}
		switch {
		case !forge.Unreviewed(log):
			cur.done = true // failed for another reason: advisory, nothing to retry
		case cur.retried:
			cur.done = true
			w.feed(ctx, store.FeedPipeline, lane.ID, "no automated review: the AI review job (%s) was unreviewed again after a retry on !%d (%s); it is yours", job.URL, mr.IID, mr.URL)
		default:
			if err := ap.RetryJob(ctx, job.ID); err != nil {
				w.Log.Debug("watch: retry ai review", "lane", lane.Name, "err", err)
				return
			}
			cur.retried = true
			w.feed(ctx, store.FeedPipeline, lane.ID, "the AI review job (%s) was unreviewed on !%d (%s); retried it once", job.URL, mr.IID, mr.URL)
		}
	case job.Status == "success" || job.Status == "canceled" || job.Status == "skipped":
		cur.done = true
		if job.Status == "success" {
			cur.ok = p.ID
		}
	}
	if w.reviewRead == nil {
		w.reviewRead = map[int64]reviewRead{}
	}
	w.reviewRead[lane.ID] = cur
}
