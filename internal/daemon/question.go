package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

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
	qs, err := s.db(r.Context()).Questions(r.Context(), ws, state)
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
		if l, err := s.db(ctx).TopicOf(ctx, store.TopicTurn, strconv.FormatInt(turn, 10)); err == nil {
			topic = l.TopicID
		}
	}
	// The links must exist, and a repo given with them must be theirs: the repo is
	// taken from the lane or the item when not given.
	repo := req.Repo
	link := func(what string, id int64, name string) bool {
		if name == "" {
			return true
		}
		if repo == "" {
			repo = name
		}
		if repo != name {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%s %d is in repo %q, not %q", what, id, name, repo))
			return false
		}
		return true
	}
	if req.LaneID != 0 {
		l, err := s.db(ctx).Lane(ctx, req.LaneID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no lane %d", req.LaneID))
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		r, err := s.db(ctx).Repo(ctx, l.RepoID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("lane %d has no repo", l.ID))
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if r.WorkspaceID != d.Workspace().ID {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no lane %d in this workspace", l.ID))
			return
		}
		if !link("lane", l.ID, r.Name) {
			return
		}
	}
	if req.NextID != 0 {
		it, err := s.db(ctx).NextItem(ctx, req.NextID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no next-up item %d", req.NextID))
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if it.LaneID != 0 && req.LaneID != 0 && it.LaneID != req.LaneID {
			writeError(w, http.StatusBadRequest, fmt.Errorf("next-up item %d is linked to lane %d, not lane %d", it.ID, it.LaneID, req.LaneID))
			return
		}
		if it.Repo != "" {
			repos, err := s.db(ctx).Repos(ctx, d.Workspace().ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			found := false
			for _, r := range repos {
				found = found || r.Name == it.Repo
			}
			if !found {
				writeError(w, http.StatusBadRequest, fmt.Errorf("no next-up item %d in this workspace", it.ID))
				return
			}
		}
		if !link("next-up item", it.ID, it.Repo) {
			return
		}
	}
	// Whatever repo the question ends up with, a standalone one included, is one of
	// the desk's workspace.
	if repo != "" {
		repos, err := s.db(ctx).Repos(ctx, d.Workspace().ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		found := false
		for _, r := range repos {
			found = found || r.Name == repo
		}
		if !found {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no repo %q in this workspace", repo))
			return
		}
	}
	q, err := s.db(ctx).AddQuestion(ctx, store.Question{WorkspaceID: d.Workspace().ID, Turn: turn, TopicID: topic, Text: req.Text,
		Repo: repo, LaneID: req.LaneID, NextID: req.NextID})
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
	q, err := s.db(r.Context()).Question(r.Context(), id)
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
	// Ordered with a reply (see deskTurn): a question is closed by one of them, once.
	defer s.questionMu.lock(q.ID)()
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
	c, err := s.db(ctx).CloseQuestion(ctx, q.ID, store.QuestionAnswered, req.AnsweredTurn)
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
	// Ordered with a reply (see deskTurn): a question is closed by one of them, once.
	defer s.questionMu.lock(q.ID)()
	if !personsNextGate(r) {
		writeError(w, http.StatusForbidden, errQuestionNotPersons)
		return
	}
	c, err := s.db(ctx).CloseQuestion(ctx, q.ID, store.QuestionSkipped, 0)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	// The skip is recorded in the thread and told to the desk at its next turn.
	if wss, err := s.db(ctx).Workspaces(ctx); err == nil {
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

// keyedMutex is a mutex per key, held only while a key is in use.
type keyedMutex struct {
	mu sync.Mutex
	m  map[int64]*keyLock
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

// lock takes the key's mutex and returns the function that releases it.
func (k *keyedMutex) lock(key int64) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[int64]*keyLock{}
	}
	l := k.m[key]
	if l == nil {
		l = &keyLock{}
		k.m[key] = l
	}
	l.refs++
	k.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}
