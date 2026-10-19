package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestDeskQuestionsLifecycle(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, err := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The desk, in a turn the system started, opens two questions in turn 11.
	system, _ := s.MintDeskTurn(false, "", 11)
	var q1, q2 store.Question
	if code := call(t, ts, system, "POST", api.PathQuestions, api.QuestionOpen{Text: "Ship the config change?"}, &q1); code != http.StatusCreated {
		t.Fatalf("open: %d", code)
	}
	if q1.Turn != 11 || q1.WorkspaceID != ws.ID || q1.State != store.QuestionOpen {
		t.Errorf("opened: %+v", q1)
	}
	if code := call(t, ts, system, "POST", api.PathQuestions, api.QuestionOpen{Text: "And the docs?"}, &q2); code != http.StatusCreated {
		t.Fatalf("open 2: %d", code)
	}
	if code := call(t, ts, system, "POST", api.PathQuestions, api.QuestionOpen{Text: "  "}, nil); code != http.StatusBadRequest {
		t.Errorf("empty question: %d", code)
	}

	// They join the one Waiting list, as their own kind.
	var w api.Waiting
	call(t, ts, s.Token, "GET", api.PathWaiting, nil, &w)
	if w.Count != 2 || w.Counts.Questions != 2 || w.Items[0].Kind != api.WaitingQuestion || w.Items[0].Turn != 11 || w.Items[0].Action != "reply" {
		t.Errorf("waiting: %+v", w)
	}
	if line := w.Line(); !strings.Contains(line, "[Waiting on you (2)] question 1: Ship the config change?") {
		t.Errorf("line: %s", line)
	}

	// A desk turn the system started may neither answer nor skip.
	if code := call(t, ts, system, "POST", api.PathQuestion(q1.ID)+"/answer", api.QuestionAnswer{}, nil); code != http.StatusForbidden {
		t.Errorf("system answer: %d", code)
	}
	if code := call(t, ts, system, "POST", api.PathQuestion(q1.ID)+"/skip", struct{}{}, nil); code != http.StatusForbidden {
		t.Errorf("system skip: %d", code)
	}

	// In a turn the person started, the desk marks it answered by that turn, and can
	// not name another.
	human, _ := s.MintDeskTurn(true, "", 14)
	var got store.Question
	if code := call(t, ts, human, "POST", api.PathQuestion(q1.ID)+"/answer", api.QuestionAnswer{AnsweredTurn: 3}, &got); code != http.StatusOK {
		t.Fatalf("answer: %d", code)
	}
	if got.State != store.QuestionAnswered || got.AnsweredTurn != 14 {
		t.Errorf("answered: %+v", got)
	}
	if code := call(t, ts, human, "POST", api.PathQuestion(q1.ID)+"/answer", api.QuestionAnswer{}, nil); code != http.StatusConflict {
		t.Errorf("answer twice: %d", code)
	}

	// The person skips the other; it is recorded in the thread.
	if code := call(t, ts, s.Token, "POST", api.PathQuestion(q2.ID)+"/skip", struct{}{}, &got); code != http.StatusOK || got.State != store.QuestionSkipped {
		t.Fatalf("skip: %d %+v", code, got)
	}
	es, _ := s.Store.DeskHistory(ctx, ws.ID, 0, 10)
	if len(es) == 0 || es[len(es)-1].Kind != store.DeskAction || !strings.Contains(es[len(es)-1].Text, "skipped your question 2") {
		t.Errorf("history: %+v", es)
	}
	call(t, ts, s.Token, "GET", api.PathWaiting, nil, &w)
	if w.Count != 0 {
		t.Errorf("still waiting: %+v", w)
	}
	var list api.QuestionList
	call(t, ts, s.Token, "GET", api.PathQuestions+"?state=skipped", nil, &list)
	if len(list.Questions) != 1 || list.Questions[0].ID != q2.ID {
		t.Errorf("skipped list: %+v", list)
	}
	if code := call(t, ts, s.Token, "GET", api.PathQuestions+"?state=x", nil, nil); code != http.StatusBadRequest {
		t.Errorf("bad state: %d", code)
	}

	// A worker's token reaches none of it.
	if code := call(t, ts, "nope", "GET", api.PathQuestions, nil, nil); !denied(code) {
		t.Errorf("bad token: %d", code)
	}
}

