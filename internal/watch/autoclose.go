package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// AutoCloseSettle is how long a lane's last run must have been over before Shepherd
// closes the lane by itself: time for the person to look, or to continue the session.
const AutoCloseSettle = 10 * time.Minute

// runsRead is how many of a lane's newest runs auto-close looks at.
const runsRead = 20

// autoClose closes a lane nobody is working in, with nothing left to land, on the
// strength of its own state rather than a forge's word: a lane with no merge request
// (config-only work), whose changes went in through another branch, or whose merge
// was missed while polling failed. It is conservative: every one of these must hold, and
// anything else leaves the lane open for the person.
//
//   - no run is going, no request waits on the lane, no decision is open or undelivered
//   - the last run succeeded and ended AutoCloseSettle ago, and its last report says done
//     (a run that was interrupted, stopped or failed never qualifies)
//   - the worktree is clean and every commit's change is in the base (fold.Review.InBase)
//   - Fold.Close, with its own checks and without force, agrees
//
// It reports whether it closed the lane.
func (w *Watcher) autoClose(ctx context.Context, lane store.Lane) bool {
	if w.NoAutoClose || w.Fold == nil {
		return false
	}
	if why := w.notIdle(ctx, lane); why != "" {
		w.Log.Debug("watch: lane not auto-closed", "lane", lane.Name, "why", why)
		return false
	}
	// The review fetches and asks the forge: not for every lane on every pass.
	if w.reviewed == nil {
		w.reviewed = map[int64]time.Time{}
	}
	if at, ok := w.reviewed[lane.ID]; ok && w.now().Sub(at) < AutoCloseSettle {
		return false
	}
	w.reviewed[lane.ID] = w.now()
	rev, err := w.Fold.ReviewLane(ctx, lane.ID, true)
	if err != nil {
		w.Log.Debug("watch: lane not auto-closed", "lane", lane.Name, "why", err)
		return false
	}
	if rev.Verdict != fold.Finished || !rev.InBase {
		w.Log.Debug("watch: lane not auto-closed", "lane", lane.Name, "why", rev.Verdict+": "+rev.Why)
		return false
	}
	res, err := w.Fold.Close(ctx, lane.ID, false)
	if err != nil {
		w.Log.Debug("watch: lane not auto-closed", "lane", lane.Name, "why", firstLine(err.Error()))
		return false
	}
	w.Log.Info("watch: lane closed, finished", "lane", lane.Name, "why", rev.Why, "branch_deleted", res.BranchDeleted)
	if err := w.Fold.WriteView(ctx, lane.RepoID); err != nil {
		w.Log.Error("watch: coordination view", "err", err)
	}
	w.feed(ctx, store.FeedLaneClosed, lane.ID, "lane %s closed by Shepherd: finished, %s; its last run reported done", lane.Name, rev.Why)
	return true
}

// notIdle says why a lane has work or a question in flight, or "" when it has none and
// its last run ended well and is settled.
func (w *Watcher) notIdle(ctx context.Context, lane store.Lane) string {
	runs, err := w.Store.Runs(ctx, lane.ID, "", runsRead)
	if err != nil {
		return "runs unreadable: " + err.Error()
	}
	if len(runs) == 0 {
		return "no run has been in it"
	}
	mine := map[int64]bool{}
	for _, r := range runs {
		mine[r.ID] = true
		if r.State == store.RunRunning {
			return fmt.Sprintf("run %d is going", r.ID)
		}
	}
	last := runs[0]
	if last.State != store.RunSucceeded {
		return fmt.Sprintf("its last run, %d, is %s", last.ID, last.State)
	}
	if last.Ended == nil || w.now().Sub(*last.Ended) < AutoCloseSettle {
		return fmt.Sprintf("its last run, %d, ended under %s ago", last.ID, AutoCloseSettle)
	}
	if !reportedDone(ctx, w.Store, last.ID) {
		return fmt.Sprintf("its last run, %d, did not report done", last.ID)
	}

	if lf, err := w.Store.LaneForge(ctx, lane.ID); err == nil && lf.MRState == "opened" {
		return fmt.Sprintf("its merge request !%d is open", lf.MR)
	}
	ds, err := w.Store.Decisions(ctx, "")
	if err != nil {
		return "decisions unreadable: " + err.Error()
	}
	for _, d := range ds {
		if !mine[d.RunID] {
			continue
		}
		if d.State == store.DecisionOpen || (d.State == store.DecisionAnswered && d.AnswerRun == 0 && d.RunID == last.ID) {
			return fmt.Sprintf("decision %d is open or its answer is undelivered", d.ID)
		}
	}
	qs, err := w.Store.Requests(ctx, store.RequestPending, store.RequestNeedsRouting, store.RequestRouted, store.RequestReplyReady)
	if err != nil {
		return "requests unreadable: " + err.Error()
	}
	for _, q := range qs {
		if q.Lane == lane.Name || mine[q.FromRun] || mine[q.TargetRun] {
			return fmt.Sprintf("request %d is open", q.ID)
		}
	}
	return ""
}

// reportedDone reports whether the run's last report says done.
func reportedDone(ctx context.Context, st store.Store, runID int64) bool {
	events, err := st.Events(ctx, runID)
	if err != nil {
		return false
	}
	status := ""
	for _, e := range events {
		if e.Kind == "report" {
			status = e.Status
		}
	}
	return status == "done"
}
