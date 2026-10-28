package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
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

// Answering a scope decision: the person's approval widens the lane in the same call,
// an overlap keeps the answer and says why in the feed, and any other answer leaves the
// scope alone.
func TestAnswerWidensOnApproval(t *testing.T) {
	s, ts := newServer(t)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: t.TempDir(), Log: s.Log}
	ctx := context.Background()
	a, _, la, _ := twoRuns(t, s)
	person, _ := s.MintDesk(true, "")
	ask := func(q string) store.Decision {
		d, err := s.Store.CreateDecision(ctx, store.Decision{RunID: a.ID, Question: q, State: store.DecisionOpen})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	answer := func(d store.Decision, text string) store.Decision {
		var out store.Decision
		if code := call(t, ts, person, "POST", api.PathDecisionAnswer(d.ID), api.Answer{Answer: text}, &out); code != http.StatusOK {
			t.Fatalf("answer: %d", code)
		}
		return out
	}
	scope := func() string {
		l, _ := s.Store.Lane(ctx, la.ID)
		return strings.Join(l.Scope, ",")
	}
	feed := func() string {
		items, _ := s.Store.Feed(ctx, 0, 100)
		var all []string
		for _, it := range items {
			all = append(all, it.Text)
		}
		return strings.Join(all, "\n")
	}

	// Not an approval: the scope stays.
	d := ask("May I widen the scope to add docs/n.md?")
	answer(d, "No, keep it narrow")
	if got := scope(); got != "a/**" {
		t.Errorf("declined answer widened the lane: %s", got)
	}

	// Hedged, or a question that offers a choice: no widening.
	d = ask("May I widen the scope to add docs/h.md?")
	answer(d, "Yes, except docs/h.md")
	d = ask("Keep the scope at a/** or allow docs/o.md too?")
	answer(d, "Yes")
	if got := scope(); got != "a/**" {
		t.Errorf("hedged or choice widened the lane: %s", got)
	}

	// Approval: the lane widens by exactly the named paths, with a feed line.
	d = ask("May I widen the scope to add docs/y.md?")
	if got := answer(d, "Yes"); got.State != store.DecisionAnswered {
		t.Errorf("state %s", got.State)
	}
	if got := scope(); got != "a/**,docs/y.md" {
		t.Errorf("scope after approval: %s", got)
	}
	if f := feed(); !strings.Contains(f, "scope widened by person: +docs/y.md (decision "+itoa(d.ID)+")") {
		t.Errorf("no widen line in feed:\n%s", f)
	}

	// Another open lane holds the glob: the answer stands, the lane stays, the feed says why.
	d = ask("May I widen the scope to add b/x.go?")
	if got := answer(d, "Yes"); got.State != store.DecisionAnswered {
		t.Errorf("state %s", got.State)
	}
	if got := scope(); got != "a/**,docs/y.md" {
		t.Errorf("overlap widened the lane: %s", got)
	}
	if f := feed(); !strings.Contains(f, "scope not widened by the answer to decision "+itoa(d.ID)) {
		t.Errorf("no refusal line in feed:\n%s", f)
	}
}

// Crook may allow the answer and refuse the widening: the answer stands, the scope stays.
func TestAnswerWidenRefusedByCrook(t *testing.T) {
	s, ts := newServer(t)
	s.Runner = &dispatch.Runner{Store: s.Store, Config: config.Default(), Dir: t.TempDir(), Log: s.Log}
	ctx := context.Background()
	a, _, la, _ := twoRuns(t, s)
	person, _ := s.MintDesk(true, "")
	old := widenVerdict
	widenVerdict = func(context.Context, crook.Action) crook.Verdict {
		return crook.Verdict{Kind: crook.Refuse, Rule: "test", Reason: "test refusal"}
	}
	defer func() { widenVerdict = old }()
	d, _ := s.Store.CreateDecision(ctx, store.Decision{RunID: a.ID, Question: "May I widen the scope to add docs/y.md?", State: store.DecisionOpen})
	var out store.Decision
	if code := call(t, ts, person, "POST", api.PathDecisionAnswer(d.ID), api.Answer{Answer: "Yes"}, &out); code != http.StatusOK || out.State != store.DecisionAnswered {
		t.Fatalf("answer: %d %s", code, out.State)
	}
	if l, _ := s.Store.Lane(ctx, la.ID); strings.Join(l.Scope, ",") != "a/**" {
		t.Errorf("scope %v", l.Scope)
	}
	items, _ := s.Store.Feed(ctx, 0, 100)
	found := false
	for _, it := range items {
		found = found || strings.Contains(it.Text, "test refusal")
	}
	if !found {
		t.Error("no refusal line in feed")
	}
}
