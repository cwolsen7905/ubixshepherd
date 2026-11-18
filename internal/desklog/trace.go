package desklog

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

var planActRe = regexp.MustCompile(`^next-up item \d+ (approved|deferred|dropped):`)

// traceNext fills NextDid, NextTurn and NextAfterSec. The person's next turn for turn T
// is the next message the person wrote after T began. What they did is looked for in
// the stretch from the end of T to the end of that next turn, and an act counts for T
// only when T was the last turn to finish before it (or, for a question, T asked it),
// so a run of event turns does not each take credit for one answer.
func traceNext(recs []Record, turns []*turnData, in Input) {
	// ends is the turns' end times, in order, for "the last turn before t".
	lastBefore := func(t time.Time) int {
		i := sort.Search(len(turns), func(i int) bool { return !turns[i].end.Before(t) })
		return i - 1
	}
	// Acts, each at a time and optionally pinned to the turn it answers.
	type act struct {
		at   time.Time
		did  string
		turn int64 // 0: the latest turn before at
	}
	var acts []act
	for _, q := range in.Questions {
		if q.Closed == nil {
			continue
		}
		switch q.State {
		case store.QuestionAnswered:
			acts = append(acts, act{*q.Closed, DidQuestionAnswered, q.Turn})
		case store.QuestionSkipped:
			acts = append(acts, act{*q.Closed, DidQuestionSkipped, q.Turn})
		}
	}
	carried := map[int64]int64{} // decision id -> the newest turn that carried it as an event
	feedDecision := map[int64][]store.FeedItem{}
	for _, f := range in.Feed {
		if f.Kind == store.FeedDecision {
			feedDecision[f.Ref] = append(feedDecision[f.Ref], f)
		}
	}
	for id, items := range feedDecision {
		newest := items[len(items)-1]
		for _, t := range turns {
			if !t.start.Before(newest.Created) && strings.Contains(t.text, newest.Text) {
				carried[id] = t.id
				break
			}
		}
	}
	for _, d := range in.Decisions {
		if d.Answered != nil {
			acts = append(acts, act{*d.Answered, DidDecisionAnswered, carried[d.ID]})
		}
	}
	for _, f := range in.Feed {
		if f.Kind == store.FeedPlan && planActRe.MatchString(f.Text) {
			acts = append(acts, act{f.Created, DidNextUpChanged, 0})
		}
	}

	// The person's turns, in order.
	var person []int
	for i, r := range recs {
		if r.StartedBy == ByPerson {
			person = append(person, i)
		}
	}
	for i, r := range recs {
		t := turns[i]
		nextIdx := -1
		for _, p := range person {
			if p > i {
				nextIdx = p
				break
			}
		}
		var windowEnd time.Time // zero: open
		if nextIdx >= 0 {
			n := turns[nextIdx]
			recs[i].NextTurn = n.id
			after := n.start.Sub(t.end).Seconds()
			if after < 0 {
				after = 0
			}
			recs[i].NextAfterSec = &after
			windowEnd = n.end
		}
		did := map[string]bool{}
		for _, a := range acts {
			if a.at.Before(t.end) || (!windowEnd.IsZero() && a.at.After(windowEnd)) {
				continue
			}
			owner := a.turn
			if owner == 0 {
				if j := lastBefore(a.at); j >= 0 {
					owner = turns[j].id
				}
			}
			if owner == r.Turn {
				did[a.did] = true
			}
		}
		// A question is answered or skipped in its own turn's account even when the
		// act came later than the next turn ended.
		for _, a := range acts {
			if a.turn == r.Turn && (a.did == DidQuestionAnswered || a.did == DidQuestionSkipped) && !a.at.Before(t.end) {
				did[a.did] = true
			}
		}
		if nextIdx >= 0 && followsUp(t.replies, turns[nextIdx].text) {
			did[DidFollowUp] = true
		}
		for _, k := range []string{DidQuestionAnswered, DidQuestionSkipped, DidDecisionAnswered, DidNextUpChanged, DidFollowUp} {
			if did[k] {
				recs[i].NextDid = append(recs[i].NextDid, k)
			}
		}
		switch {
		case len(recs[i].NextDid) > 0:
		case nextIdx < 0:
			recs[i].NextDid = []string{DidNoNextTurn}
		default:
			recs[i].NextDid = []string{DidNothing}
		}
	}
}

// followsUp reports whether the person's message names an id or link the desk's reply
// had named: a question about something already reported.
func followsUp(replies []string, next string) bool {
	if len(replies) == 0 {
		return false
	}
	asked := keysOf(next)
	if len(asked) == 0 {
		return false
	}
	named := map[string]bool{}
	for _, k := range keysOf(strings.Join(replies, "\n")) {
		named[k] = true
	}
	for _, k := range asked {
		if named[k] {
			return true
		}
	}
	return false
}
