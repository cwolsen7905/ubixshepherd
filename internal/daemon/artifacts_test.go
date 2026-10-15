package daemon

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A worker declares a file of its own worktree, asks a question with another, and the
// person's reads show both: the list, one artifact, and the decision's view.
func TestWorkerDeclaresArtifactsAndDecisionLinksThem(t *testing.T) {
	s, ts := newServer(t)
	a, _, la, _ := twoRuns(t, s)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: t.TempDir(), Log: s.Log}
	for rel, body := range map[string]string{"plan.md": "# plan\n", "notes.md": "notes\n"} {
		if err := os.MkdirAll(la.Worktree, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(la.Worktree, rel), []byte(body), 0o644)
	}
	worker, _ := s.MintWorker(a.ID)
	op := s.Token

	var got api.ArtifactDeclared
	if code := call(t, ts, worker, "POST", api.PathRunArtifacts(a.ID), api.ArtifactDeclare{Path: "plan.md", Title: "Plan", Intent: "keep"}, &got); code != http.StatusOK {
		t.Fatalf("declare: %d", code)
	}
	if !got.New || !got.Version || got.Artifact.Path != "plan.md" || got.Artifact.RunID != a.ID || got.Artifact.LaneID != la.ID {
		t.Errorf("declared = %+v", got)
	}
	// What is not a file of the worktree is the agent's error, not a server error.
	if code := call(t, ts, worker, "POST", api.PathRunArtifacts(a.ID), api.ArtifactDeclare{Path: "../x.md"}, nil); code != http.StatusConflict && code != http.StatusBadRequest {
		t.Errorf("escaping path: %d", code)
	}
	if code := call(t, ts, worker, "POST", api.PathRunArtifacts(a.ID), api.ArtifactDeclare{Path: "plan.md", Intent: "bogus"}, nil); code != http.StatusConflict && code != http.StatusBadRequest {
		t.Errorf("bad intent: %d", code)
	}

	var d store.Decision
	if code := call(t, ts, worker, "POST", api.PathRunDecisions(a.ID), store.Decision{Question: "ok?", Recommendation: "yes", ArtifactPaths: []string{"notes.md", "plan.md"}}, &d); code != http.StatusOK {
		t.Fatalf("ask: %d", code)
	}
	// A path that does not exist holds no question.
	if code := call(t, ts, worker, "POST", api.PathRunDecisions(a.ID), store.Decision{Question: "again?", ArtifactPaths: []string{"missing.md"}}, nil); code == http.StatusOK {
		t.Error("a question naming a missing file was held")
	}
	var views []api.DecisionView
	if code := call(t, ts, op, "GET", api.PathDecisions+"?state=open", nil, &views); code != http.StatusOK || len(views) != 1 {
		t.Fatalf("decisions: %d, %d views", code, len(views))
	}
	if len(views[0].Artifacts) != 2 {
		t.Errorf("decision artifacts = %+v", views[0].Artifacts)
	}

	var list []store.Artifact
	if code := call(t, ts, op, "GET", api.ArtifactListQuery(store.ArtifactFilter{LaneID: la.ID, Kind: store.ArtifactMarkdown}), nil, &list); code != http.StatusOK || len(list) != 2 {
		t.Fatalf("list: %d, %+v", code, list)
	}
	var one store.Artifact
	if code := call(t, ts, op, "GET", api.PathArtifact(got.Artifact.ID), nil, &one); code != http.StatusOK || len(one.Versions) != 1 || len(one.Links) != 1 || one.Links[0].Kind != store.ArtifactLinkDecision {
		t.Errorf("one: %d, %+v", code, one)
	}
	if code := call(t, ts, op, "GET", api.PathArtifact(999), nil, nil); code != http.StatusNotFound {
		t.Errorf("missing: %d, want 404", code)
	}
	if code := call(t, ts, op, "GET", api.PathArtifacts+"?lane_id=x", nil, nil); code != http.StatusBadRequest {
		t.Errorf("bad filter: %d", code)
	}
}

