package muster

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Next-up kinds of finding.
const (
	KindNextNoRepo      = "next.no-repo"
	KindNextLaneClosed  = "next.lane-closed"
	KindNextImplemented = "next.implemented"
	KindNextDuplicate   = "next.duplicate"
)

// DuplicateThreshold is how alike two titles must be, by shared words, to be proposed
// as duplicates. Plain string matching, no model.
const DuplicateThreshold = 0.8

// checkNext keeps the next-up list an agenda: so the next-up list stays a true agenda:
//   - an item with no repo asks for one;
//   - an item whose lane closed without a merge asks: back to approved, or drop;
//   - an item whose cited requirements are now implemented is proposed as done;
//   - items with near-identical titles are proposed as duplicates.
func checkNext(ctx context.Context, r *run) (int, error) {
	p := r.p
	items, err := p.Store.NextItems(ctx)
	if err != nil {
		return 0, err
	}
	var live []store.NextItem
	for _, it := range items {
		if it.State != store.NextDone && it.State != store.NextDropped {
			live = append(live, it)
		}
	}
	n := 0
	for _, it := range live {
		if it.Repo == "" {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d", KindNextNoRepo, it.ID), kind: KindNextNoRepo,
				subjectKind: "next", subjectID: it.ID,
				reason: fmt.Sprintf("next-up item %d has no repo: %s. Say which repo it is for", it.ID, it.Title),
			})
			n += got
			if err != nil {
				return n, err
			}
		}
		if it.LaneID != 0 {
			l, err := p.Store.Lane(ctx, it.LaneID)
			if err == nil && l.State == store.LaneClosed {
				// With no forge record (the store answers a zero one) or one that cannot be read it is not known that the
				// lane closed unmerged, so nothing is asked.
				lf, ferr := p.Store.LaneForge(ctx, l.ID)
				if ferr == nil && (lf.MR != 0 || lf.MRState != "") && !strings.EqualFold(lf.MRState, "merged") && lf.MergeSHA == "" {
					got, err := r.raise(ctx, candidate{
						key:  fmt.Sprintf("%s:%d:%d", KindNextLaneClosed, it.ID, l.ID),
						kind: KindNextLaneClosed, subjectKind: "next", subjectID: it.ID, repo: it.Repo,
						reason: fmt.Sprintf("lane %s closed without a merge, so next-up item %d (%s) is not done: put it back to approved, or drop it", l.Name, it.ID, it.Title),
						fix:    FixNextReapprove,
					})
					n += got
					if err != nil {
						return n, err
					}
				}
			}
		}
		if p.Probe.Implemented != nil && it.State != store.NextDeferred {
			if ok, why := p.Probe.Implemented(ctx, it); ok {
				got, err := r.raise(ctx, candidate{
					key:  fmt.Sprintf("%s:%d", KindNextImplemented, it.ID),
					kind: KindNextImplemented, subjectKind: "next", subjectID: it.ID, repo: it.Repo,
					reason: fmt.Sprintf("what next-up item %d cites looks implemented (%s): mark it done? %s", it.ID, why, it.Title),
					fix:    FixNextDone,
				})
				n += got
				if err != nil {
					return n, err
				}
			}
		}
	}
	got, err := r.duplicates(ctx, live)
	return n + got, err
}

// duplicates proposes dropping the later of two items whose titles are near-identical,
// when they are for the same repo (or one has none).
func (r *run) duplicates(ctx context.Context, live []store.NextItem) (int, error) {
	words := make([]map[string]bool, len(live))
	for i, it := range live {
		words[i] = titleWords(it.Title)
	}
	n := 0
	for j := range live {
		for i := 0; i < j; i++ {
			a, b := live[i], live[j]
			if a.Repo != b.Repo && a.Repo != "" && b.Repo != "" {
				continue
			}
			if similarity(words[i], words[j]) < DuplicateThreshold {
				continue
			}
			first, second := a, b
			if b.ID < a.ID {
				first, second = b, a
			}
			got, err := r.raise(ctx, candidate{
				key:  fmt.Sprintf("%s:%d:%d", KindNextDuplicate, second.ID, first.ID),
				kind: KindNextDuplicate, subjectKind: "next", subjectID: second.ID, repo: second.Repo,
				reason: fmt.Sprintf("next-up item %d (%s) reads like item %d (%s): drop the later one?", second.ID, second.Title, first.ID, first.Title),
				fix:    FixNextDrop,
			})
			n += got
			if err != nil {
				return n, err
			}
			break // one proposal per item
		}
	}
	return n, nil
}

var stopWords = map[string]bool{"the": true, "a": true, "an": true, "of": true, "to": true, "for": true, "and": true, "in": true, "on": true, "with": true, "is": true}

// titleWords is the set of a title's words, lower case, without filler.
func titleWords(title string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stopWords[w] {
			out[w] = true
		}
	}
	return out
}

// similarity is the shared share of two word sets (Jaccard). Sets of fewer than two
// words never match: a one-word title is too thin to call a duplicate.
func similarity(a, b map[string]bool) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
