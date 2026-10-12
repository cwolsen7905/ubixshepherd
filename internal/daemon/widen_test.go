package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// lane_widen: the person's call, so the desk needs a turn they started and an answered
// decision from the lane's own runs; the Fold still refuses another lane's globs.
func TestWidenLane(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	a, b, la, _ := twoRuns(t, s)
	open, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: a.ID, Question: "may I touch x?", State: store.DecisionOpen})
	done, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: a.ID, Question: "may I touch y?", State: store.DecisionOpen})
	if _, err := s.Store.AnswerDecision(ctx, done.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	other, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: b.ID, Question: "and z?", State: store.DecisionOpen})
	if _, err := s.Store.AnswerDecision(ctx, other.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	path := api.PathLaneWiden(la.ID)
	body := func(dec int64, add ...string) api.WidenLane { return api.WidenLane{Add: add, Decision: dec} }

	system, _ := s.MintDesk(false, "")
	if code := call(t, ts, system, "POST", path, body(done.ID, "docs/y.md"), nil); code != http.StatusForbidden {
		t.Errorf("desk outside a person's turn: %d", code)
	}
	worker, _ := s.MintWorker(a.ID)
	if code := call(t, ts, worker, "POST", path, body(done.ID, "docs/y.md"), nil); !denied(code) {
		t.Errorf("worker: %d", code)
	}
	human, _ := s.MintDesk(true, "")
	if code := call(t, ts, human, "POST", path, body(done.ID), nil); code != http.StatusBadRequest {
		t.Errorf("no globs: %d", code)
	}
	if code := call(t, ts, human, "POST", path, body(0, "docs/y.md"), nil); code != http.StatusBadRequest {
		t.Errorf("no decision: %d", code)
	}
	if code := call(t, ts, human, "POST", path, body(open.ID, "docs/y.md"), nil); code != http.StatusConflict {
		t.Errorf("unanswered decision: %d", code)
	}
	if code := call(t, ts, human, "POST", path, body(other.ID, "docs/y.md"), nil); code != http.StatusConflict {
		t.Errorf("another lane's decision: %d", code)
	}
	if code := call(t, ts, human, "POST", path, body(done.ID, "b/**"), nil); code == http.StatusOK {
		t.Errorf("a glob another lane holds was added")
	}
	var lane store.Lane
	if code := call(t, ts, human, "POST", path, body(done.ID, "docs/y.md"), &lane); code != http.StatusOK ||
		strings.Join(lane.Scope, ",") != "a/**,docs/y.md" {
		t.Fatalf("widen: %d %+v", code, lane)
	}
	got, _ := s.Store.Lane(ctx, la.ID)
	if strings.Join(got.Scope, ",") != "a/**,docs/y.md" {
		t.Errorf("stored scope %v", got.Scope)
	}
}
