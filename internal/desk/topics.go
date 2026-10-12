package desk

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// topicHead is the open topics put ahead of a turn's message, so the desk names the
// matter of the turn with topic_set and prefers a topic that exists.
func (d *Desk) topicHead(ctx context.Context) string {
	lines := d.topicLines(ctx, topicHeadMax)
	if len(lines) == 0 {
		return ""
	}
	return "[Shepherd] Open topics (topic_set names the matter of this turn; prefer one of these):\n- " +
		strings.Join(lines, "\n- ") + "\nThe person's message follows.\n\n"
}
