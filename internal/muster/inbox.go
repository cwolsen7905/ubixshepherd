package muster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Inbox kinds of finding.
const (
	KindQuestionOutdated = "question.outdated"
	KindDecisionOutdated = "decision.outdated"
	KindQuestionSkip     = "question.skip"
	KindInboxDigest      = "inbox.digest"
	KindQuestionNoAbout  = "question.no-about"
)

// checkInbox keeps the inbox to questions that still matter:
//   - a question or decision whose lane or item is closed or done is closed as out of
//     date, with the reason, and can be reopened;
//   - ones unanswered for the digest days go into one digest finding, and a question is
//     marked skipped after the skip days more (a decision is not: an agent is waiting
//     on its answer);
//   - a question made before questions carried a link is flagged.
func checkInbox(ctx context.Context, r *run) (int, error) {
	p := r.p
	for _, k := range []string{KindQuestionOutdated, KindDecisionOutdated, KindQuestionSkip, KindInboxDigest, KindQuestionNoAbout} {
		r.ran[k] = true
	}
	mc := p.conf().Muster
	now := p.now()
	day := 24 * time.Hour
	n := 0

	qs, err := p.Store.Questions(ctx, 0, store.QuestionOpen)
	if err != nil {
		return 0, err
	}
	type waiter struct {
		kind string
		id   int64
		text string
		at   time.Time
	}
	var old []waiter
	for _, q := range qs {
		if why := p.closedAbout(ctx, q.LaneID, q.NextID); why != "" {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d", KindQuestionOutdated, q.ID), kind: KindQuestionOutdated,
				subjectKind: "question", subjectID: q.ID, repo: q.Repo,
				reason: fmt.Sprintf("question %d is out of date, %s: %s", q.ID, why, clip(q.Text)),
				fix:    FixQuestionClose, auto: true, undoable: true,
			})
			n += got
			if err != nil {
				return n, err
			}
			continue
		}
		if !q.HasAbout() {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d", KindQuestionNoAbout, q.ID), kind: KindQuestionNoAbout,
				subjectKind: "question", subjectID: q.ID,
				reason: fmt.Sprintf("question %d links to no repo and lane or item, so it cannot be told when it goes out of date: %s", q.ID, clip(q.Text)),
			})
			n += got
			if err != nil {
				return n, err
			}
		}
		if mc.Digest() > 0 && now.Sub(q.Created) >= time.Duration(mc.Digest())*day {
			old = append(old, waiter{"question", q.ID, q.Text, q.Created})
		}
		if mc.Digest() > 0 && mc.Skip() > 0 && now.Sub(q.Created) >= time.Duration(mc.Digest()+mc.Skip())*day {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d", KindQuestionSkip, q.ID), kind: KindQuestionSkip,
				subjectKind: "question", subjectID: q.ID, repo: q.Repo,
				reason: fmt.Sprintf("question %d went unanswered for %d days, so it is marked skipped: %s", q.ID, mc.Digest()+mc.Skip(), clip(q.Text)),
				fix:    FixQuestionSkip, auto: true, undoable: true,
			})
			n += got
			if err != nil {
				return n, err
			}
		}
	}

	ds, err := p.Store.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		return n, err
	}
	for _, d := range ds {
		run, err := p.Store.Run(ctx, d.RunID)
		if err != nil {
			continue
		}
		if l, err := p.Store.Lane(ctx, run.LaneID); err == nil && l.State == store.LaneClosed {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d", KindDecisionOutdated, d.ID), kind: KindDecisionOutdated,
				subjectKind: "decision", subjectID: d.ID,
				reason: fmt.Sprintf("decision %d is out of date, lane %s is closed: %s", d.ID, l.Name, clip(d.Question)),
				fix:    FixDecisionClose, auto: true, undoable: true,
			})
			n += got
			if err != nil {
				return n, err
			}
			continue
		}
		if mc.Digest() > 0 && now.Sub(d.Created) >= time.Duration(mc.Digest())*day {
			old = append(old, waiter{"decision", d.ID, d.Question, d.Created})
		}
	}

	if len(old) > 0 {
		oldest := old[0]
		var lines []string
		for _, w := range old {
			if w.at.Before(oldest.at) {
				oldest = w
			}
			lines = append(lines, fmt.Sprintf("%s %d", w.kind, w.id))
		}
		got, err := r.raise(ctx, candidate{
			key: fmt.Sprintf("%s:%s:%d", KindInboxDigest, oldest.kind, oldest.id), kind: KindInboxDigest,
			subjectKind: oldest.kind, subjectID: oldest.id,
			reason: fmt.Sprintf("%d waiting %d days or more: %s", len(old), mc.Digest(), strings.Join(lines, ", ")),
		})
		n += got
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// closedAbout says why what a question is about is over: its lane is closed or its item
// is done or dropped. Empty when it is still live, or links to nothing.
func (p *Pass) closedAbout(ctx context.Context, laneID, nextID int64) string {
	if laneID != 0 {
		if l, err := p.Store.Lane(ctx, laneID); err == nil && l.State == store.LaneClosed {
			return fmt.Sprintf("lane %s is closed", l.Name)
		}
	}
	if nextID != 0 {
		if it, err := p.Store.NextItem(ctx, nextID); err == nil && (it.State == store.NextDone || it.State == store.NextDropped) {
			return fmt.Sprintf("next-up item %d is %s", it.ID, it.State)
		}
	}
	return ""
}

// clip is a question's text cut to a line for a reason.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		return string(r[:117]) + "..."
	}
	return s
}
