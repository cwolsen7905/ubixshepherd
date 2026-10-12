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

// A desk token minted for a turn files that turn under the topic the tool names, with
// only an id or a new title; another turn is the person's to file, and a token with no
// turn has nothing to file.
func TestTopicSetFilesTheDesksCurrentTurn(t *testing.T) {
	s, ts := newServer(t)
	twoRuns(t, s)
	turn := func(n int64) string { return strconv.FormatInt(n, 10) }
	desk, _ := s.MintDeskTurn(true, "s", 41)
	noTurn, _ := s.MintDesk(false, "")

	var a api.TopicView
	if code := call(t, ts, desk, "POST", api.PathTopics, api.TopicAdd{Title: "release"}, &a); code != http.StatusOK || len(a.Links) != 1 || a.Links[0].Kind != store.TopicTurn || a.Links[0].Ref != turn(41) {
		t.Fatalf("a new title files the turn: %d %+v", code, a)
	}
	var b api.TopicView
	call(t, ts, noTurn, "POST", api.PathTopics, api.TopicAdd{Title: "docs"}, &b)
	if len(b.Links) != 0 {
		t.Fatalf("a token with no turn linked something: %+v", b)
	}
	if code := call(t, ts, noTurn, "PATCH", api.PathTopic(b.ID), api.TopicUpdate{}, nil); code != http.StatusBadRequest {
		t.Errorf("nothing to tie and no turn: %d", code)
	}
	// Another turn, started later, files with the id alone.
	later, _ := s.MintDeskTurn(true, "s", 42)
	var got api.TopicView
	if code := call(t, ts, later, "PATCH", api.PathTopic(a.ID), api.TopicUpdate{}, &got); code != http.StatusOK || len(got.Links) != 2 {
		t.Fatalf("id alone files the turn: %d %+v", code, got)
	}
	// A turn already in a topic stays: moving it is the person's, and the call is not an error.
	if code := call(t, ts, later, "PATCH", api.PathTopic(b.ID), api.TopicUpdate{}, &got); code != http.StatusOK || len(got.Links) != 0 {
		t.Errorf("the desk moved a filed turn: %d %+v", code, got)
	}
	// Naming another turn is refused for the desk, allowed for the person.
	other := api.TopicUpdate{Link: &api.TopicRef{Kind: store.TopicTurn, Ref: turn(7)}}
	if code := call(t, ts, later, "PATCH", api.PathTopic(b.ID), other, nil); code != http.StatusForbidden {
		t.Errorf("the desk filed another turn: %d", code)
	}
	if code := call(t, ts, s.Token, "PATCH", api.PathTopic(b.ID), other, nil); code != http.StatusOK {
		t.Errorf("the person files a turn: %d", code)
	}
}

