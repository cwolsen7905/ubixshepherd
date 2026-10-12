package desk

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// wakeTurn wakes an attached desk with the items and returns the system turn's id once it
// has run.
func (r *rig) wakeTurn(t *testing.T, items ...store.FeedItem) int64 {
	t.Helper()
	d := r.desk(t)
	sub := d.Subscribe()
	t.Cleanup(func() { d.Unsubscribe(sub) })
	r.m.Wake(items)
	es := r.waitEnded(t, 1)
	for _, e := range es {
		if e.Kind == store.DeskSystem {
			return e.Turn
		}
	}
	t.Fatalf("no wake-up turn: %s", kinds(es))
	return 0
}

func (r *rig) addTopic(t *testing.T, title, kind string, ref int64) store.Topic {
	t.Helper()
	ctx := context.Background()
	tp, err := r.st.AddTopic(ctx, store.Topic{Title: title, Status: store.TopicOpen, Source: store.TopicByDesk})
	if err != nil {
		t.Fatal(err)
	}
	if kind != "" {
		if _, err := r.st.LinkTopic(ctx, store.TopicLink{TopicID: tp.ID, Kind: kind, Ref: strconv.FormatInt(ref, 10), By: store.TopicLinkDesk}); err != nil {
			t.Fatal(err)
		}
	}
	return tp
}

func (r *rig) turnTopic(turn int64) (store.TopicLink, error) {
	return r.st.TopicOf(context.Background(), store.TopicTurn, strconv.FormatInt(turn, 10))
}

func runItem(kind string, run int64) store.FeedItem {
	return store.FeedItem{Kind: kind, Text: "run " + strconv.FormatInt(run, 10) + " ended", Ref: run}
}

// A turn Shepherd starts for a run tied to a topic, or to a lane whose other run is,
// arrives filed under that topic by Shepherd, and the desk is told so.
func TestEventTurnFiledUnderTopicOfItsRun(t *testing.T) {
	r := newRig(t, func(o *Options) { o.Grace = time.Millisecond })
	r.records(t, "x")
	// Run 2 is tied; run 1 reaches the topic through their lane.
	tp := r.addTopic(t, "login form", store.TopicRun, 2)
	turn := r.wakeTurn(t, runItem(store.FeedRunFailed, 1))
	l, err := r.turnTopic(turn)
	if err != nil || l.TopicID != tp.ID || l.By != store.TopicLinkShepherd {
		t.Fatalf("turn link = %+v, %v; want topic %d by shepherd", l, err, tp.ID)
	}
	if log := r.claudeLog(t); !strings.Contains(log, "already filed under topic #"+strconv.FormatInt(tp.ID, 10)) {
		t.Errorf("the desk was not told:\n%s", log)
	}
}

// An event for a run in two topics' company goes to the topic most events belong to, and
// the head names the other.
func TestEventTurnWithTwoTopicsTakesTheBusier(t *testing.T) {
	r := newRig(t, func(o *Options) { o.Grace = time.Millisecond })
	r.records(t, "x")
	a := r.addTopic(t, "login form", store.TopicDecision, 1)
	b := r.addTopic(t, "crash", store.TopicRun, 2)
	turn := r.wakeTurn(t,
		store.FeedItem{Kind: store.FeedDecision, Text: "decision 1 waits", Ref: 1},
		store.FeedItem{Kind: store.FeedRequestStuck, Text: "request 1 stuck", Ref: 1}, // from run 1: its lane's topic
		runItem(store.FeedRunFailed, 2),
		runItem(store.FeedRunPassed, 2))
	l, err := r.turnTopic(turn)
	if err != nil || l.TopicID != b.ID {
		t.Fatalf("turn link = %+v, %v; want topic %d (two events)", l, err, b.ID)
	}
	if log := r.claudeLog(t); !strings.Contains(log, "also touch #"+strconv.FormatInt(a.ID, 10)) {
		t.Errorf("the other topic was not named:\n%s", log)
	}
}

// Events about nothing in a topic leave the turn unfiled for the desk to name, and a
// person's turn is never filed by Shepherd.
func TestEventTurnWithNoTopicIsLeftUnfiled(t *testing.T) {
	r := newRig(t, func(o *Options) { o.Grace = time.Millisecond })
	r.records(t, "x")
	r.addTopic(t, "something else", "", 0)
	turn := r.wakeTurn(t, runItem(store.FeedRunFailed, 1))
	if l, err := r.turnTopic(turn); err == nil {
		t.Fatalf("filed with no topic: %+v", l)
	}
	if log := r.claudeLog(t); !strings.Contains(log, "not filed under a topic") {
		t.Errorf("the desk was not told it is unfiled:\n%s", log)
	}
	id, _, err := r.desk(t).Send(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	r.waitEnded(t, 2)
	if l, err := r.turnTopic(id); err == nil {
		t.Errorf("a person's turn was filed: %+v", l)
	}
}

// A cleared topic does not take events: its turn would vanish from the thread.
func TestEventTurnSkipsClearedTopic(t *testing.T) {
	r := newRig(t, func(o *Options) { o.Grace = time.Millisecond })
	r.records(t, "x")
	tp := r.addTopic(t, "old", store.TopicRun, 1)
	cleared := store.TopicCleared
	if _, err := r.st.UpdateTopic(context.Background(), tp.ID, store.TopicPatch{Status: &cleared}); err != nil {
		t.Fatal(err)
	}
	turn := r.wakeTurn(t, runItem(store.FeedRunFailed, 1))
	if l, err := r.turnTopic(turn); err == nil {
		t.Fatalf("filed under a cleared topic: %+v", l)
	}
}
