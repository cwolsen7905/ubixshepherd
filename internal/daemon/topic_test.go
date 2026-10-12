package daemon

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Topics: the desk names and tags, the person alone renames, merges, moves and clears,
// a cleared topic says reopen? when something needs the person, and a merged id resolves.
func TestTopicsOriginRulesClearAndMerge(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	run, _, la, _ := twoRuns(t, s)
	system, _ := s.MintDesk(false, "")
	human, _ := s.MintDesk(true, "")
	worker, _ := s.MintWorker(run.ID)

	if code := call(t, ts, worker, "GET", api.PathTopics, nil, nil); !denied(code) {
		t.Errorf("worker read topics: %d", code)
	}
	var rel api.TopicView
	if code := call(t, ts, system, "POST", api.PathTopics, api.TopicAdd{Title: "release v0.3.0 glpat-AbCdEfGhIjKlMnOpQrStUv", Link: &api.TopicRef{Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10)}}, &rel); code != http.StatusOK ||
		rel.Source != store.TopicByDesk || len(rel.Links) != 1 || rel.Links[0].By != store.TopicLinkDesk || rel.Links[0].Label != la.Name {
		t.Fatalf("desk creates: %d %+v", code, rel)
	}
	if code := call(t, ts, system, "POST", api.PathTopics, api.TopicAdd{Title: rel.Title}, nil); code != http.StatusConflict {
		t.Errorf("a repeated title: %d", code)
	}
	long := make([]byte, 81)
	for i := range long {
		long[i] = 'a'
	}
	if code := call(t, ts, system, "POST", api.PathTopics, api.TopicAdd{Title: string(long)}, nil); code != http.StatusBadRequest {
		t.Errorf("a long title: %d", code)
	}
	if code := call(t, ts, system, "POST", api.PathTopics, api.TopicAdd{Title: "bad", Link: &api.TopicRef{Kind: "bogus", Ref: "1"}}, nil); code != http.StatusBadRequest {
		t.Errorf("bad kind: %d", code)
	}

	// Outside a turn the person started the desk tags and describes, nothing more.
	var post api.TopicView
	call(t, ts, system, "POST", api.PathTopics, api.TopicAdd{Title: "announcement"}, &post)
	title, cleared, open := "renamed", store.TopicCleared, store.TopicOpen
	for name, body := range map[string]api.TopicUpdate{
		"rename": {Title: &title}, "clear": {Status: &cleared}, "merge": {MergeInto: rel.ID},
		// the lane already belongs to the release topic: moving it is the person's
		"move": {Link: &api.TopicRef{Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10)}},
	} {
		if code := call(t, ts, system, "PATCH", api.PathTopic(post.ID), body, nil); code != http.StatusForbidden {
			t.Errorf("desk %s outside a human turn: %d", name, code)
		}
	}
	sum := "what we tell people"
	if code := call(t, ts, system, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{Summary: &sum}, nil); code != http.StatusOK {
		t.Errorf("desk summary: %d", code)
	}
	// The person's move sticks against the desk's later tag.
	if code := call(t, ts, human, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{Link: &api.TopicRef{Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10)}}, &post); code != http.StatusOK || len(post.Links) != 1 {
		t.Fatalf("person move: %d %+v", code, post)
	}
	if code := call(t, ts, system, "PATCH", api.PathTopic(rel.ID), api.TopicUpdate{Link: &api.TopicRef{Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10)}}, nil); code != http.StatusForbidden {
		t.Errorf("desk took back a person's link: %d", code)
	}

	// Clear, then something needs the person: the topic says reopen?.
	var v api.TopicView
	if code := call(t, ts, human, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{Status: &cleared}, &v); code != http.StatusOK || v.Status != store.TopicCleared || v.Reopen {
		t.Fatalf("clear: %d %+v", code, v)
	}
	var l api.TopicList
	call(t, ts, s.Token, "GET", api.PathTopics, nil, &l)
	if len(l.Topics) != 1 || l.Topics[0].ID != rel.ID {
		t.Fatalf("cleared topics shown by default: %+v", l)
	}
	d, err := s.Store.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "Ship it?", State: store.DecisionOpen})
	if err != nil {
		t.Fatal(err)
	}
	if code := call(t, ts, s.Token, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{Link: &api.TopicRef{Kind: store.TopicDecision, Ref: strconv.FormatInt(d.ID, 10)}}, &v); code != http.StatusOK || !v.Reopen || v.Waits != 1 {
		t.Fatalf("reopen?: %d %+v", code, v)
	}
	call(t, ts, s.Token, "GET", api.PathTopics+"?all=1", nil, &l)
	if len(l.Topics) != 2 || l.Topics[0].ID != rel.ID && l.Topics[0].Status == store.TopicCleared && l.Topics[0].Waits == 0 {
		t.Errorf("all: %+v", l)
	}
	if code := call(t, ts, human, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{Status: &open}, &v); code != http.StatusOK || v.Status != store.TopicOpen {
		t.Errorf("reopen: %d %+v", code, v)
	}

	// Merging keeps links, and the old id still resolves.
	if code := call(t, ts, human, "PATCH", api.PathTopic(post.ID), api.TopicUpdate{MergeInto: rel.ID}, &v); code != http.StatusOK || v.ID != rel.ID || len(v.Links) != 2 {
		t.Fatalf("merge: %d %+v", code, v)
	}
	if code := call(t, ts, s.Token, "GET", api.PathTopic(post.ID), nil, &v); code != http.StatusOK || v.ID != rel.ID {
		t.Errorf("old id: %d %+v", code, v)
	}
	if code := call(t, ts, s.Token, "GET", api.PathTopic(999), nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown topic: %d", code)
	}
}

// A decision held in a run of a linked lane counts toward the topic without being linked.
func TestTopicCountsDecisionsOfLinkedLanes(t *testing.T) {
	s, ts := newServer(t)
	run, _, la, _ := twoRuns(t, s)
	var v api.TopicView
	call(t, ts, s.Token, "POST", api.PathTopics, api.TopicAdd{Title: "release", Link: &api.TopicRef{Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10)}}, &v)
	if _, err := s.Store.CreateDecision(context.Background(), store.Decision{RunID: run.ID, Question: "Tag now?", State: store.DecisionOpen}); err != nil {
		t.Fatal(err)
	}
	call(t, ts, s.Token, "GET", api.PathTopic(v.ID), nil, &v)
	if v.Waits != 1 || len(v.Links) != 2 || v.Links[1].By != store.TopicLinkShepherd {
		t.Fatalf("derived decision: %+v", v)
	}
}
