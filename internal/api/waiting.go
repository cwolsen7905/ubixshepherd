package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PathWaiting is GET /v1/waiting: everything that waits on the person's action, in one
// list. It is derived from the decisions, lanes, merge requests and Next-up items the
// daemon already holds; nothing is stored for it. The optional workspace_id narrows it
// to one workspace's lanes (Next-up items belong to none, and are always listed).
const PathWaiting = "/v1/waiting"

// The kinds of Waiting item, and the actions the person can take on them.
const (
	WaitingDecision = "decision" // an agent's ask_human, open
	WaitingMR       = "mr"       // a lane's merge request held at the require-approval job, or failed
	WaitingShip     = "ship"     // an open lane with commits not yet pushed
	WaitingNext     = "next"     // a Next-up item proposed to the person, planned
	WaitingQuestion = "question" // a question the front desk asked and marked open
	WaitingBlocked  = "blocked"  // an open lane whose last run reported blocked, with no run going
	WaitingScope    = "scope"    // an answered scope-widening decision the lane's scope does not yet show
	WaitingRequest  = "request"  // a request between lanes that Shepherd cannot route by rule
)

// WaitingItem is one thing that needs the person. Action says what they can do about it
// from the web page: answer (a decision), approve (a merge request, then Shepherd retries
// its approval job), ship (the lane's unpushed work), review (a Next-up item, on the Next
// up page), reply (a desk question, in the thread) or open (look at the lane: nothing Shepherd can do for them). A blocked lane and an
// unapplied scope widening are shown with open.
type WaitingItem struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	Repo   string `json:"repo,omitempty"`
	Lane   string `json:"lane,omitempty"`
	LaneID int64  `json:"lane_id,omitempty"`
	// DecisionID and NextID are set for their kinds; MR is the merge request's number.
	DecisionID int64 `json:"decision_id,omitempty"`
	NextID     int64 `json:"next_id,omitempty"`
	MR         int   `json:"mr,omitempty"`
	// QuestionID and Turn are set for a question: its id, and the desk turn it was asked
	// in, which is the thread message to scroll to.
	QuestionID int64 `json:"question_id,omitempty"`
	Turn       int64 `json:"turn,omitempty"`
	// RequestID is set for a request that needs routing.
	RequestID int64  `json:"request_id,omitempty"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	// Since is when it started to wait, as far as Shepherd knows.
	Since time.Time `json:"since,omitzero"`
}

// WaitingCounts is the number of items of each kind.
type WaitingCounts struct {
	Decisions int `json:"decisions"`
	MRs       int `json:"mrs"`
	Ship      int `json:"ship"`
	Next      int `json:"next"`
	Questions int `json:"questions"`
	Blocked   int `json:"blocked"`
	Scope     int `json:"scope"`
	// Requests counts requests that need routing. Older clients ignore it.
	Requests int `json:"requests"`
}

// Waiting answers GET /v1/waiting: the items, oldest first, with their counts.
type Waiting struct {
	Count  int           `json:"count"`
	Counts WaitingCounts `json:"counts"`
	Items  []WaitingItem `json:"items"`
	// Version changes whenever the list changes (an item added, removed or altered), so a
	// client can read the list once and tell cheaply whether it moved. Call Seal after
	// the list is final.
	Version string `json:"version"`
}

// Seal sets Count and Version from the items. The version is a hash of every item's
// fields, so it is stable while the list is and moves when any item does.
func (w *Waiting) Seal() {
	w.Count = len(w.Items)
	h := sha256.New()
	for _, it := range w.Items {
		b, _ := json.Marshal(it)
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	w.Version = hex.EncodeToString(h.Sum(nil))[:16]
}

// Line is the list as one tagged line for the front desk, or "" when nothing waits:
// "[Waiting on you (3)] decision 12 (lane a): ...; ..."
func (w Waiting) Line() string {
	if w.Count == 0 {
		return ""
	}
	parts := make([]string, 0, len(w.Items))
	for _, it := range w.Items {
		p := it.Label()
		if it.Detail != "" {
			p += ": " + it.Detail
		}
		parts = append(parts, p)
	}
	return fmt.Sprintf("[Waiting on you (%d)] %s", w.Count, strings.Join(parts, "; "))
}

// Label names the item the way the person and the desk refer to it.
func (it WaitingItem) Label() string {
	where := it.Lane
	if it.Repo != "" && it.Lane != "" {
		where = it.Repo + ":" + it.Lane
	}
	var s string
	switch it.Kind {
	case WaitingDecision:
		s = fmt.Sprintf("decision %d", it.DecisionID)
	case WaitingMR:
		s = fmt.Sprintf("MR !%d", it.MR)
	case WaitingShip:
		s = "ship"
	case WaitingBlocked:
		s = "blocked"
	case WaitingScope:
		s = "scope"
	case WaitingRequest:
		s = fmt.Sprintf("request %d", it.RequestID)
	case WaitingNext:
		return fmt.Sprintf("next-up item %d", it.NextID)
	case WaitingQuestion:
		return fmt.Sprintf("question %d", it.QuestionID)
	default:
		s = it.Kind
	}
	if where != "" {
		s += " (" + where + ")"
	}
	return s
}
