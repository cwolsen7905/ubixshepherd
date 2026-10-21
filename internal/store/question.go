package store

import (
	"errors"
	"time"
)

// Desk question states: a question the front desk asked the person is open until the
// person answers it or skips it.
const (
	QuestionOpen     = "open"
	QuestionAnswered = "answered"
	QuestionSkipped  = "skipped"
	// QuestionOutdated: the upkeep pass closed it because what it was about is closed or
	// done. It can be reopened.
	QuestionOutdated = "outdated"
)

// MaxQuestionText bounds a question's text; longer text is cut.
const MaxQuestionText = 2000

// Question is something the front desk asked the person in a reply, marked explicitly
// by the desk (never detected from the text). It waits in the "Waiting on you" list
// until cleared.
type Question struct {
	ID          int64 `json:"id"`
	WorkspaceID int64 `json:"workspace_id"`
	// Turn is the desk turn the question was asked in: the thread message it belongs to.
	Turn    int64 `json:"turn"`
	TopicID int64 `json:"topic_id,omitempty"`
	// Repo, LaneID and NextID say what the question is about: a repo's name plus a lane
	// or a next-up item. A new question has a repo and at least one of the other two.
	Repo   string `json:"repo,omitempty"`
	LaneID int64  `json:"lane_id,omitempty"`
	NextID int64  `json:"next_id,omitempty"`
	Text   string `json:"text"`
	State  string `json:"state"`
	// AnsweredTurn is the person's turn that answered it; 0 while open or when skipped.
	AnsweredTurn int64      `json:"answered_turn,omitempty"`
	Created      time.Time  `json:"created"`
	Closed       *time.Time `json:"closed,omitempty"`
}

// ErrNoAbout is why a question is refused: it names nothing it is about.
var ErrNoAbout = errors.New("a question needs a repo and a lane or a next-up item it is about")

// HasAbout reports whether the question names what it is about.
func (q Question) HasAbout() bool { return q.Repo != "" && (q.LaneID != 0 || q.NextID != 0) }
