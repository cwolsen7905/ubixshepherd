package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Approving a lane's merge request from the web page is the person's own act. The
// approve route is for a browser session only: the desk, an agent and the CLI's operator
// token are refused, there is no command or MCP tool for it, and it records who and when
// in the feed and, through acted, in the desk's thread. Agents never approve.

// The two routes, and below them their wire types.
const (
	pathLaneApproval = "/v1/lanes/{id}/approval"
	pathLaneApprove  = "/v1/lanes/{id}/approve"
)

// laneApproval is a lane's merge request as the approval control shows it.
type laneApproval struct {
	// Supported is false when the lane's forge cannot approve from here; Reason says why.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	// MR is 0 when the lane has no merge request.
	MR      int    `json:"mr,omitempty"`
	MRURL   string `json:"mr_url,omitempty"`
	MRState string `json:"mr_state,omitempty"` // opened, merged, closed
	// SHA is the merge request's head now: what an approval would be for.
	SHA        string   `json:"sha,omitempty"`
	Approved   bool     `json:"approved"`
	ApprovedBy []string `json:"approved_by,omitempty"`
	Pipeline   int64    `json:"pipeline,omitempty"`
	// PipelineStatus is the head pipeline's; RequireApproval the status of its
	// require-approval job, empty when the pipeline has none.
	PipelineStatus  string `json:"pipeline_status,omitempty"`
	PipelineURL     string `json:"pipeline_url,omitempty"`
	RequireApproval string `json:"require_approval,omitempty"`
}

// approveBody is the body of POST /v1/lanes/{id}/approve: the head the person looked at.
type approveBody struct {
	SHA string `json:"sha"`
}

// approveResult is what an approval did.
type approveResult struct {
	laneApproval
	// Retried says the require-approval job was retried so the pipeline judges the new
	// sign-off; RetryNote says why not, when it was not.
	Retried   bool   `json:"retried"`
	RetryNote string `json:"retry_note,omitempty"`
}

var errNoMR = errors.New("this lane has no merge request")

// laneMR is the forge and merge request of a lane, read live. The forge is nil when
// Shepherd cannot read the lane's.
func (s *Server) laneMR(ctx context.Context, laneID int64) (store.Lane, forge.Forge, *forge.MR, error) {
	lane, err := s.Store.Lane(ctx, laneID)
	if err != nil {
		return lane, nil, nil, err
	}
	repo, err := s.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return lane, nil, nil, err
	}
	if s.Fold.ForgeFor == nil || repo.Remote == "" {
		return lane, nil, nil, nil
	}
	fg, err := s.Fold.ForgeFor(repo.Remote)
	if err != nil {
		// A forge Shepherd cannot read (GitHub for now): not an error, just unsupported.
		return lane, nil, nil, nil
	}
	mr, err := fg.MRForBranch(ctx, lane.Branch)
	return lane, fg, mr, err
}

// readApproval is the lane's approval view. A forge without approval gives Supported
// false with the merge request's link when one is known.
func (s *Server) readApproval(ctx context.Context, laneID int64) (store.Lane, forge.Approver, laneApproval, error) {
	lane, fg, mr, err := s.laneMR(ctx, laneID)
	if err != nil {
		return lane, nil, laneApproval{}, err
	}
	var v laneApproval
	if mr != nil {
		v.MR, v.MRURL, v.MRState, v.SHA = mr.IID, mr.URL, mr.State, mr.SHA
		if p := mr.Pipeline; p != nil {
			v.Pipeline, v.PipelineStatus, v.PipelineURL = p.ID, p.Status, p.URL
		}
	}
	ap, ok := fg.(forge.Approver)
	if !ok {
		v.Reason = "Approval is not available for this lane's forge: use the merge request's link."
		if fg == nil {
			v.Reason = "The lane's forge is not one Shepherd can read: approve on the forge."
		}
		return lane, nil, v, nil
	}
	v.Supported = true
	if mr == nil {
		return lane, ap, v, nil
	}
	if a, err := ap.Approvals(ctx, mr.IID); err == nil {
		v.Approved, v.ApprovedBy = a.Approved, a.By
	} else {
		return lane, ap, v, err
	}
	if mr.Pipeline != nil {
		if j, err := ap.JobByName(ctx, mr.Pipeline.ID, forge.RequireApprovalJob); err == nil && j != nil {
			v.RequireApproval = j.Status
		}
	}
	return lane, ap, v, nil
}

