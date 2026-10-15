package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestWaitingListsDecisionsAndProposals(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	run, _, _, _ := twoRuns(t, s)
	if _, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "Which?", State: store.DecisionOpen}); err != nil {
		t.Fatal(err)
	}
	system, _ := s.MintDesk(false, "")
	if code := call(t, ts, system, "POST", api.PathNext, api.NextAdd{Title: "Idea", Propose: true}, nil); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	var w api.Waiting
	if code := call(t, ts, s.Token, "GET", api.PathWaiting, nil, &w); code != http.StatusOK {
		t.Fatalf("waiting: %d", code)
	}
	if w.Count != 2 || w.Counts.Decisions != 1 || w.Counts.Next != 1 || w.Items[0].Kind != api.WaitingDecision || w.Items[0].Lane != "a" {
		t.Errorf("waiting: %+v", w)
	}
	if code := call(t, ts, s.Token, "GET", api.PathWaiting+"?workspace_id=x", nil, nil); code != http.StatusBadRequest {
		t.Errorf("bad workspace: %d", code)
	}
	worker, _ := s.MintWorker(run.ID)
	if code := call(t, ts, worker, "GET", api.PathWaiting, nil, nil); !denied(code) {
		t.Errorf("worker read the list: %d", code)
	}
}

// A lane whose request merged but that still holds a file nobody decided waits in the
// list; a file in the Shepherd directory, or a lane not yet merged, does not.
func TestWaitingListsMergedLaneWithUndecidedArtifacts(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	run, _, la, _ := twoRuns(t, s)
	put := func(path, intent string) {
		t.Helper()
		if _, _, err := s.Store.UpsertArtifact(ctx, store.Artifact{
			RepoID: la.RepoID, LaneID: la.ID, RunID: run.ID, Path: path, Kind: store.ArtifactMarkdown,
			Source: store.ArtifactDiscovered, Intent: intent, IntentBy: store.IntentByDefault, SHA256: "ab",
		}); err != nil {
			t.Fatal(err)
		}
	}
	put("docs/plan.md", store.ArtifactUndecided)
	put(".shepherd/artifacts/notes.md", store.ArtifactUndecided)
	put("docs/kept.md", store.ArtifactKeep)
	read := func() api.Waiting {
		t.Helper()
		var w api.Waiting
		if code := call(t, ts, s.Token, "GET", api.PathWaiting, nil, &w); code != http.StatusOK {
			t.Fatalf("waiting: %d", code)
		}
		return w
	}
	if w := read(); w.Counts.Artifacts != 0 {
		t.Fatalf("an unmerged lane waits: %+v", w)
	}
	if err := s.Store.PutLaneForge(ctx, store.LaneForge{LaneID: la.ID, MR: 12, MRState: "merged"}); err != nil {
		t.Fatal(err)
	}
	w := read()
	var found *api.WaitingItem
	for i := range w.Items {
		if w.Items[i].Kind == api.WaitingArtifact {
			found = &w.Items[i]
		}
	}
	if found == nil || w.Counts.Artifacts != 1 || found.LaneID != la.ID || found.MR != 12 || found.Action != "open" ||
		!strings.Contains(found.Title, "1 file to keep or discard") || !strings.Contains(found.Detail, "docs/plan.md") ||
		strings.Contains(found.Detail, "notes.md") || strings.Contains(found.Detail, "kept.md") {
		t.Errorf("waiting: %+v", w)
	}
}
