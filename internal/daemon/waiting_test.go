package daemon

import (
	"context"
	"net/http"
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
