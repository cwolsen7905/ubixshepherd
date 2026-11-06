// Package desklog derives a per-turn record of the front desk's conversation, so the
// person and the desk can study what was signal and what was noise in the thread.
//
// It is passive: it reads what the store already keeps (the desk's events, the feed,
// questions, decisions and topics) and changes nothing. Derive is a pure function of its
// Input, so the same history gives the same log.
package desklog

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Who started a turn.
const (
	ByPerson   = "person"   // the person's message
	ByEvent    = "event"    // a digest of swarm events
	ByShepherd = "shepherd" // any other message Shepherd sent the desk on its own
)

// How a reply opened.
const (
	OpenNeedsYou = "needs_you"
	OpenNothing  = "nothing_needs_you"
	OpenOther    = "other"
	OpenNone     = "none" // no reply: the turn failed or was interrupted
)

// What the person did after a turn, in NextDid.
const (
	DidQuestionAnswered = "question_answered"
	DidQuestionSkipped  = "question_skipped"
	DidDecisionAnswered = "decision_answered"
	DidNextUpChanged    = "next_up_changed"
	DidFollowUp         = "follow_up"
	DidNothing          = "nothing_traceable"
	DidNoNextTurn       = "no_next_turn"
)

// Acts are the NextDid values that count as the person acting. A follow-up is
// attention, not action, so it does not.
var Acts = map[string]bool{
	DidQuestionAnswered: true, DidQuestionSkipped: true,
	DidDecisionAnswered: true, DidNextUpChanged: true,
}

// Event is one swarm event a turn carried, matched from its line to the feed item.
type Event struct {
	Kind string `json:"kind"`
	Repo string `json:"repo,omitempty"`
	Lane string `json:"lane,omitempty"`
	Run  int64  `json:"run,omitempty"`
}