func (s *Server) laneApproval(w http.ResponseWriter, r *http.Request) {
	_, _, v, err := s.readApproval(r.Context(), pathID(r))
	if err != nil {
		s.approvalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) approvalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case forge.Unreachable(err):
		writeError(w, http.StatusBadGateway, fmt.Errorf("the forge is unreachable: %w", err))
	default:
		writeError(w, http.StatusBadGateway, fmt.Errorf("the forge refused: %s", redact.String(err.Error())))
	}
}

func (s *Server) approveLane(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The person's own click in the browser: no token, the desk's included, may do it.
	if principalOf(ctx).Role != RoleWeb {
		writeError(w, http.StatusForbidden, errors.New("approving is the person's own act in the web page; Shepherd's tools cannot do it"))
		return
	}
	var body approveBody
	if !decode(w, r, &body) {
		return
	}
	body.SHA = strings.TrimSpace(body.SHA)
	if body.SHA == "" {
		writeError(w, http.StatusBadRequest, errors.New("sha is the head the approval is for"))
		return
	}
	lane, ap, v, err := s.readApproval(ctx, pathID(r))
	if err != nil {
		s.approvalError(w, err)
		return
	}
	switch {
	case !v.Supported:
		writeError(w, http.StatusConflict, errors.New(v.Reason))
		return
	case v.MR == 0:
		writeError(w, http.StatusConflict, errNoMR)
		return
	case v.MRState != "opened":
		writeError(w, http.StatusConflict, fmt.Errorf("!%d is %s: there is nothing to approve", v.MR, v.MRState))
		return
	case v.SHA != body.SHA:
		writeError(w, http.StatusConflict, fmt.Errorf("!%d changed since you looked (now at %s): look at the diff again", v.MR, short(v.SHA)))
		return
	}
	if err := ap.Approve(ctx, v.MR, body.SHA); err != nil {
		s.approvalError(w, err)
		return
	}
	s.Log.Info("merge request approved from the web", "lane", lane.Name, "mr", v.MR, "sha", short(body.SHA))
	s.Store.AddFeed(ctx, store.FeedMRApprove, fmt.Sprintf("!%d approved by the person from the web at %s (lane %s)", v.MR, short(body.SHA), lane.Name), lane.ID)

	res := approveResult{laneApproval: v}
	res.Approved = true
	// GitLab does not judge a pipeline again on an approval, and require-approval holds
	// the merge until it passes: retry it so the person need not go to the forge.
	switch {
	case v.Pipeline == 0:
		res.RetryNote = "the merge request has no pipeline to retry"
	case v.RequireApproval == "":
		res.RetryNote = "the pipeline has no " + forge.RequireApprovalJob + " job"
	case v.RequireApproval != "failed" && v.RequireApproval != "canceled":
		res.RetryNote = fmt.Sprintf("the %s job is %s: it will judge the approval itself", forge.RequireApprovalJob, v.RequireApproval)
	default:
		j, err := ap.JobByName(ctx, v.Pipeline, forge.RequireApprovalJob)
		if err == nil && j != nil {
			err = ap.RetryJob(ctx, j.ID)
		}
		if err != nil || j == nil {
			res.RetryNote = "could not retry the " + forge.RequireApprovalJob + " job: retry it on the forge"
			s.Log.Warn("retry the approval job", "lane", lane.Name, "err", err)
		} else {
			res.Retried = true
			res.RequireApproval = "pending"
		}
	}
	s.refreshView(lane.RepoID)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) describeApproveLane(ctx context.Context, r *http.Request, body, _ []byte) (int64, string) {
	ws, lane := s.workspaceOfLane(ctx, pathID(r))
	return ws, "approved the merge request of lane " + lane.Name + " from the web page"
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
