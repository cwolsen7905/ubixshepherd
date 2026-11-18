package desklog

import (
	"sort"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/redact"
)

// Day is one day's totals. Days are in the time zone of the record's time.
type Day struct {
	Day         string `json:"day"`
	Turns       int    `json:"turns"`
	PersonTurns int    `json:"person_turns"`
	EventTurns  int    `json:"event_turns"`
	OtherTurns  int    `json:"shepherd_turns"`
	ReplyChars  int    `json:"reply_chars"`
}

// Repeated is a Needs-you item that came back in more than one turn, named by an id or
// link it carries.
type Repeated struct {
	Key   string `json:"key"`
	Turns int    `json:"turns"`
	// Sample is the item as the desk last worded it, redacted and shortened.
	Sample string `json:"sample"`
}

// KindStat is how an event kind fared: the turns that carried it, and how many of them
// led to no action.
type KindStat struct {
	Kind     string `json:"kind"`
	Turns    int    `json:"turns"`
	NoAction int    `json:"no_action"`
}

// Summary is the short read of a log.
type Summary struct {
	Days []Day `json:"days"`
	// EventTurns counts event turns whose next person turn is known (Judged) and how
	// many of those led to no action. NoActionShare is NoAction over Judged, 0 when
	// none was judged. A turn after which the person has not written yet, and a trimmed one, is not judged.
	EventTurns    int     `json:"event_turns"`
	Judged        int     `json:"event_turns_judged"`
	NoAction      int     `json:"event_turns_no_action"`
	NoActionShare float64 `json:"no_action_share"`
	// MostRepeated are the Needs-you items listed in the most turns, at least two.
	MostRepeated []Repeated `json:"most_repeated"`
	// NoActionKinds are the event kinds most often in a turn that led to no action.
	NoActionKinds []KindStat `json:"no_action_kinds"`
}

const topN = 10

// Summarise totals records. It needs the records Derive made: the item texts do not
// survive a round trip through JSON.
func Summarise(recs []Record) Summary {
	var s Summary
	days := map[string]*Day{}
	type rep struct {
		turns  int
		sample string
	}
	reps := map[string]*rep{}
	kinds := map[string]*KindStat{}
	for _, r := range recs {
		key := r.Time.Format("2006-01-02")
		d := days[key]
		if d == nil {
			d = &Day{Day: key}
			days[key] = d
		}
		d.Turns++
		d.ReplyChars += r.ReplyChars
		switch r.StartedBy {
		case ByPerson:
			d.PersonTurns++
		case ByEvent:
			d.EventTurns++
		default:
			d.OtherTurns++
		}
		for _, it := range r.items {
			if len(it.keys) == 0 {
				continue
			}
			// An item is counted once, under its first key (ids sort before links).
			k := it.keys[0]
			x := reps[k]
			if x == nil {
				x = &rep{}
				reps[k] = x
			}
			x.turns++
			x.sample = it.text
		}
		if r.StartedBy != ByEvent {
			continue
		}
		s.EventTurns++
		if r.Trimmed || hasDid(r.NextDid, DidNoNextTurn) {
			continue
		}
		s.Judged++
		none := !actedOn(r.NextDid)
		if none {
			s.NoAction++
		}
		seen := map[string]bool{}
		for _, e := range r.Events {
			if seen[e.Kind] {
				continue
			}
			seen[e.Kind] = true
			k := kinds[e.Kind]
			if k == nil {
				k = &KindStat{Kind: e.Kind}
				kinds[e.Kind] = k
			}
			k.Turns++
			if none {
				k.NoAction++
			}
		}
	}
	if s.Judged > 0 {
		s.NoActionShare = float64(s.NoAction) / float64(s.Judged)
	}
	for _, d := range days {
		s.Days = append(s.Days, *d)
	}
	sort.Slice(s.Days, func(i, j int) bool { return s.Days[i].Day < s.Days[j].Day })
	s.MostRepeated = []Repeated{}
	for k, x := range reps {
		if x.turns >= 2 {
			s.MostRepeated = append(s.MostRepeated, Repeated{Key: k, Turns: x.turns, Sample: clip(redact.String(x.sample), 120)})
		}
	}
	sort.Slice(s.MostRepeated, func(i, j int) bool {
		a, b := s.MostRepeated[i], s.MostRepeated[j]
		if a.Turns != b.Turns {
			return a.Turns > b.Turns
		}
		return a.Key < b.Key
	})
	if len(s.MostRepeated) > topN {
		s.MostRepeated = s.MostRepeated[:topN]
	}
	s.NoActionKinds = []KindStat{}
	for _, k := range kinds {
		if k.NoAction > 0 {
			s.NoActionKinds = append(s.NoActionKinds, *k)
		}
	}
	sort.Slice(s.NoActionKinds, func(i, j int) bool {
		a, b := s.NoActionKinds[i], s.NoActionKinds[j]
		if a.NoAction != b.NoAction {
			return a.NoAction > b.NoAction
		}
		return a.Kind < b.Kind
	})
	if len(s.NoActionKinds) > topN {
		s.NoActionKinds = s.NoActionKinds[:topN]
	}
	return s
}

func hasDid(did []string, want string) bool {
	for _, d := range did {
		if d == want {
			return true
		}
	}
	return false
}

func actedOn(did []string) bool {
	for _, d := range did {
		if Acts[d] {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
