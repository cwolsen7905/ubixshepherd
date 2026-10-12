package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// approvingForge is a forge that can approve, recording what it was asked.
type approvingForge struct {
	mr       *forge.MR
	by       []string
	job      *forge.PipelineJob
	approved []string
	retried  []int64
}

func (f *approvingForge) Name() string { return "fake" }
func (f *approvingForge) MRForBranch(context.Context, string) (*forge.MR, error) {
	return f.mr, nil
}
func (f *approvingForge) FailedJobs(context.Context, int64) ([]forge.Job, error) { return nil, nil }
func (f *approvingForge) JobLog(context.Context, int64, int) (string, error)     { return "", nil }
func (f *approvingForge) CreateMR(context.Context, string, string, string, string) (*forge.MR, error) {
	return nil, nil
}
func (f *approvingForge) RefPipeline(context.Context, string) (*forge.Pipeline, error) {
	return nil, nil
}
func (f *approvingForge) Approvals(context.Context, int) (*forge.Approvals, error) {
	return &forge.Approvals{Approved: len(f.by) > 0, By: f.by}, nil
}
func (f *approvingForge) Approve(_ context.Context, _ int, sha string) error {
	f.approved = append(f.approved, sha)
	f.by = append(f.by, "me")
	return nil
}
func (f *approvingForge) JobByName(context.Context, int64, string) (*forge.PipelineJob, error) {
	return f.job, nil
}
func (f *approvingForge) RetryJob(_ context.Context, id int64) error {
	f.retried = append(f.retried, id)
	return nil
}

func approveLane(t *testing.T, s *Server, fg forge.Forge) store.Lane {
	t.Helper()
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app"), Remote: "git@gl.example.com:g/app.git"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := s.Store.Repos(ctx, ws.ID)
	l, err := s.Store.CreateLane(ctx, store.Lane{RepoID: repos[0].ID, Name: "feat", Branch: "feat/x", Base: "main",
		Worktree: filepath.Join(root, "feat"), Scope: []string{"x/**"}, State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	s.Fold.ForgeFor = func(string) (forge.Forge, error) { return fg, nil }
	return l
}

func openMR() *forge.MR {
	return &forge.MR{IID: 7, State: "opened", SHA: "abcdef0123456789", URL: "https://gl.example.com/g/app/-/merge_requests/7",
		Pipeline: &forge.Pipeline{ID: 9, Status: "failed", URL: "p"}}
}

func TestApproveIsOnlyTheBrowsers(t *testing.T) {
	s, base, _ := webDaemon(t)
	fg := &approvingForge{mr: openMR(), job: &forge.PipelineJob{ID: 4, Status: "failed"}}
	l := approveLane(t, s, fg)
	path := "/v1/lanes/" + itoa(l.ID) + "/approve"
	body := map[string]string{"sha": fg.mr.SHA}

	deskTok, _ := s.tokens.mint(principal{Role: RoleDesk, Human: true})
	workerTok, _ := s.tokens.mint(principal{Role: RoleWorker, Run: 1})
	for name, tok := range map[string]string{"operator": s.Token, "desk": deskTok, "worker": workerTok} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+path, strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, resp.StatusCode)
		}
	}
	if len(fg.approved) != 0 {
		t.Fatalf("approved by a token: %v", fg.approved)
	}
}

