package desk

import (
	"context"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"strconv"
	"strings"
	"testing"
)

func (r *rig) sendIn(t *testing.T, text string, topic int64) int64 {
	t.Helper()
	n := 0
	for _, e := range r.events(t) {
		if e.Kind == store.DeskTurnEnd {
			n++
		}
	}
	id, _, err := r.desk(t).SendIn(context.Background(), text, topic)
	if err != nil {
		t.Fatal(err)
	}
	r.waitEnded(t, n+1)
	return id
}

func (r *rig) wantFiled(t *testing.T, turn, topic int64, how string) {
	t.Helper()
	l, err := r.turnTopic(turn)
	if topic == 0 {
		if err == nil {
			t.Fatalf("turn %d filed under %+v; want unfiled", turn, l)
		}
		return
	}
	if err != nil || l.TopicID != topic || l.By != store.TopicLinkShepherd {
		t.Fatalf("turn %d link = %+v, %v; want topic %d by shepherd", turn, l, err, topic)
	}
	if log := r.claudeLog(t); !strings.Contains(log, "already filed under topic #"+strconv.FormatInt(topic, 10)) || !strings.Contains(log, how) {
		t.Errorf("the desk was not told (%q):\n%s", how, log)
	}
}

// The records rig has lane 1 (feat/login), runs 1 and 2, decision 1 and request 1, all of
// run 1's lane.
func TestPersonTurnFilingRule(t *testing.T) {
	ctx := context.Background()
	type tc struct {
		name, text string
		viewing    func(a, b int64) int64
		want       func(a, b int64) int64
		how        string
	}
	a := func(a, b int64) int64 { return a }
	b := func(a, b int64) int64 { return b }
	none := func(a, b int64) int64 { return 0 }
	cases := []tc{
		{"viewing holds when the name is in it", "what about run 2?", b, b, "was viewing"},
		{"a lane in another topic beats the view", "how is feat/login going?", b, a, "lane feat/login"},
		{"an MR in another topic beats the view", "is !158 green?", b, a, "!158"},
		{"nothing named keeps the view", "hello there", a, a, "was viewing"},
		{"two topics named keep the view", "run 2 and feat/login", a, a, "names things in several topics (lane feat/login, run 2)"},
		{"a lane name", "how is feat/login going?", nil, a, "lane feat/login"},
		{"a lane name in quotes", "check `feat/login`.", nil, a, "lane feat/login"},
		{"a run", "why did run 2 stop", nil, b, "run 2"},
		{"run with hash", "Run #2?", nil, b, "run 2"},
		{"a decision", "re decision 1: yes", nil, a, "decision 1"},
		{"a request", "request #1 is stale", nil, a, "request 1"},
		{"an MR bang", "is !158 green?", nil, a, "!158"},
		{"an MR word", "merge request 158 looks done", nil, a, "!158"},
		{"mr is not a request", "merge request 1 and !158", nil, a, "!158"},
		{"two topics fall through", "run 2 and feat/login", nil, none, ""},
		{"an unknown thing names none", "run 99", nil, none, ""},
		{"plain text names none", "hello there", nil, none, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.records(t, "x")
			ta := r.addTopic(t, "login", store.TopicLane, 1)
			tb := r.addTopic(t, "crash", store.TopicRun, 2)
			if _, err := r.st.LaneForge(ctx, 1); err != nil {
				t.Fatal(err)
			}
			f, _ := r.st.LaneForge(ctx, 1)
			f.MR = 158
			if err := r.st.PutLaneForge(ctx, f); err != nil {
				t.Fatal(err)
			}
			var view int64
			if c.viewing != nil {
				view = c.viewing(ta.ID, tb.ID)
			}
			id := r.sendIn(t, c.text, view)
			r.wantFiled(t, id, c.want(ta.ID, tb.ID), c.how)
		})
	}
}

func TestPersonTurnTakesTheLastTurnsTopic(t *testing.T) {
	r := newRig(t, nil)
	r.records(t, "x")
	ta := r.addTopic(t, "login", store.TopicLane, 1)
	first := r.sendIn(t, "start", ta.ID)
	r.wantFiled(t, first, ta.ID, "was viewing")
	second := r.sendIn(t, "and then?", 0)
	r.wantFiled(t, second, ta.ID, "last turn")
	// A turn the person's last turn stays in a cleared topic is not carried on.
	cleared := store.TopicCleared
	if _, err := r.st.UpdateTopic(context.Background(), ta.ID, storePatch(cleared)); err != nil {
		t.Fatal(err)
	}
	third := r.sendIn(t, "anything", 0)
	r.wantFiled(t, third, 0, "")
	// A cleared topic being viewed is not filed under either.
	fourth := r.sendIn(t, "hello", ta.ID)
	r.wantFiled(t, fourth, 0, "")
}

func storePatch(status string) store.TopicPatch { return store.TopicPatch{Status: &status} }
