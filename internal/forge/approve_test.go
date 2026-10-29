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

func TestPersonApproversSkipAuthorBotsAndListed(t *testing.T) {
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"approved":true,"approved_by":[{"user":{"username":"dev"}},{"user":{"username":"svc","bot":true}},{"user":{"username":"project_3_bot_ab"}},{"user":{"username":"helper"}},{"user":{"username":"pat"}}]}`), nil
	}}
	a, err := g.Approvals(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(a.PersonApprovers("dev", []string{"helper"}), ","); got != "pat" {
		t.Fatalf("persons = %q", got)
	}
}

func TestGitLabMergeSendsTheHeadAsPut(t *testing.T) {
	var got []string
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return []byte(`{}`), nil
	}}
	if err := g.Merge(context.Background(), 7, "abc123"); err != nil {
		t.Fatal(err)
	}
	line := strings.Join(got, " ")
	for _, want := range []string{"--method PUT", "projects/group%2Fapp/merge_requests/7/merge", "--raw-field sha=abc123"} {
		if !strings.Contains(line, want) {
			t.Errorf("args %q lack %q", line, want)
		}
	}
}

func TestGitLabApprovedHeadCountsOnlyApprovalsAfterThePush(t *testing.T) {
	g := &GitLab{Host: "gl.example.com", Project: "group/app", Run: func(_ context.Context, args ...string) ([]byte, error) {
		switch path := args[len(args)-1]; {
		case strings.Contains(path, "/versions"):
			return []byte(`[{"head_commit_sha":"new","created_at":"2026-10-28T10:00:00Z"},{"head_commit_sha":"old","created_at":"2026-10-28T08:00:00Z"}]`), nil
		case strings.Contains(path, "/notes"):
			return []byte(`[
{"system":true,"body":"approved this merge request","created_at":"2026-10-28T11:00:00Z","author":{"username":"late"}},
{"system":true,"body":"unapproved this merge request","created_at":"2026-10-28T10:30:00Z","author":{"username":"changed"}},
{"system":true,"body":"approved this merge request","created_at":"2026-10-28T09:00:00Z","author":{"username":"early"}},
{"system":true,"body":"approved this merge request","created_at":"2026-10-28T08:30:00Z","author":{"username":"changed"}},
{"system":false,"body":"approved this merge request","created_at":"2026-10-28T11:30:00Z","author":{"username":"talker"}}]`), nil
		}
		return nil, fmt.Errorf("unexpected %v", args)
	}}
	got, err := g.ApprovedHead(context.Background(), 7, "new", []string{"late", "changed", "early", "talker"})
	if err != nil || strings.Join(got, ",") != "late" {
		t.Fatalf("approved = %v, %v", got, err)
	}
	if _, err := g.ApprovedHead(context.Background(), 7, "unknown", []string{"late"}); err == nil {
		t.Fatal("an unknown head was accepted")
	}
	if err := g.Merge(context.Background(), 7, ""); err == nil {
		t.Fatal("merged with no head")
	}
}
