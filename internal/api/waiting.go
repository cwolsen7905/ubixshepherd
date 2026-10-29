package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
	WaitingArtifact = "artifact" // a merged lane held open by files nobody said to keep, commit or discard
)

// WaitingItem is one thing that needs the person. Action says what they can do about it
// from the web page: answer (a decision), approve (a merge request, then Shepherd retries
// its approval job), ship (the lane's unpushed work), review (a Next-up item, on the Next
// up page), reply (a desk question, in the thread) or open (look at the lane: nothing Shepherd can do for them). A blocked lane, an
// unapplied scope widening and a merged lane's undecided files are shown with open.
type WaitingItem struct {
	// ID is the item's short stable id ("D410"), derived from its kind and record id by
	// StableID and set by Seal. The person and the desk answer by it.
	ID     string `json:"id,omitempty"`
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
	// RequestID is set for a request that needs routing; RunID for a blocked lane, the run
	// that reported it.
	RequestID int64  `json:"request_id,omitempty"`
	RunID     int64  `json:"run_id,omitempty"`
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
	// Artifacts counts merged lanes held open by undecided files. Older clients ignore it.
	Artifacts int `json:"artifacts"`
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
	for i := range w.Items {
		w.Items[i].ID = w.Items[i].StableID()
	}
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
		parts = append(parts, it.lineItem())
	}
	return fmt.Sprintf("[Waiting on you (%d)] %s", w.Count, strings.Join(parts, "; "))
}

// lineMax is how long one item's text may be in the desk's line.
const lineMax = 80

// lineItem is one item for the line: its label, then its text. The text is Detail, or
// Title when there is no Detail, with any leading copy of the label removed (a question's
// title often starts "question 27:", sometimes twice), markdown stripped and cut at a word.
func (it WaitingItem) lineItem() string {
	label := it.Label()
	text := it.Detail
	if text == "" {
		text = it.Title
	}
	text = plainText(text)
	plain := it.label()
	base, _, _ := strings.Cut(plain, " (")
	for stripped := true; stripped; {
		stripped = false
		for _, k := range []string{plain, base} {
			if rest, ok := strings.CutPrefix(text, k+":"); ok {
				text, stripped = strings.TrimSpace(rest), true
			}
		}
	}
	text = shorten(text, lineMax)
	if text == "" {
		return label
	}
	return label + ": " + text
}

// plainText removes what markdown would read: backticks, asterisks, brackets, a link's
// target and an autolink's angle brackets, and folds whitespace, so a cut item can never
// leave a span open. Comparison signs stay.
func plainText(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdAutolink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("`", "", "*", "", "[", "", "]", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

var mdAutolink = regexp.MustCompile(`<(https?://[^>\s]*)>`)

var mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// shorten cuts s to at most max runes at a word boundary, ending "…" when it cut.
func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max-1])
	if i := strings.LastIndexByte(cut, ' '); i > 0 && r[max-1] != ' ' {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// StableID is the item's short id, from the record the item is about so it is the same on
// every read and never names another item: D<decision>, Q<question>, R<request>,
// N<next-up item>, W<decision> (an approved scope widening), B<run> (a blocked lane, by the
// run that reported it), and per lane S<lane> (work to ship) and F<lane> (files to decide).
// A merge request is <repo>!<number>, the forge's own reference, since numbers repeat across
// repos. It is "" when the item lacks the record id it is keyed by.
func (it WaitingItem) StableID() string {
	n := func(letter string, id int64) string {
		if id == 0 {
			return ""
		}
		return fmt.Sprintf("%s%d", letter, id)
	}
	switch it.Kind {
	case WaitingDecision:
		return n("D", it.DecisionID)
	case WaitingQuestion:
		return n("Q", it.QuestionID)
	case WaitingRequest:
		return n("R", it.RequestID)
	case WaitingNext:
		return n("N", it.NextID)
	case WaitingScope:
		return n("W", it.DecisionID)
	case WaitingBlocked:
		return n("B", it.RunID)
	case WaitingShip:
		return n("S", it.LaneID)
	case WaitingArtifact:
		return n("F", it.LaneID)
	case WaitingMR:
		if it.MR == 0 {
			return ""
		}
		if it.Repo == "" {
			return fmt.Sprintf("!%d", it.MR)
		}
		return fmt.Sprintf("%s!%d", it.Repo, it.MR)
	}
	return ""
}

// Label names the item the way the person and the desk refer to it.
func (it WaitingItem) Label() string {
	label := it.label()
	if id := it.StableID(); id != "" {
		return id + " " + label
	}
	return label
}

func (it WaitingItem) label() string {
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
	case WaitingArtifact:
		s = "artifacts"
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
