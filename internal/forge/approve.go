package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// RequireApprovalJob is the name of the CI job that holds a merge request until a person
// has signed off (bin/ci/require-approval.sh).
const RequireApprovalJob = "require-approval"

// AIReviewJob is the name of the pipeline's AI review job, as Kelpie's review template names
// it. It is advisory (never red); it exits UnreviewedExit when it could not review.
const AIReviewJob = "kelpie-review"

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
	// Approvers is the same people with what the forge says about their accounts.
	Approvers []ApprovedBy `json:"approvers,omitempty"`
}

// ApprovedBy is one account that approved a request.
type ApprovedBy struct {
	Username string `json:"username"`
	// Bot is the forge's own mark of a bot account (a project or group access token's
	// user, or a service account).
	Bot bool `json:"bot,omitempty"`
}

// botName matches the usernames GitLab gives project and group access-token users.
var botName = regexp.MustCompile(`^(project|group)_[0-9]+_bot`)

// IsBot says the account is a bot: the forge marks it so or its name is a token user's.
func (a ApprovedBy) IsBot() bool { return a.Bot || botName.MatchString(a.Username) }

// PersonApprovers is the approvers who count as a person's sign-off on a request by
// author: not the author, not a bot, and not one of the extra usernames in skip.
func (a *Approvals) PersonApprovers(author string, skip []string) []string {
	var out []string
	for _, p := range a.Approvers {
		if p.Username == "" || p.Username == author || p.IsBot() || slices.Contains(skip, p.Username) {
			continue
		}
		out = append(out, p.Username)
	}
	return out
}

// Merger is a forge that can merge a request. It is separate from Approver: merging is
// the act of a repo whose person approved or whose rule allows it, never an agent's.
type Merger interface {
	// Merge merges the request. sha is the head that was checked: the forge refuses if
	// the head has moved. How it merges (merge commit, squash) is the project's setting.
	Merge(ctx context.Context, iid int, sha string) error
}

var _ Merger = (*GitLab)(nil)

// HeadApprover is a forge that can say which approvers approved a request's current
// head. GitLab's approvals are not per commit: a project may keep them across pushes, so
// a name in Approvals proves nothing about the head about to be merged.
type HeadApprover interface {
	// ApprovedHead is the users among who whose latest approval came at or after sha was
	// pushed to the request. An unapproval after an approval cancels it; a head the forge
	// does not know is an error.
	ApprovedHead(ctx context.Context, iid int, sha string, who []string) ([]string, error)
}

var _ HeadApprover = (*GitLab)(nil)

func (g *GitLab) ApprovedHead(ctx context.Context, iid int, sha string, who []string) ([]string, error) {
	if sha == "" {
		return nil, errors.New("no head to check approvals against")
	}
	var versions []struct {
		Head      string `json:"head_commit_sha"`
		CreatedAt string `json:"created_at"`
	}
	if err := g.api(ctx, fmt.Sprintf("%s/merge_requests/%d/versions", g.project(), iid), &versions); err != nil {
		return nil, err
	}
	var pushed time.Time
	for _, v := range versions {
		if v.Head == sha {
			pushed = glTime(v.CreatedAt)
			break
		}
	}
	if pushed.IsZero() {
		return nil, fmt.Errorf("the forge has no version of !%d at %s", iid, sha)
	}
	latest := map[string]time.Time{} // zero: the last word was an unapproval
	for page := 1; page <= 5; page++ {
		var notes []struct {
			Body      string `json:"body"`
			System    bool   `json:"system"`
			CreatedAt string `json:"created_at"`
			Author    struct {
				Username string `json:"username"`
			} `json:"author"`
		}
		q := fmt.Sprintf("%s/merge_requests/%d/notes?sort=desc&order_by=created_at&per_page=100&page=%d", g.project(), iid, page)
		if err := g.api(ctx, q, &notes); err != nil {
			return nil, err
		}
		for _, n := range notes {
			if _, seen := latest[n.Author.Username]; seen || !n.System {
				continue
			}
			switch {
			case strings.HasPrefix(n.Body, "unapproved this merge request"):
				latest[n.Author.Username] = time.Time{}
			case strings.HasPrefix(n.Body, "approved this merge request"):
				latest[n.Author.Username] = glTime(n.CreatedAt)
			}
		}
		if len(notes) < 100 {
			break
		}
	}
	var out []string
	for _, u := range who {
		if at := latest[u]; !at.IsZero() && !at.Before(pushed) {
			out = append(out, u)
		}
	}
	return out, nil
}

func (g *GitLab) Merge(ctx context.Context, iid int, sha string) error {
	if sha == "" {
		return errors.New("refusing to merge without the head that was checked")
	}
	_, err := g.send(ctx, "PUT", fmt.Sprintf("%s/merge_requests/%d/merge", g.project(), iid), "sha="+sha)
	return err
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
				Bot      bool   `json:"bot"`
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
			out.Approvers = append(out.Approvers, ApprovedBy{Username: b.User.Username, Bot: b.User.Bot})
		}
	}
	return out, nil
}

func (g *GitLab) post(ctx context.Context, path string, fields ...string) ([]byte, error) {
	return g.send(ctx, "POST", path, fields...)
}

func (g *GitLab) send(ctx context.Context, method, path string, fields ...string) ([]byte, error) {
	run := g.Run
	if run == nil {
		run = glab
	}
	args := []string{"api", "--hostname", g.Host, "--method", method, path}
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
