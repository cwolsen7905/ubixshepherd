package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Desk questions: the desk opens one when a reply asks the person something and marks it
// answered when a main-chat reply covers it; the person answers it from the thread
// (POST /v1/desk/turn with reply_to) or skips it. Clearing is the person's: the desk
// may mark one answered only in a turn the person started.

func (s *Server) listQuestions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ws, _ := strconv.ParseInt(q.Get("workspace_id"), 10, 64)
	state := q.Get("state")
	switch state {
	case "", store.QuestionOpen, store.QuestionAnswered, store.QuestionSkipped:
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("state is open, answered or skipped, not %q", state))
		return
	}
	qs, err := s.Store.Questions(r.Context(), ws, state)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.QuestionList{Questions: qs})
}

func (s *Server) openQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req api.QuestionOpen
	if !decode(w, r, &req) {
		return
	}
	req.Text = redact.String(req.Text)
	d, ok := s.deskFor(w, r, req.WorkspaceID)
	if !ok {
		return
	}
	turn := req.Turn
	if turn == 0 {
		turn = principalOf(ctx).Turn
	}
	topic := req.TopicID
	if topic == 0 && turn != 0 {
		if l, err := s.Store.TopicOf(ctx, store.TopicTurn, strconv.FormatInt(turn, 10)); err == nil {
			topic = l.TopicID
		}
	}
	q, err := s.Store.AddQuestion(ctx, store.Question{WorkspaceID: d.Workspace().ID, Turn: turn, TopicID: topic, Text: req.Text})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (s *Server) questionID(w http.ResponseWriter, r *http.Request) (store.Question, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad question id"))
		return store.Question{}, false
	}
	q, err := s.Store.Question(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no question %d", id))
		return q, false
	}
	if err != nil {
		s.fail(w, err)
		return q, false
	}
	return q, true
}

var errQuestionNotPersons = errors.New("the front desk may clear a question only in a turn the person started")

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q, ok := s.questionID(w, r)
	if !ok {
		return
	}
	var req api.QuestionAnswer
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	p := principalOf(ctx)
	if p.Role == RoleDesk {
		if !p.Human {
			writeError(w, http.StatusForbidden, errQuestionNotPersons)
			return
		}
		// The turn in progress is the person's reply: the desk cannot name another.
		req.AnsweredTurn = p.Turn
	}
	c, err := s.Store.CloseQuestion(ctx, q.ID, store.QuestionAnswered, req.AnsweredTurn)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) skipQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q, ok := s.questionID(w, r)
	if !ok {
		return
	}
	if !personsNext(r) {
		writeError(w, http.StatusForbidden, errQuestionNotPersons)
		return
	}
	c, err := s.Store.CloseQuestion(ctx, q.ID, store.QuestionSkipped, 0)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	// The skip is recorded in the thread and told to the desk at its next turn.
	if wss, err := s.Store.Workspaces(ctx); err == nil {
		for _, ws := range wss {
			if ws.ID != q.WorkspaceID {
				continue
			}
			d, err := s.desks.For(ws)
			if err == nil {
				err = d.Acted(ctx, fmt.Sprintf("skipped your question %d without an answer: %s", c.ID, oneLine(c.Text)))
			}
			if err != nil {
				s.Log.Warn("record a skipped question", "question", c.ID, "err", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, c)
}
