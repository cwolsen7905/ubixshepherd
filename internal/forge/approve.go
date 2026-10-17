package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// RequireApprovalJob is the name of the CI job that holds a merge request until a person
// has signed off (bin/ci/require-approval.sh).
const RequireApprovalJob = "require-approval"

// AIReviewJob is the name of the pipeline's AI review job. It is advisory (never red); it
// exits UnreviewedExit when it could not review.
const AIReviewJob = "ai-review-mr"

// UnreviewedExit is the exit code of an AI review job that did not review.
const UnreviewedExit = 75

// Unreviewed says whether a job's log shows it did not review: its summary says
// UNREVIEWED, or the runner reports exit code 75.
func Unreviewed(log string) bool {
	return strings.Contains(log, "UNREVIEWED") || strings.Contains(log, fmt.Sprintf("exit code %d", UnreviewedExit))
}

// Approvals is who has signed off on a merge request.
type Approvals struct {
	Approved bool `json:"approved"`
	// By is the usernames of the approvers.
	By []string `json:"by,omitempty"`
}

// PipelineJob is one job of a pipeline, as far as retrying it needs.
type PipelineJob struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

// Approver is a forge that can approve a merge request and retry a pipeline job. It is
// separate from Forge: a forge that cannot (GitHub is not read at all yet) simply does not
// implement it, and callers say approval is not available there.
//
// Approve is the person's act. Nothing that runs unattended may call it: Shepherd's
// rule is that agents never approve.
type Approver interface {
	// Approvals reads the request's approvals.
	Approvals(ctx context.Context, iid int) (*Approvals, error)
	// Approve approves the request as the forge's signed-in user. sha is the head the
	// person looked at: the forge refuses if the head has moved.
	Approve(ctx context.Context, iid int, sha string) error
	// JobByName is the pipeline's job of that name, or nil.
	JobByName(ctx context.Context, pipeline int64, name string) (*PipelineJob, error)
	// RetryJob retries a job.
	RetryJob(ctx context.Context, job int64) error
}

var _ Approver = (*GitLab)(nil)

func (g *GitLab) Approvals(ctx context.Context, iid int) (*Approvals, error) {
	var a struct {
		Approved   bool `json:"approved"`
		ApprovedBy []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if err := g.api(ctx, fmt.Sprintf("%s/merge_requests/%d/approvals", g.project(), iid), &a); err != nil {
		return nil, err
	}
	out := &Approvals{Approved: a.Approved}
	for _, b := range a.ApprovedBy {
		if b.User.Username != "" {
			out.By = append(out.By, b.User.Username)
		}
	}
	return out, nil
}

func (g *GitLab) post(ctx context.Context, path string, fields ...string) ([]byte, error) {
	run := g.Run
	if run == nil {
		run = glab
	}
	args := []string{"api", "--hostname", g.Host, "--method", "POST", path}
	for _, f := range fields {
		args = append(args, "--raw-field", f)
	}
	return run(ctx, args...)
}

func (g *GitLab) Approve(ctx context.Context, iid int, sha string) error {
	var fields []string
	if sha != "" {
		fields = append(fields, "sha="+sha)
	}
	_, err := g.post(ctx, fmt.Sprintf("%s/merge_requests/%d/approve", g.project(), iid), fields...)
	return err
}

func (g *GitLab) JobByName(ctx context.Context, pipeline int64, name string) (*PipelineJob, error) {
	var jobs []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	}
	if err := g.api(ctx, fmt.Sprintf("%s/pipelines/%d/jobs?per_page=100&include_retried=false&name=%s", g.project(), pipeline, url.QueryEscape(name)), &jobs); err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.Name == name {
			return &PipelineJob{ID: j.ID, Name: j.Name, Status: j.Status, URL: j.WebURL}, nil
		}
	}
	return nil, nil
}

func (g *GitLab) RetryJob(ctx context.Context, job int64) error {
	out, err := g.post(ctx, fmt.Sprintf("%s/jobs/%d/retry", g.project(), job))
	if err != nil {
		return err
	}
	var j struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	return nil
}
