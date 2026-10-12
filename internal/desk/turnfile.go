package desk

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// How Shepherd filed a person's turn: the topic the person was viewing, the topic of
// something their message names, or the topic of their last turn.
const (
	fileViewing = "viewing"
	fileNamed   = "named"
	fileLast    = "last"
)

// lastTurnScan is how many of the newest desk events are read to find the person's last
// filed turn.
const lastTurnScan = 200

var (
	// mrRef is "!158", "mr 158", "MR !158" and "merge request 158". It is stripped from
	// the text before requests are matched, so "merge request 158" is not request 158.
	mrRef       = regexp.MustCompile(`(?i)(?:\bmerge request\s*|\bmr\s*)[!#]?(\d+)\b|!(\d+)\b`)
	runRef      = regexp.MustCompile(`(?i)\brun[ -]?#?(\d+)\b`)
	decisionRef = regexp.MustCompile(`(?i)\bdecision[ -]?#?(\d+)\b`)
	requestRef  = regexp.MustCompile(`(?i)\brequest[ -]?#?(\d+)\b`)
)

func refNumbers(re *regexp.Regexp, text string) []int64 {
	var out []int64
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		for _, g := range m[1:] {
			if n, err := strconv.ParseInt(g, 10, 64); err == nil && g != "" && n > 0 {
				out = append(out, n)
			}
		}
	}
	return out
}

// openTopic is id when it names an open topic, else 0.
func (d *Desk) openTopic(ctx context.Context, id int64) int64 {
	if id <= 0 {
		return 0
	}
	if t, err := d.m.o.Store.Topic(ctx, id); err == nil && t.Status == store.TopicOpen {
		return t.ID
	}
	return 0
}

// namedTopics is the open topics the lanes, runs, decisions and merge requests a message
// names are already in, each with one thing named in it. Things not in a topic, or that
// do not exist, name none.
func (d *Desk) namedTopics(ctx context.Context, text string) map[int64]string {
	st := d.m.o.Store
	found := map[int64]string{}
	add := func(topic int64, what string) {
		if topic = d.openTopic(ctx, topic); topic != 0 {
			if _, ok := found[topic]; !ok {
				found[topic] = what
			}
		}
	}
	for _, n := range refNumbers(runRef, text) {
		add(store.RunTopic(ctx, st, n), fmt.Sprintf("run %d", n))
	}
	for _, n := range refNumbers(decisionRef, text) {
		id := strconv.FormatInt(n, 10)
		if l, err := st.TopicOf(ctx, store.TopicDecision, id); err == nil {
			add(l.TopicID, "decision "+id)
		} else if dc, err := st.Decision(ctx, n); err == nil {
			add(store.RunTopic(ctx, st, dc.RunID), "decision "+id)
		}
	}
	mrs := refNumbers(mrRef, text)
	rest := mrRef.ReplaceAllString(text, " ")
	for _, n := range refNumbers(requestRef, rest) {
		id := strconv.FormatInt(n, 10)
		if l, err := st.TopicOf(ctx, store.TopicRequest, id); err == nil {
			add(l.TopicID, "request "+id)
		} else if q, err := st.Request(ctx, n); err == nil {
			add(store.RunTopic(ctx, st, q.FromRun), "request "+id)
		}
	}
	names := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._/-", r))
	}) {
		names[strings.Trim(tok, "._/-")] = true
	}
	repos, err := st.Repos(ctx, d.ws.ID)
	if err != nil {
		return found
	}
	for _, repo := range repos {
		lanes, err := st.Lanes(ctx, repo.ID)
		if err != nil {
			continue
		}
		for _, l := range lanes {
			switch {
			case names[strings.ToLower(l.Name)]:
				add(store.LaneTopic(ctx, st, l.ID), "lane "+l.Name)
			case len(mrs) > 0:
				if f, err := st.LaneForge(ctx, l.ID); err == nil && f.MR > 0 {
					for _, n := range mrs {
						if int64(f.MR) == n {
							add(store.LaneTopic(ctx, st, l.ID), fmt.Sprintf("!%d", n))
						}
					}
				}
			}
		}
	}
	return found
}

// lastTurnTopic is the topic of the person's newest filed turn, or 0 when that turn's
// topic is cleared or none is filed: a cleared topic stays cleared.
func (d *Desk) lastTurnTopic(ctx context.Context) int64 {
	st := d.m.o.Store
	es, err := st.DeskHistory(ctx, d.ws.ID, 0, lastTurnScan)
	if err != nil {
		return 0
	}
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		if e.Kind != store.DeskUser || e.Origin != store.DeskByHuman {
			continue
		}
		if l, err := st.TopicOf(ctx, store.TopicTurn, strconv.FormatInt(e.Turn, 10)); err == nil {
			return d.openTopic(ctx, l.TopicID)
		}
	}
	return 0
}

// chooseTopic applies the filing rule to a person's message, in order: the topic they
// wrote from, the one topic of what they name, the topic of their last turn. It returns
// 0 when none applies, and how the topic was chosen with what the message named.
func (d *Desk) chooseTopic(ctx context.Context, text string, viewing int64) (topic int64, how, what string) {
	if t := d.openTopic(ctx, viewing); t != 0 {
		return t, fileViewing, ""
	}
	if named := d.namedTopics(ctx, text); len(named) == 1 {
		for t, w := range named {
			return t, fileNamed, w
		}
	} else if len(named) > 1 {
		var ids []int64
		for t := range named {
			ids = append(ids, t)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		d.m.o.Log.Debug("desk: a message names things in several topics; none chosen", "topics", ids)
	}
	if t := d.lastTurnTopic(ctx); t != 0 {
		return t, fileLast, ""
	}
	return 0, "", ""
}
