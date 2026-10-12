package desk

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// topicHeadMax caps the topics listed at the head of a turn: the open ones, newest first.
const topicHeadMax = 12

// topicLines is the open topics, newest first, one line each: id, title and what waits
// on the person. The waiting count is read from the linked decisions, requests and
// proposed items now, so it cannot go stale.
func (d *Desk) topicLines(ctx context.Context, max int) []string {
	st := d.m.o.Store
	ts, err := st.Topics(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range ts {
		if t.Status != store.TopicOpen {
			continue
		}
		if len(out) == max {
			break
		}
		waits := 0
		links, _ := st.TopicLinks(ctx, t.ID)
		for _, l := range links {
			id, _ := strconv.ParseInt(l.Ref, 10, 64)
			switch l.Kind {
			case store.TopicDecision:
				if dc, err := st.Decision(ctx, id); err == nil && dc.State == store.DecisionOpen {
					waits++
				}
			case store.TopicRequest:
				if q, err := st.Request(ctx, id); err == nil && q.State == store.RequestNeedsRouting {
					waits++
				}
			case store.TopicNextItem:
				if it, err := st.NextItem(ctx, id); err == nil && it.State == store.NextPlanned && it.Source != store.NextByPerson {
					waits++
				}
			}
		}
		line := fmt.Sprintf("#%d %s", t.ID, strings.ReplaceAll(t.Title, "\n", " "))
		if waits > 0 {
			line += fmt.Sprintf(" (%d waiting on the person)", waits)
		}
		out = append(out, line)
	}
	return out
}

// eventTopic is the topic a swarm event is already tied to, or 0: the event's run, decision
// or request is in a topic, or belongs to a lane that is. A cleared topic is not one.
func (m *Manager) eventTopic(it store.FeedItem) int64 {
	st, ctx := m.o.Store, m.ctx
	id := strconv.FormatInt(it.Ref, 10)
	var topic int64
	switch api.EventKind(it.Kind) {
	case api.EventDecisionAsked:
		if l, err := st.TopicOf(ctx, store.TopicDecision, id); err == nil {
			topic = l.TopicID
		} else if dc, err := st.Decision(ctx, it.Ref); err == nil {
			topic = store.RunTopic(ctx, st, dc.RunID)
		}
	case api.EventRequestAttention:
		if l, err := st.TopicOf(ctx, store.TopicRequest, id); err == nil {
			topic = l.TopicID
		} else if q, err := st.Request(ctx, it.Ref); err == nil {
			topic = store.RunTopic(ctx, st, q.FromRun)
		}
	default:
		topic = store.RunTopic(ctx, st, it.Ref)
	}
	if t, err := st.Topic(ctx, topic); err != nil || t.Status != store.TopicOpen {
		return 0
	}
	return topic
}

// fileEventTurn files a turn Shepherd started under the topic its events are tied to:
// the one most of them are (ties go to the most recently active), and reports the
// others. A turn with no tied event is left unfiled for the desk to name.
func (d *Desk) fileEventTurn(ctx context.Context, turnID int64, topics []int64) (int64, []int64) {
	count := map[int64]int{}
	for _, id := range topics {
		if id != 0 {
			count[id]++
		}
	}
	if len(count) == 0 {
		return 0, nil
	}
	st := d.m.o.Store
	var order []int64
	for id := range count {
		order = append(order, id)
	}
	updated := func(id int64) time.Time {
		t, _ := st.Topic(ctx, id)
		return t.Updated
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if count[a] != count[b] {
			return count[a] > count[b]
		}
		if ua, ub := updated(a), updated(b); !ua.Equal(ub) {
			return ua.After(ub)
		}
		return a < b
	})
	l := store.TopicLink{TopicID: order[0], Kind: store.TopicTurn, Ref: strconv.FormatInt(turnID, 10), By: store.TopicLinkShepherd}
	if _, err := st.LinkTopic(ctx, l); err != nil {
		d.m.o.Log.Warn("desk: file a turn under its events' topic", "turn", turnID, "topic", order[0], "err", err)
		return 0, nil
	}
	return order[0], order[1:]
}

// topicHead is what is put ahead of a turn's message so the desk files the turn: its id,
// the topic Shepherd already filed it under (for a turn the events started), and the open
// topics, so it prefers one that exists.
func (d *Desk) topicHead(ctx context.Context, t turn) string {
	var b strings.Builder
	title := func(id int64) string {
		tp, err := d.m.o.Store.Topic(ctx, id)
		if err != nil {
			return fmt.Sprintf("#%d", id)
		}
		return fmt.Sprintf("#%d %q", id, strings.ReplaceAll(tp.Title, "\n", " "))
	}
	lines := d.topicLines(ctx, topicHeadMax)
	switch {
	case t.topic != 0:
		fmt.Fprintf(&b, "[Shepherd] This turn is #%d, already filed under topic %s, the one its events are about.", t.id, title(t.topic))
		if len(t.others) > 0 {
			names := make([]string, len(t.others))
			for i, id := range t.others {
				names[i] = title(id)
			}
			fmt.Fprintf(&b, " Its events also touch %s: name them in your reply.", strings.Join(names, ", "))
		}
		b.WriteString(" Do not file it again.\n")
	case len(lines) == 0:
		// Nothing to prefer: a new topic's title files the turn, and the head need not say so.
		return ""
	case t.origin == store.DeskBySystem:
		fmt.Fprintf(&b, "[Shepherd] This turn is #%d and is not filed under a topic: topic_set with an id (or a title for a new one) files it.\n", t.id)
	default:
		fmt.Fprintf(&b, "[Shepherd] This turn is #%d: topic_set with only an id (or a title for a new topic) files it under that topic.\n", t.id)
	}
	if len(lines) > 0 {
		b.WriteString("Open topics (prefer one of these):\n- " + strings.Join(lines, "\n- ") + "\n")
	}
	if t.origin == store.DeskByHuman {
		b.WriteString("The person's message follows.\n\n")
	} else {
		b.WriteString("\n")
	}
	return b.String()
}
