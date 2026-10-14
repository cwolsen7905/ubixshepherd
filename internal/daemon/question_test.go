package daemon

import (
	"context"
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
	if line := w.Line(); !strings.Contains(line, "[Waiting on you (2)] question 1: question 1: Ship the config change?") {
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

func TestReplyToClearsTheQuestion(t *testing.T) {
	s, ts := newServer(t)
	ctx := context.Background()
	root, _ := paths.Canonical(t.TempDir())
	ws, _ := s.Store.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, nil)
	q, _ := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: ws.ID, Turn: 4, Text: "Which repo first?"})

	var acc api.DeskTurnAccepted
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "the app", ReplyTo: q.ID}, &acc); code != http.StatusAccepted {
		t.Fatalf("reply: %d", code)
	}
	got, _ := s.Store.Question(ctx, q.ID)
	if got.State != store.QuestionAnswered || got.AnsweredTurn != acc.Turn || acc.Turn == 0 {
		t.Errorf("question after reply: %+v (turn %d)", got, acc.Turn)
	}
	es, _ := s.Store.DeskHistory(ctx, ws.ID, 0, 10)
	if len(es) == 0 || es[0].Kind != store.DeskUser || !strings.Contains(es[0].Text, "Replying to your question 1") || !strings.HasSuffix(es[0].Text, "the app") {
		t.Errorf("person's turn: %+v", es)
	}
	// A second reply to a closed question, and one to no question, are refused.
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "again", ReplyTo: q.ID}, nil); code != http.StatusConflict {
		t.Errorf("reply to a closed question: %d", code)
	}
	if code := call(t, ts, s.Token, "POST", api.PathDeskTurn, api.DeskTurn{Text: "x", ReplyTo: 99}, nil); code != http.StatusNotFound {
		t.Errorf("reply to nothing: %d", code)
	}
}
