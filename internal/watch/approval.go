package watch

import (
	"context"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// ApprovalRefresh is how long a lane's approval read stands when nothing about its
// merge request or head pipeline has changed: approving moves neither.
const ApprovalRefresh = 5 * time.Minute

// approvalRead is what a lane's approval was last read against, and when.
type approvalRead struct {
	iid      int
	sha      string
	pipeline int64
	status   string
	at       time.Time
}

// approval fills next's Approved and RequireApproval from the forge, for the open
// request's head pipeline. It reads only a forge that can approve, and only when the
// request or pipeline changed or the last read is older than ApprovalRefresh; a failed
// read keeps what was stored. It never approves or retries: that is the person's act.
func (w *Watcher) approval(ctx context.Context, f forge.Forge, lane store.Lane, mr *forge.MR, prev store.LaneForge, next *store.LaneForge) {
	p := mr.Pipeline
	if mr.State != "opened" || p == nil {
		next.Approved, next.RequireApproval = false, ""
		delete(w.approvalRead, lane.ID)
		return
	}
	if mr.IID != prev.MR {
		next.Approved, next.RequireApproval = false, ""
	}
	ap, ok := f.(forge.Approver)
	if !ok {
		return
	}
	cur := approvalRead{iid: mr.IID, sha: mr.SHA, pipeline: p.ID, status: p.Status, at: w.now()}
	if last, ok := w.approvalRead[lane.ID]; ok {
		same := last.iid == cur.iid && last.sha == cur.sha && last.pipeline == cur.pipeline && last.status == cur.status
		if same && cur.at.Sub(last.at) < ApprovalRefresh {
			return
		}
	}
	a, err := ap.Approvals(ctx, mr.IID)
	if err != nil {
		w.Log.Debug("watch: approvals", "lane", lane.Name, "err", err)
		return
	}
	job, err := ap.JobByName(ctx, p.ID, forge.RequireApprovalJob)
	if err != nil {
		w.Log.Debug("watch: approval job", "lane", lane.Name, "err", err)
		return
	}
	next.Approved, next.RequireApproval = a.Approved, ""
	if job != nil {
		next.RequireApproval = job.Status
	}
	if w.approvalRead == nil {
		w.approvalRead = map[int64]approvalRead{}
	}
	w.approvalRead[lane.ID] = cur
}