// A declaration copies the file at once; its snapshot is served redacted with the
// content headers; changing intent, pinning, discarding and retention are the person's.
func TestArtifactSnapshotsAndPersonsEdits(t *testing.T) {
	s, ts := newServer(t)
	a, _, la, _ := twoRuns(t, s)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: t.TempDir(), Log: s.Log}
	s.Fold.ArtifactDir = filepath.Join(t.TempDir(), "artifacts")
	os.MkdirAll(la.Worktree, 0o755)
	os.WriteFile(filepath.Join(la.Worktree, "plan.md"), []byte("# plan\napi_key=abcd1234efgh\n"), 0o644)
	worker, _ := s.MintWorker(a.ID)
	desk, _ := s.MintDesk(false, "t")
	op := s.Token

	var got api.ArtifactDeclared
	if code := call(t, ts, worker, "POST", api.PathRunArtifacts(a.ID), api.ArtifactDeclare{Path: "plan.md", Intent: "keep"}, &got); code != http.StatusOK {
		t.Fatalf("declare: %d", code)
	}
	id := got.Artifact.ID
	var one store.Artifact
	call(t, ts, op, "GET", api.PathArtifact(id), nil, &one)
	if len(one.Versions) != 1 || one.Versions[0].Snapshotted == nil {
		t.Fatalf("versions = %+v", one.Versions)
	}

	req, _ := http.NewRequest("GET", ts.URL+api.PathArtifact(id)+"/content?version="+one.Versions[0].SHA256, nil)
	req.Header.Set("Authorization", "Bearer "+desk)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(body), "abcd1234efgh") || !strings.Contains(string(body), "# plan") ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("content: %d %q %v", resp.StatusCode, body, resp.Header)
	}

	// The desk and agents cannot make the person's calls.
	pin := map[string]any{"pinned": true}
	for _, tok := range []string{desk, worker} {
		if code := call(t, ts, tok, "PATCH", api.PathArtifact(id), pin, nil); code != http.StatusForbidden {
			t.Errorf("patch with a scoped token: %d", code)
		}
		if code := call(t, ts, tok, "POST", api.PathArtifact(id)+"/discard", nil, nil); code != http.StatusForbidden {
			t.Errorf("discard with a scoped token: %d", code)
		}
		if code := call(t, ts, tok, "POST", api.PathArtifacts+"/gc", map[string]any{}, nil); code != http.StatusForbidden {
			t.Errorf("gc with a scoped token: %d", code)
		}
	}
	var patched store.Artifact
	if code := call(t, ts, op, "PATCH", api.PathArtifact(id), map[string]any{"pinned": true, "intent": "commit"}, &patched); code != http.StatusOK ||
		!patched.Pinned || patched.Intent != store.ArtifactCommit || patched.IntentBy != store.IntentByPerson {
		t.Fatalf("patch: %d %+v", code, patched)
	}
	if code := call(t, ts, op, "PATCH", api.PathArtifact(id), map[string]any{"intent": "bogus"}, nil); code != http.StatusConflict {
		t.Errorf("bad intent: %d", code)
	}
	var res fold.GCResult
	if code := call(t, ts, op, "POST", api.PathArtifacts+"/gc", map[string]any{"dry_run": true}, &res); code != http.StatusOK || res.KeptByte == 0 {
		t.Errorf("gc: %d %+v", code, res)
	}
	var gone store.Artifact
	if code := call(t, ts, op, "POST", api.PathArtifact(id)+"/discard", nil, &gone); code != http.StatusOK || gone.State != store.ArtifactDiscarded {
		t.Fatalf("discard: %d %+v", code, gone)
	}
	if _, err := os.Stat(filepath.Join(la.Worktree, "plan.md")); !os.IsNotExist(err) {
		t.Error("file left after discard")
	}
	if code := call(t, ts, op, "GET", api.PathArtifact(id)+"/content", nil, nil); code != http.StatusNotFound {
		t.Errorf("content after discard: %d", code)
	}
}