// Record is one desk turn. It is one JSONL line.
type Record struct {
	Turn      int64     `json:"turn"`
	Time      time.Time `json:"time"`
	StartedBy string    `json:"started_by"`
	Topic     string    `json:"topic,omitempty"`
	Events    []Event   `json:"events,omitempty"`
	// Waiting is the Waiting-on-you count the desk was shown; nil when the turn was
	// not shown one (a person's turn, or nothing waited).
	Waiting *int `json:"waiting,omitempty"`
	// ReplyChars counts the reply in characters (runes).
	ReplyChars int    `json:"reply_chars"`
	Opening    string `json:"opening"`
	NeedsYou   int    `json:"needs_you_items"`
	// NeedsYouRepeated counts the listed items that name an id or link an earlier
	// turn's list named.
	NeedsYouRepeated int `json:"needs_you_repeated"`
	// NeedsYouKeys are the ids and links the items name, for tracing a repeat.
	NeedsYouKeys []string `json:"needs_you_keys,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	// NextDid says what the person's next turn did; see the Did constants.
	NextDid  []string `json:"next_did"`
	NextTurn int64    `json:"next_turn,omitempty"`
	// NextAfterSec is the seconds from this turn's last event to the person's next
	// message; nil when they have not written again.
	NextAfterSec *float64 `json:"next_after_sec,omitempty"`
	// Trimmed marks a turn whose conversation was trimmed away: only what the desk
	// kept as it began is known, and the fields about its reply and what followed are
	// empty.
	Trimmed bool `json:"trimmed,omitempty"`

	// items keeps each listed item's text, for the summary; never written out.
	items []needItem
}

// Resolver names the repo, lane and run a feed item is about. It may be nil.
type Resolver func(kind string, ref int64) (repo, lane string, run int64)

// Input is everything Derive reads.
type Input struct {
	// Events is the desk's conversation, oldest first.
	Events    []store.DeskEvent
	Feed      []store.FeedItem
	Questions []store.Question
	Decisions []store.Decision
	// TurnTopic is the title of the topic a turn is filed under, by turn id.
	TurnTopic map[int64]string
	Resolve   Resolver
	// Logged is what the desk kept of each turn as it began. It wins over what is
	// derived from the conversation, and the only trace of a turn the conversation
	// no longer holds.
	Logged []store.DeskTurnLog
	// Since and Until bound the turns returned by their start; zero is unbounded.
	// History before Since still informs repeats and traces.
	Since, Until time.Time
}

type turnData struct {
	id         int64
	start, end time.Time
	origin     string
	text       string
	replies    []string
	tools      []string
	ended      bool
}

// Derive builds one Record per desk turn, oldest first.
func Derive(in Input) []Record {
	turns := groupTurns(in.Events)
	feedByText := map[string][]store.FeedItem{}
	for _, f := range in.Feed {
		feedByText[f.Text] = append(feedByText[f.Text], f)
	}
	seen := map[string]bool{}
	logged := map[int64]store.DeskTurnLog{}
	for _, l := range in.Logged {
		logged[l.Turn] = l
	}
	recs := make([]Record, 0, len(turns))
	for _, t := range turns {
		r := Record{Turn: t.id, Time: t.start, StartedBy: startedBy(t), Topic: in.TurnTopic[t.id],
			Tools: t.tools, NextDid: []string{}}
		if r.StartedBy != ByPerson {
			r.Events = eventsOf(t, feedByText, in.Resolve)
			r.Waiting = WaitingCount(t.text)
		}
		if l, ok := logged[t.id]; ok {
			// What the desk kept as the turn began beats a guess from the digest's text.
			if l.Topic != "" {
				r.Topic = l.Topic
			}
			if r.StartedBy != ByPerson {
				r.Events, r.Waiting = fromLog(l.Events), l.Waiting
			}
		}
		reply := strings.Join(t.replies, "\n")
		r.ReplyChars = len([]rune(reply))
		r.Opening = opening(reply)
		items := needsYou(reply)
		r.NeedsYou, r.items = len(items), items
		turnKeys := map[string]bool{}
		for _, it := range items {
			rep := false
			for _, k := range it.keys {
				rep = rep || seen[k]
				turnKeys[k] = true
			}
			if rep {
				r.NeedsYouRepeated++
			}
		}
		for k := range turnKeys {
			seen[k] = true
			r.NeedsYouKeys = append(r.NeedsYouKeys, k)
		}
		sort.Strings(r.NeedsYouKeys)
		recs = append(recs, r)
	}
	traceNext(recs, turns, in)
	have := map[int64]bool{}
	for _, t := range turns {
		have[t.id] = true
	}
	for _, l := range in.Logged {
		if !have[l.Turn] {
			recs = append(recs, Record{Turn: l.Turn, Time: l.Created, StartedBy: l.StartedBy, Topic: l.Topic,
				Events: fromLog(l.Events), Waiting: l.Waiting, Opening: OpenNone, NextDid: []string{}, Trimmed: true})
		}
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Turn < recs[j].Turn })
	out := recs[:0]
	for _, r := range recs {
		if (!in.Since.IsZero() && r.Time.Before(in.Since)) || (!in.Until.IsZero() && r.Time.After(in.Until)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// groupTurns collects the desk's events by turn. A turn starts at its user or system
// event; events of a turn whose start was trimmed away are dropped. Browser actions
// belong to no turn.
func groupTurns(events []store.DeskEvent) []*turnData {
	byID := map[int64]*turnData{}
	var order []*turnData
	for _, e := range events {
		switch e.Kind {
		case store.DeskUser, store.DeskSystem:
			t := &turnData{id: e.Turn, start: e.Created, end: e.Created, origin: e.Origin, text: e.Text}
			if t.id == 0 {
				t.id = e.Seq
			}
			if t.origin == "" && e.Kind == store.DeskSystem {
				t.origin = store.DeskBySystem
			}
			byID[t.id] = t
			order = append(order, t)
			continue
		case store.DeskNew, store.DeskAction:
			continue
		}
		t := byID[e.Turn]
		if t == nil {
			continue
		}
		if e.Created.After(t.end) {
			t.end = e.Created
		}
		switch e.Kind {
		case store.DeskAssistant:
			t.replies = append(t.replies, e.Text)
		case store.DeskTool:
			if n := toolName(e.Text); n != "" {
				t.tools = append(t.tools, n)
			}
		case store.DeskTurnEnd:
			t.ended = true
		}
	}
	return order
}

func startedBy(t *turnData) string { return StartedBy(t.origin, t.text) }

// StartedBy says who began a turn from its origin and the message that started it.
func StartedBy(origin, text string) string {
	switch {
	case origin != store.DeskBySystem:
		return ByPerson
	case strings.HasPrefix(text, "[Shepherd] Since your last turn:\n"):
		return ByEvent
	}
	return ByShepherd
}

// toolName is a tool call's name: the first word of the desk's short line.
func toolName(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

var waitingRe = regexp.MustCompile(`\[Waiting on you \((\d+)\)\]`)

// WaitingCount is the count in the "[Waiting on you (N)]" line of a message to the desk,
// or nil when it has none.
func WaitingCount(text string) *int {
	m := waitingRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	n, _ := strconv.Atoi(m[1])
	return &n
}

// eventsOf matches the lines of a digest to the feed items they came from, by text,
// taking for each line the newest unconsumed item that existed when the turn began.
func eventsOf(t *turnData, byText map[string][]store.FeedItem, resolve Resolver) []Event {
	const head = "[Shepherd] Since your last turn:\n"
	if !strings.HasPrefix(t.text, head) {
		return nil
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimPrefix(t.text, head), "\n") {
		if !strings.HasPrefix(line, "- ") {
			break
		}
		line = strings.TrimPrefix(line, "- ")
		ev := Event{Kind: "unknown"}
		cands := byText[line]
		for i := len(cands) - 1; i >= 0; i-- {
			if cands[i].Created.After(t.start) {
				continue
			}
			ev.Kind = cands[i].Kind
			if resolve != nil {
				ev.Repo, ev.Lane, ev.Run = resolve(cands[i].Kind, cands[i].Ref)
			}
			byText[line] = append(cands[:i:i], cands[i+1:]...)
			break
		}
		out = append(out, ev)
	}
	return out
}

func opening(reply string) string {
	first := strings.TrimSpace(reply)
	if first == "" {
		return OpenNone
	}
	first, _, _ = strings.Cut(first, "\n")
	switch first = strings.TrimSpace(first); {
	case strings.HasPrefix(first, "Needs you:"):
		return OpenNeedsYou
	case strings.HasPrefix(first, "Nothing needs you."):
		return OpenNothing
	}
	return OpenOther
}

func fromLog(es []store.DeskLogEvent) []Event {
	var out []Event
	for _, e := range es {
		out = append(out, Event(e))
	}
	return out
}
