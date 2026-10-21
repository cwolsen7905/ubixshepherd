package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGitLabApprovals(t *testing.T) {
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
		if got := args[len(args)-1]; got != "projects/group%2Fapp/merge_requests/7/approvals" {
			return nil, fmt.Errorf("unexpected %s", got)
		}
		return []byte(`{"approved":true,"approved_by":[{"user":{"username":"ann"}},{"user":{"username":"bo"}}]}`), nil
	}}
	a, err := g.Approvals(context.Background(), 7)
	if err != nil || !a.Approved || strings.Join(a.By, ",") != "ann,bo" {
		t.Fatalf("approvals = %+v, %v", a, err)
	}
}

func TestGitLabApproveSendsTheHead(t *testing.T) {
	var got []string
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return []byte(`{}`), nil
	}}
	if err := g.Approve(context.Background(), 7, "abc123"); err != nil {
		t.Fatal(err)
	}
	line := strings.Join(got, " ")
	for _, want := range []string{"--method POST", "projects/group%2Fapp/merge_requests/7/approve", "--raw-field sha=abc123"} {
		if !strings.Contains(line, want) {
			t.Errorf("args %q lack %q", line, want)
		}
	}
}

func TestGitLabJobByNameAndRetry(t *testing.T) {
	var retried string
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		switch {
		case strings.Contains(line, "pipelines/9/jobs"):
			return []byte(`[{"id":3,"name":"lint","status":"success"},{"id":4,"name":"require-approval","status":"failed","web_url":"u"}]`), nil
		case strings.Contains(line, "jobs/4/retry"):
			retried = line
			return []byte(`{"id":5}`), nil
		}
		return nil, fmt.Errorf("unexpected %s", line)
	}}
	j, err := g.JobByName(context.Background(), 9, RequireApprovalJob)
	if err != nil || j == nil || j.ID != 4 || j.Status != "failed" {
		t.Fatalf("job = %+v, %v", j, err)
	}
	if err := g.RetryJob(context.Background(), j.ID); err != nil || !strings.Contains(retried, "--method POST") {
		t.Fatalf("retry = %q, %v", retried, err)
	}
	if j, _ := g.JobByName(context.Background(), 9, "nope"); j != nil {
		t.Errorf("found %+v", j)
	}
}

// The watcher's tests use AIReviewJob itself, so they pass whatever it is set to. This literal
// only guards the constant's value against an accidental edit; it cannot read Kelpie's
// template. When the CI include ref is bumped, check the template's job name and update both.
func TestAIReviewJobValue(t *testing.T) {
	if AIReviewJob != "kelpie-review" {
		t.Fatalf("AIReviewJob = %q, want kelpie-review", AIReviewJob)
	}
}