// A lane with no link takes the topic of the next-up item it was opened for, or of a run
// tied to it, and shows in that topic's links.
func TestLaneTakesTopicFromItemOrRun(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	ra, _, la, lb := twoRuns(t, s)
	item, err := s.Store.AddNextItem(ctx, store.NextItem{Title: "login", Source: store.NextByPerson})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.UpdateNextItem(ctx, item.ID, store.NextPatch{LaneID: &la.ID}); err != nil {
		t.Fatal(err)
	}
	tp, _ := s.Store.AddTopic(ctx, store.Topic{Title: "login", Status: store.TopicOpen, Source: store.TopicByDesk})
	s.Store.LinkTopic(ctx, store.TopicLink{TopicID: tp.ID, Kind: store.TopicNextItem, Ref: strconv.FormatInt(item.ID, 10), By: store.TopicLinkDesk})
	other, _ := s.Store.AddTopic(ctx, store.Topic{Title: "crash", Status: store.TopicOpen, Source: store.TopicByDesk})
	rb, _ := s.Store.Runs(ctx, lb.ID, "", 1)
	s.Store.LinkTopic(ctx, store.TopicLink{TopicID: other.ID, Kind: store.TopicRun, Ref: strconv.FormatInt(rb[0].ID, 10), By: store.TopicLinkDesk})
	_ = ra

	hasLane := func(id int64, lane store.Lane) bool {
		var v api.TopicView
		if code := call(t, ts, s.Token, "GET", api.PathTopic(id), nil, &v); code != http.StatusOK {
			t.Fatalf("get topic: %d", code)
		}
		for _, l := range v.Links {
			if l.Kind == store.TopicLane && l.Ref == strconv.FormatInt(lane.ID, 10) && l.Label == lane.Name {
				return true
			}
		}
		return false
	}
	if !hasLane(tp.ID, la) || hasLane(other.ID, la) {
		t.Errorf("lane a should be in the item's topic only")
	}
	if !hasLane(other.ID, lb) || hasLane(tp.ID, lb) {
		t.Errorf("lane b should be in its run's topic only")
	}
	// A lane's own link wins over what it would take.
	s.Store.LinkTopic(ctx, store.TopicLink{TopicID: other.ID, Kind: store.TopicLane, Ref: strconv.FormatInt(la.ID, 10), By: store.TopicLinkPerson})
	if hasLane(tp.ID, la) || !hasLane(other.ID, la) {
		t.Errorf("a lane's own link should win")
	}
}

// A turn Shepherd filed by its rule is the desk's to re-file when the person clearly
// changed the subject, in a turn the person started; one the person filed, or one of a
// turn Shepherd started, is not.
func TestDeskRefilesATurnShepherdFiled(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	a, _ := s.Store.AddTopic(ctx, store.Topic{Title: "login", Status: store.TopicOpen, Source: store.TopicByPerson})
	b, _ := s.Store.AddTopic(ctx, store.Topic{Title: "release", Status: store.TopicOpen, Source: store.TopicByPerson})
	file := func(turn int64, by string) {
		t.Helper()
		if _, err := s.Store.LinkTopic(ctx, store.TopicLink{TopicID: a.ID, Kind: store.TopicTurn, Ref: strconv.FormatInt(turn, 10), By: by}); err != nil {
			t.Fatal(err)
		}
	}
	at := func(turn int64) int64 {
		l, err := s.Store.TopicOf(ctx, store.TopicTurn, strconv.FormatInt(turn, 10))
		if err != nil {
			t.Fatal(err)
		}
		return l.TopicID
	}
	file(51, store.TopicLinkShepherd)
	human, _ := s.MintDeskTurn(true, "s", 51)
	if code := call(t, ts, human, "PATCH", api.PathTopic(b.ID), api.TopicUpdate{}, nil); code != http.StatusOK || at(51) != b.ID {
		t.Errorf("id alone did not re-file: %d, topic %d", code, at(51))
	}
	file(52, store.TopicLinkShepherd)
	human, _ = s.MintDeskTurn(true, "s", 52)
	link := api.TopicUpdate{Link: &api.TopicRef{Kind: store.TopicTurn, Ref: "52"}}
	if code := call(t, ts, human, "PATCH", api.PathTopic(b.ID), link, nil); code != http.StatusOK || at(52) != b.ID {
		t.Errorf("a turn link did not re-file: %d, topic %d", code, at(52))
	}
	file(53, store.TopicLinkPerson)
	human, _ = s.MintDeskTurn(true, "s", 53)
	call(t, ts, human, "PATCH", api.PathTopic(b.ID), api.TopicUpdate{}, nil)
	if at(53) != a.ID {
		t.Errorf("the desk moved the person's filing: topic %d", at(53))
	}
	file(54, store.TopicLinkShepherd)
	system, _ := s.MintDeskTurn(false, "s", 54)
	call(t, ts, system, "PATCH", api.PathTopic(b.ID), api.TopicUpdate{}, nil)
	if at(54) != a.ID {
		t.Errorf("the desk moved an event turn: topic %d", at(54))
	}
}
