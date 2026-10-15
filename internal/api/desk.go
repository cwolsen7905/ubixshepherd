package api

import (
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The front desk the daemon runs: one conversation per workspace, kept in the store, that
// every client follows. Only the operator token may call these. Each takes the workspace
// as workspace_id (a query parameter, or in the body of a POST); it may be left out
// when the daemon has exactly one workspace.
//
//	POST /v1/desk/turn       the person's message: 202 with DeskTurnAccepted; 429 when
//	                         DeskQueueMax turns already wait
//	GET  /v1/desk/stream     server-sent events, see below
//	GET  /v1/desk/history    ?before=SEQ&limit=N: DeskHistory, oldest first
//	GET  /v1/desk/status     DeskStatus
//	POST /v1/desk/interrupt  stop the turn in progress: DeskInterrupted
//	POST /v1/desk/new        a new conversation: the next turn starts a new session
//
// The stream (text/event-stream) sends each stored event as
//
//	id: <seq>
//	event: <kind>
//	data: <store.DeskEvent as JSON>
//
// where kind is one of store's Desk* kinds: user, system, turn_start, assistant, tool,
// cost (the turn's own cost in dollars, as text), error, turn_end (text: done, failed,
// interrupted...), new, action. An action is something the person did from a browser
// (answered a decision, stopped a run, closed a lane, routed or closed a request,
// continued or dismissed a run): the daemon records it, it has no turn and starts none,
// and the desk is told of it at the head of its next turn. A turn's id is the seq of the user or system message that
// started it, and every event of the turn carries it with the turn's origin, human or
// system. Partial replies come as "event: partial" with no id: they are never stored,
// and the whole reply follows as an assistant event, so a client that resumes misses
// nothing but the streaming. Resume with ?after=SEQ or the Last-Event-ID header; with
// neither, the stream starts at the newest event (read the past with history). A
// comment line (": ping") is sent every 15 seconds. A client following the stream
// counts as attached for desk.wake.
const (
	PathDeskTurn      = "/v1/desk/turn"
	PathDeskStream    = "/v1/desk/stream"
	PathDeskHistory   = "/v1/desk/history"
	PathDeskStatus    = "/v1/desk/status"
	PathDeskInterrupt = "/v1/desk/interrupt"
	PathDeskNew       = "/v1/desk/new"
)

// DeskPartial is the stream's event for a piece of a reply as it streams.
const DeskPartial = "partial"

// DeskTurn is the body of POST /v1/desk/turn.
type DeskTurn struct {
	WorkspaceID int64  `json:"workspace_id,omitempty"`
	Text        string `json:"text"`
	// TopicID is the topic the person wrote from (the web thread's filter), 0 for none.
	// Shepherd files the turn under it before the desk sees the turn.
	TopicID int64 `json:"topic_id,omitempty"`
	// ReplyTo is the id of an open desk question this message answers (the thread's
	// "Reply here"): the turn is sent as usual and the question is then marked answered
	// by it. Unknown or already closed questions are refused before anything is sent.
	ReplyTo int64 `json:"reply_to,omitempty"`
	// About is the entity the message is about (the inspector's composer): the desk is
	// told of it at the head of the turn so it can name it.
	About *DeskAbout `json:"about,omitempty"`
}

// DeskAboutKinds are the entity kinds a turn can be about.
var DeskAboutKinds = []string{"lane", "run", "decision", "question", "request", "artifact", "topic"}

// DeskAboutMaxIDs and DeskAboutMaxLabel bound a DeskAbout.
const (
	DeskAboutMaxIDs   = 20
	DeskAboutMaxLabel = 200
)

// DeskAbout names the entity a desk turn is about: a kind and the ids, with the words
// the person knows it by.
type DeskAbout struct {
	Kind  string  `json:"kind"`
	IDs   []int64 `json:"ids"`
	Label string  `json:"label,omitempty"`
}

// Validate refuses an unknown kind, no ids or too many, a non-positive id or a long label.
func (a DeskAbout) Validate() error {
	known := false
	for _, k := range DeskAboutKinds {
		known = known || k == a.Kind
	}
	switch {
	case !known:
		return fmt.Errorf("about.kind is one of %s, not %q", strings.Join(DeskAboutKinds, ", "), a.Kind)
	case len(a.IDs) == 0 || len(a.IDs) > DeskAboutMaxIDs:
		return fmt.Errorf("about.ids takes 1 to %d ids", DeskAboutMaxIDs)
	case len([]rune(a.Label)) > DeskAboutMaxLabel:
		return fmt.Errorf("about.label is at most %d characters", DeskAboutMaxLabel)
	}
	for _, id := range a.IDs {
		if id <= 0 {
			return fmt.Errorf("about.ids holds positive ids, not %d", id)
		}
	}
	return nil
}

// Line is the reference the desk reads at the head of a turn: "[About lane 5, 6 (label)]".
// The label is redacted, its whitespace collapsed and its brackets and parentheses
// dropped, so it cannot close the reference early. The ids are the person's hint about
// the subject, not checked against the store: the desk confirms them with its read tools.
func (a DeskAbout) Line() string {
	ids := make([]string, len(a.IDs))
	for i, id := range a.IDs {
		ids[i] = fmt.Sprint(id)
	}
	line := a.Kind + " " + strings.Join(ids, ", ")
	clean := strings.NewReplacer("[", "", "]", "", "(", "", ")", "").Replace(redact.String(a.Label))
	if label := strings.Join(strings.Fields(clean), " "); label != "" {
		line += " (" + label + ")"
	}
	return "[About " + line + "]"
}

// DeskTurnAccepted answers POST /v1/desk/turn.
type DeskTurnAccepted struct {
	WorkspaceID int64 `json:"workspace_id"`
	// Turn is the turn's id: the seq of its user event.
	Turn int64 `json:"turn"`
	// Ahead is how many turns run before it, the one in progress included.
	Ahead int `json:"ahead"`
}

// DeskWorkspace is the body of POST /v1/desk/interrupt and /v1/desk/new.
type DeskWorkspace struct {
	WorkspaceID int64 `json:"workspace_id,omitempty"`
}

// DeskHistory answers GET /v1/desk/history.
type DeskHistory struct {
	Events []store.DeskEvent `json:"events"`
	// More says whether older events remain: ask again with before set to the first
	// event's seq.
	More bool `json:"more"`
}

// DeskStatus answers GET /v1/desk/status.
type DeskStatus struct {
	WorkspaceID int64 `json:"workspace_id"`
	Busy        bool  `json:"busy"`
	Queued      int   `json:"queued"`
	Attached    int   `json:"attached"`
	// Session is the agent session the next turn resumes; "" starts a new one.
	Session string `json:"session"`
	Model   string `json:"model"`
	// Wake is desk.wake: attached, always or never.
	Wake string `json:"wake"`
}

// DeskInterrupted answers POST /v1/desk/interrupt.
type DeskInterrupted struct {
	Interrupted bool `json:"interrupted"`
}