func TestApproveFromTheWeb(t *testing.T) {
	s, base, _ := webDaemon(t)
	fg := &approvingForge{mr: openMR(), job: &forge.PipelineJob{ID: 4, Name: "require-approval", Status: "failed"}}
	l := approveLane(t, s, fg)
	br := newBrowser(t, base)
	signIn(t, s, br)
	path := "/v1/lanes/" + itoa(l.ID)

	var v laneApproval
	resp, out := br.do("GET", path+"/approval", nil)
	if resp.StatusCode != 200 || json.Unmarshal([]byte(out), &v) != nil || !v.Supported || v.Approved || v.MR != 7 || v.RequireApproval != "failed" || v.PipelineStatus != "failed" {
		t.Fatalf("read: %d %s", resp.StatusCode, out)
	}

	// A head that moved since the person looked is refused, and nothing is approved.
	resp, _ = br.do("POST", path+"/approve", approveBody{SHA: "0000000"})
	if resp.StatusCode != http.StatusConflict || len(fg.approved) != 0 {
		t.Fatalf("stale head: %d, approved %v", resp.StatusCode, fg.approved)
	}
	resp, _ = br.do("POST", path+"/approve", approveBody{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no sha: %d", resp.StatusCode)
	}

	var res approveResult
	resp, out = br.do("POST", path+"/approve", approveBody{SHA: fg.mr.SHA})
	if resp.StatusCode != 200 || json.Unmarshal([]byte(out), &res) != nil || !res.Approved || !res.Retried {
		t.Fatalf("approve: %d %s", resp.StatusCode, out)
	}
	if len(fg.approved) != 1 || fg.approved[0] != fg.mr.SHA || len(fg.retried) != 1 || fg.retried[0] != 4 {
		t.Errorf("forge calls: approved %v, retried %v", fg.approved, fg.retried)
	}

	feed, _ := s.Store.Feed(context.Background(), 0, 50)
	found := false
	for _, f := range feed {
		if f.Kind == store.FeedMR && f.Ref == l.ID && strings.Contains(f.Text, "!7 approved by the person from the web at abcdef01") {
			found = true
		}
	}
	if !found {
		t.Errorf("no feed line: %+v", feed)
	}
	got := actionTexts(deskHistory(t, s, base))
	if len(got) != 1 || !strings.Contains(got[0], "approved the merge request of lane feat") {
		t.Errorf("desk history = %q", got)
	}

	// A merged request has nothing to approve.
	fg.mr.State = "merged"
	resp, _ = br.do("POST", path+"/approve", approveBody{SHA: fg.mr.SHA})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("merged: %d", resp.StatusCode)
	}
}

func TestApproveWithNoMRAndNoApprovalSupport(t *testing.T) {
	s, base, _ := webDaemon(t)
	fg := &approvingForge{}
	l := approveLane(t, s, fg)
	br := newBrowser(t, base)
	signIn(t, s, br)
	path := "/v1/lanes/" + itoa(l.ID)

	resp, _ := br.do("POST", path+"/approve", approveBody{SHA: "abc"})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("no MR: %d", resp.StatusCode)
	}

	// A forge that cannot approve: the read says so, with the link, and approve refuses.
	s.Fold.ForgeFor = func(string) (forge.Forge, error) { return readOnlyForge{mr: openMR()}, nil }
	var v laneApproval
	resp, out := br.do("GET", path+"/approval", nil)
	if resp.StatusCode != 200 || json.Unmarshal([]byte(out), &v) != nil || v.Supported || v.MRURL == "" || v.Reason == "" {
		t.Fatalf("unsupported read: %d %s", resp.StatusCode, out)
	}
	resp, _ = br.do("POST", path+"/approve", approveBody{SHA: openMR().SHA})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("unsupported approve: %d", resp.StatusCode)
	}

	// A forge Shepherd cannot read at all is unsupported too, not an error.
	s.Fold.ForgeFor = func(string) (forge.Forge, error) { return nil, context.Canceled }
	resp, out = br.do("GET", path+"/approval", nil)
	if resp.StatusCode != 200 || json.Unmarshal([]byte(out), &v) != nil || v.Supported {
		t.Errorf("unreadable forge: %d %s", resp.StatusCode, out)
	}
}

// readOnlyForge reads a merge request and nothing more.
type readOnlyForge struct{ mr *forge.MR }

func (readOnlyForge) Name() string { return "readonly" }
func (r readOnlyForge) MRForBranch(context.Context, string) (*forge.MR, error) {
	return r.mr, nil
}
func (readOnlyForge) FailedJobs(context.Context, int64) ([]forge.Job, error) { return nil, nil }
func (readOnlyForge) JobLog(context.Context, int64, int) (string, error)     { return "", nil }
func (readOnlyForge) CreateMR(context.Context, string, string, string, string) (*forge.MR, error) {
	return nil, nil
}
func (readOnlyForge) RefPipeline(context.Context, string) (*forge.Pipeline, error) { return nil, nil }
