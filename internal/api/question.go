package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Desk questions (docs/desk-questions.md): what the front desk asked the person in a
// reply, marked explicitly by the desk and tracked until answered or skipped. Open ones
// are in the Waiting list.
//
//	GET  /v1/questions                ?workspace_id=&state=open|answered|skipped: QuestionList
//	POST /v1/questions                the desk opens one (QuestionOpen): 201 with the Question
//	POST /v1/questions/{id}/answer    the desk marks it answered by the turn in progress
//	                                  (QuestionAnswer): only in a turn the person started
//	POST /v1/questions/{id}/skip      the person clears it unanswered: not from the desk
//	                                  outside a turn the person started
//
// A person's reply in the thread is POST /v1/desk/turn with reply_to set to the
// question's id; that answers it.
const PathQuestions = "/v1/questions"

// PathQuestion is one question's route; add /answer or /skip.
func PathQuestion(id int64) string { return fmt.Sprintf("%s/%d", PathQuestions, id) }

// QuestionOpen is the body of POST /v1/questions. Turn is the desk turn the question is
// asked in; a desk token's own turn is used when it is 0. TopicID is the question's
// topic; the turn's topic is used when it is 0.
type QuestionOpen struct {
	WorkspaceID int64  `json:"workspace_id,omitempty"`
	Text        string `json:"text"`
	Turn        int64  `json:"turn,omitempty"`
	TopicID     int64  `json:"topic_id,omitempty"`
}

// QuestionAnswer is the body of POST /v1/questions/{id}/answer. AnsweredTurn is the
// person's turn that answered it; the desk's turn in progress when 0.
type QuestionAnswer struct {
	AnsweredTurn int64 `json:"answered_turn,omitempty"`
}

// QuestionList answers GET /v1/questions.
type QuestionList struct {
	Questions []store.Question `json:"questions"`
}

// AddQuestions puts open desk questions in the list, by the time each began to wait, and
// counts them. Questions are stored by the desk, so the collector that derives the other
// kinds leaves them to its callers.
func (w *Waiting) AddQuestions(qs []store.Question) {
	for _, q := range qs {
		if q.State != store.QuestionOpen {
			continue
		}
		title := strings.Join(strings.Fields(q.Text), " ")
		if r := []rune(title); len(r) > 160 {
			title = string(r[:159]) + "…"
		}
		w.Items = append(w.Items, WaitingItem{
			Kind: WaitingQuestion, Action: "reply", QuestionID: q.ID, Turn: q.Turn,
			Title: title, Detail: fmt.Sprintf("question %d: %s", q.ID, title), Since: q.Created,
		})
		w.Counts.Questions++
	}
	sort.SliceStable(w.Items, func(i, j int) bool { return w.Items[i].Since.Before(w.Items[j].Since) })
	w.Count = len(w.Items)
}