func TestReplyToClosesTheQuestion(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, _ := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	q, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "Which repo first?"})
	other, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "And the docs?"})

	// A reply that is not a plain yes or no still closes the question it names.
	var acc api.DeskTurnAccepted
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "the app, though I am not sure yet", ReplyTo: q.ID}, &acc); code != http.StatusAccepted {
		t.Fatalf("reply: %d", code)
	}
	got, _ := s.Store.Question(ctx, q.ID)
	if got.State != store.QuestionAnswered || got.AnsweredTurn != acc.Turn || acc.Turn == 0 {
		t.Errorf("question after reply: %+v (turn %d)", got, acc.Turn)
	}
	if o, _ := s.Store.Question(ctx, other.ID); o.State != store.QuestionOpen {
		t.Errorf("another question: %+v", o)
	}
	var w api.Waiting
	call(t, ts, s.Token, "GET", api.PathWaiting, nil, &w)
	if w.Counts.Questions != 1 || w.Items[0].QuestionID != other.ID {
		t.Errorf("waiting after reply: %+v", w)
	}
	es, _ := s.Store.DeskHistory(ctx, ws.ID, 0, 10)
	if len(es) == 0 || es[0].Kind != store.DeskUser || !strings.Contains(es[0].Text, "Replying to your question 1") || !strings.HasSuffix(es[0].Text, "the app, though I am not sure yet") {
		t.Errorf("person's turn: %+v", es)
	}
	// Once closed, a further reply, a skip or an unknown id is refused.
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "again", ReplyTo: q.ID}, nil); code != http.StatusConflict {
		t.Errorf("reply to a closed question: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "x", ReplyTo: 99}, nil); code != http.StatusNotFound {
		t.Errorf("reply to nothing: %d", code)
	}
	// A refused reply closes nothing.
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "  ", ReplyTo: other.ID}, nil); code != http.StatusBadRequest {
		t.Errorf("empty reply: %d", code)
	}
	if o, _ := s.Store.Question(ctx, other.ID); o.State != store.QuestionOpen {
		t.Errorf("after an empty reply: %+v", o)
	}
}

func TestConcurrentRepliesReachTheDeskOnce(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, _ := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	q, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "Ship it?"})

	// The goroutines only report: call would t.Fatal off the test goroutine.
	body, _ := json.Marshal(api.DeskTurn{Text: "yes", ReplyTo: q.ID})
	codes := make(chan int, 6)
	for i := 0; i < 6; i++ {
		go func() {
			req, _ := http.NewRequest("POST", ts.URL+api.PathDeskTurn, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+s.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- -1
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}()
	}
	accepted := 0
	for i := 0; i < 6; i++ {
		if <-codes == http.StatusAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("%d replies accepted, want 1", accepted)
	}
}

func TestReplyAfterASkipIsRefused(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, _ := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	q, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "Ship it?"})
	// A skip that came first leaves a later reply refused, and no person turn reaches the desk.
	call(t, ts, s.Token, "POST", api.PathQuestion(q.ID)+"/skip", struct{}{}, nil)
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "yes", ReplyTo: q.ID}, nil); code != http.StatusConflict {
		t.Errorf("reply after skip: %d", code)
	}
	es, _ := s.Store.DeskHistory(ctx, ws.ID, 0, 10)
	for _, e := range es {
		if e.Kind == store.DeskUser {
			t.Errorf("a person turn reached the desk: %+v", e)
		}
	}
}

func TestSkipRacingAReplyClosesOnce(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, _ := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	for i := 0; i < 20; i++ {
		q, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "Ship it?"})
		reply, _ := json.Marshal(api.DeskTurn{Text: "yes", ReplyTo: q.ID})
		do := func(path string, body []byte) int {
			req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+s.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return -1
			}
			resp.Body.Close()
			return resp.StatusCode
		}
		codes := make(chan [2]int, 2)
		go func() { codes <- [2]int{0, do(api.PathDeskTurn, reply)} }()
		go func() { codes <- [2]int{1, do(api.PathQuestion(q.ID)+"/skip", []byte("{}"))} }()
		var got [2]int
		for j := 0; j < 2; j++ {
			c := <-codes
			got[c[0]] = c[1]
		}
		replied, skipped := got[0] == http.StatusAccepted, got[1] == http.StatusOK
		if replied == skipped {
			t.Fatalf("round %d: reply %d, skip %d: exactly one should win", i, got[0], got[1])
		}
		if c, _ := s.Store.Question(ctx, q.ID); (replied && c.State != store.QuestionAnswered) || (skipped && c.State != store.QuestionSkipped) {
			t.Fatalf("round %d: reply %d, skip %d, state %s", i, got[0], got[1], c.State)
		}
	}
}
