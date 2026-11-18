package desklog

import (
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

type conv struct {
	seq int64
	evs []store.DeskEvent
}

func (c *conv) add(kind string, turn int64, origin, text string, min int) int64 {
	c.seq++
	if turn == 0 && (kind == store.DeskUser || kind == store.DeskSystem) {
		turn = c.seq
	}
	c.evs = append(c.evs, store.DeskEvent{Seq: c.seq, Kind: kind, Text: text, Turn: turn, Origin: origin, Created: at(min)})
	return turn
}

const digestText = "[Shepherd] Since your last turn:\n- lane a: run 4 failed\n- MR !12 is ready\n- decision 9 asked\n\nreact\n[Waiting on you (3)] decision 9"

func scenario() Input {
	c := &conv{}
	t1 := c.add(store.DeskSystem, 0, store.DeskBySystem, digestText, 0)
	c.add(store.DeskTool, t1, store.DeskBySystem, "next_list", 1)
	c.add(store.DeskTool, t1, store.DeskBySystem, "lane_show a", 1)
	c.add(store.DeskAssistant, t1, store.DeskBySystem, "Needs you:\n1. Approve [!12](https://x/mr/12): green.\n2. Decide 9 (#9).\n\nStatus:\n- lane a failed", 2)
	c.add(store.DeskTurnEnd, t1, store.DeskBySystem, "", 2)
	// Person answers decision 9 without writing about it, much later.
	t2 := c.add(store.DeskUser, 0, store.DeskByHuman, "what happened with lane a?", 30)
	c.add(store.DeskAssistant, t2, store.DeskByHuman, "Needs you:\n1. Approve [!12](https://x/mr/12) again.\n\nStatus:\n- ok", 31)
	t3 := c.add(store.DeskSystem, 0, store.DeskBySystem, "[Shepherd] Since your last turn:\n- nothing special", 60)
	c.add(store.DeskAssistant, t3, store.DeskBySystem, "Nothing needs you.", 61)
	t4 := c.add(store.DeskUser, 0, store.DeskByHuman, "thanks", 90)
	c.add(store.DeskAssistant, t4, store.DeskByHuman, "welcome", 91)
	ans := at(29)
	return Input{
		Events: c.evs,
		Feed: []store.FeedItem{
			{ID: 1, Kind: store.FeedRunFailed, Text: "lane a: run 4 failed", Ref: 4, Created: at(-1)},
			{ID: 2, Kind: store.FeedMR, Text: "MR !12 is ready", Ref: 1, Created: at(-1)},
			{ID: 4, Kind: store.FeedDecision, Text: "decision 9 asked", Ref: 9, Created: at(-1)},
			{ID: 3, Kind: store.FeedPlan, Text: "next-up item 5 approved: x", Ref: 5, Created: at(100)},
		},
		Decisions: []store.Decision{{ID: 9, Answered: &ans}},
		TurnTopic: map[int64]string{t1: "release"},
		Resolve: func(kind string, ref int64) (string, string, int64) {
			if kind == store.FeedRunFailed {
				return "repo", "a", ref
			}
			return "", "", 0
		},
	}
}

func TestDerive(t *testing.T) {
	recs := Derive(scenario())
	if len(recs) != 4 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.StartedBy != ByEvent || r.Topic != "release" || r.Waiting == nil || *r.Waiting != 3 {
		t.Fatalf("turn 1 header: %+v", r)
	}
	if len(r.Events) != 3 || r.Events[0] != (Event{Kind: "run_failed", Repo: "repo", Lane: "a", Run: 4}) || r.Events[1].Kind != "mr" {
		t.Fatalf("events: %+v", r.Events)
	}
	if strings.Join(r.Tools, ",") != "next_list,lane_show" || r.Opening != OpenNeedsYou || r.NeedsYou != 2 || r.NeedsYouRepeated != 0 {
		t.Fatalf("reply: %+v", r)
	}
	if r.ReplyChars == 0 || r.NextTurn != recs[1].Turn || r.NextAfterSec == nil || *r.NextAfterSec != 28*60 {
		t.Fatalf("next: %+v", r)
	}
	// The decision was answered between turn 1 and turn 2's end, before turn 3: turn 1's.
	if strings.Join(r.NextDid, ",") != DidDecisionAnswered {
		t.Fatalf("turn 1 next: %v", r.NextDid)
	}
	if recs[1].StartedBy != ByPerson || recs[1].Events != nil || recs[1].Waiting != nil {
		t.Fatalf("person turn: %+v", recs[1])
	}
	if recs[1].NeedsYou != 1 || recs[1].NeedsYouRepeated != 1 {
		t.Fatalf("repeat by link/id: %+v", recs[1])
	}
	// Turn 2 had no act of its own; turn 3 follows no one; the person said thanks.
	if strings.Join(recs[1].NextDid, ",") != DidNothing {
		t.Fatalf("turn 2 next: %v", recs[1].NextDid)
	}
	if recs[2].Opening != OpenNothing || strings.Join(recs[2].NextDid, ",") != DidNothing {
		t.Fatalf("turn 3: %+v", recs[2])
	}
	// The plan change came after the last turn: turn 4's, with no next turn of its own.
	if strings.Join(recs[3].NextDid, ",") != DidNextUpChanged || recs[3].NextAfterSec != nil {
		t.Fatalf("turn 4: %+v", recs[3])
	}
}

func TestFollowUpAndQuestions(t *testing.T) {
	c := &conv{}
	t1 := c.add(store.DeskUser, 0, store.DeskByHuman, "status?", 0)
	c.add(store.DeskAssistant, t1, store.DeskByHuman, "Needs you:\n1. Look at [!44](u).\n2. Pick A or B.\n\nStatus:\n- x", 1)
	t2 := c.add(store.DeskUser, 0, store.DeskByHuman, "why is !44 red?", 5)
	c.add(store.DeskAssistant, t2, store.DeskByHuman, "because", 6)
	closed := at(5)
	recs := Derive(Input{Events: c.evs, Questions: []store.Question{
		{ID: 1, Turn: t1, State: store.QuestionSkipped, Closed: &closed},
	}})
	if got := strings.Join(recs[0].NextDid, ","); got != DidQuestionSkipped+","+DidFollowUp {
		t.Fatalf("got %s", got)
	}
	if recs[1].NextDid[0] != DidNoNextTurn {
		t.Fatalf("got %v", recs[1].NextDid)
	}
}

func TestRunOfEventTurnsShareOneAct(t *testing.T) {
	c := &conv{}
	e1 := c.add(store.DeskSystem, 0, store.DeskBySystem, "[Shepherd] Since your last turn:\n- a", 0)
	c.add(store.DeskAssistant, e1, store.DeskBySystem, "ok", 1)
	e2 := c.add(store.DeskSystem, 0, store.DeskBySystem, "[Shepherd] Since your last turn:\n- b", 10)
	c.add(store.DeskAssistant, e2, store.DeskBySystem, "ok", 11)
	h := c.add(store.DeskUser, 0, store.DeskByHuman, "hi", 20)
	c.add(store.DeskAssistant, h, store.DeskByHuman, "hi", 21)
	ans := at(15)
	recs := Derive(Input{Events: c.evs, Decisions: []store.Decision{{ID: 1, Answered: &ans}}})
	if recs[0].NextDid[0] != DidNothing || recs[1].NextDid[0] != DidDecisionAnswered {
		t.Fatalf("%v %v", recs[0].NextDid, recs[1].NextDid)
	}
}

func TestSinceKeepsEarlierHistoryForRepeats(t *testing.T) {
	in := scenario()
	in.Since = at(20)
	recs := Derive(in)
	if len(recs) != 3 || recs[0].NeedsYouRepeated != 1 {
		t.Fatalf("%d records, first %+v", len(recs), recs[0])
	}
}

func TestSummarise(t *testing.T) {
	s := Summarise(Derive(scenario()))
	if len(s.Days) != 1 || s.Days[0].Turns != 4 || s.Days[0].PersonTurns != 2 || s.Days[0].EventTurns != 2 {
		t.Fatalf("days: %+v", s.Days)
	}
	// Two event turns: turn 1 led to an action, turn 3 to none.
	if s.EventTurns != 2 || s.Judged != 2 || s.NoAction != 1 || s.NoActionShare != 0.5 {
		t.Fatalf("share: %+v", s)
	}
	if len(s.MostRepeated) == 0 || s.MostRepeated[0].Turns != 2 || !strings.Contains(s.MostRepeated[0].Key, "12") {
		t.Fatalf("repeated: %+v", s.MostRepeated)
	}
	// Turn 3's one line matched no feed item, so it has kind unknown.
	if len(s.NoActionKinds) != 1 || s.NoActionKinds[0].Kind != "unknown" {
		t.Fatalf("kinds: %+v", s.NoActionKinds)
	}
}

func TestLoggedTurnsWinAndOutliveTheConversation(t *testing.T) {
	in := scenario()
	two := 2
	in.Logged = []store.DeskTurnLog{
		// Turn 1 is still in the conversation: the kept row beats the digest's text.
		{Turn: 1, StartedBy: ByEvent, Topic: "kept", Waiting: &two,
			Events: []store.DeskLogEvent{{Kind: "run_failed", Repo: "r", Lane: "l", Run: 9}}},
		// An older turn the conversation has trimmed away.
		{Turn: 0, StartedBy: ByEvent, Created: at(-60), Events: []store.DeskLogEvent{{Kind: "mr"}}},
	}
	in.Logged[1].Turn = -5
	recs := Derive(in)
	if len(recs) != 5 || recs[0].Turn != -5 || !recs[0].Trimmed || recs[0].Opening != OpenNone || recs[0].Events[0].Kind != "mr" {
		t.Fatalf("trimmed: %+v", recs[0])
	}
	r := recs[1]
	if r.Topic != "kept" || *r.Waiting != 2 || len(r.Events) != 1 || r.Events[0].Run != 9 || r.Trimmed {
		t.Fatalf("kept: %+v", r)
	}
	// A trimmed turn is not judged on what followed.
	if s := Summarise(recs); s.EventTurns != 3 || s.Judged != 2 {
		t.Fatalf("summary: %+v", s)
	}
}
