package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// maxTopicTitle bounds a title so a confused model cannot flood the rail; maxTopicSummary
// is a line of context.
const (
	maxTopicTitle   = 80
	maxTopicSummary = 300
)

var errTopicNotPersons = errors.New("the front desk may rename, merge, move, clear or reopen a topic only in a turn the person started; say it to them first")

// topicLabel shortens a decision's or request's text to a line.
func topicLabel(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

// topicLinkView borrows a linked thing's own state: a link whose target is gone keeps its
// reference with no label, never an error.
func (s *Server) topicLinkView(ctx context.Context, l store.TopicLink) (api.TopicLinkView, bool) {
	v := api.TopicLinkView{Kind: l.Kind, Ref: l.Ref, By: l.By, Time: l.Created}
	// closedWork is true for a thing that no longer holds a topic open.
	closedWork := false
	id, _ := strconv.ParseInt(l.Ref, 10, 64)
	switch l.Kind {
	case store.TopicLane:
		if lane, err := s.Store.Lane(ctx, id); err == nil {
			v.Label, v.State = lane.Name, lane.State
			closedWork = lane.State == store.LaneClosed
		}
	case store.TopicNextItem:
		if it, err := s.Store.NextItem(ctx, id); err == nil {
			v.Label, v.State, v.Time = it.Title, it.State, later(v.Time, it.Updated)
			sv := s.nextView(ctx, it)
			v.State = sv.State
			v.Waits = sv.Proposed
			closedWork = sv.State == store.NextDone || sv.State == store.NextDropped
		}
	case store.TopicDecision:
		if d, err := s.Store.Decision(ctx, id); err == nil {
			v.Label, v.State = topicLabel(d.Question), d.State
			v.Waits = d.State == store.DecisionOpen
			v.Time = later(v.Time, d.Created)
			if d.Answered != nil {
				v.Time = later(v.Time, *d.Answered)
			}
			closedWork = d.State != store.DecisionOpen
		}
	case store.TopicRequest:
		if q, err := s.Store.Request(ctx, id); err == nil {
			v.Label, v.State = topicLabel(q.Message), q.State
			v.Waits = q.State == store.RequestNeedsRouting
			v.Time = later(v.Time, q.Updated)
		}
	case store.TopicRun:
		if r, err := s.Store.Run(ctx, id); err == nil {
			v.Label, v.State = fmt.Sprintf("run %d (%s)", r.ID, r.Agent), r.State
		}
	}
	return v, closedWork
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// topicView derives a topic's state from its links.
func (s *Server) topicView(ctx context.Context, t store.Topic) api.TopicView {
	v := api.TopicView{ID: t.ID, Title: t.Title, Status: t.Status, Summary: t.Summary, Source: t.Source,
		Created: t.Created, Updated: t.Updated, ClearedAt: t.ClearedAt, Activity: t.Updated, Links: []api.TopicLinkView{}}
	links, err := s.Store.TopicLinks(ctx, t.ID)
	if err != nil {
		s.Log.Warn("read a topic's links", "topic", t.ID, "err", err)
		return v
	}
	open, work := 0, 0
	approvedIdle := false
	for _, l := range links {
		lv, closed := s.topicLinkView(ctx, l)
		v.Links = append(v.Links, lv)
		v.Activity = later(v.Activity, lv.Time)
		if lv.Waits {
			v.Waits++
		}
		switch l.Kind {
		case store.TopicLane, store.TopicNextItem:
			work++
			if !closed {
				open++
			}
		}
		if l.Kind == store.TopicNextItem && lv.State == store.NextApproved {
			approvedIdle = true
		}
	}
	// A lane with no link of its own belongs to the topic of the next-up item that opened
	// it, or of a run tied to it: derived here, never stored, like the decisions below.
	haveLane := map[int64]bool{}
	var candidates []int64
	for _, l := range links {
		id, _ := strconv.ParseInt(l.Ref, 10, 64)
		switch l.Kind {
		case store.TopicLane:
			haveLane[id] = true
		case store.TopicNextItem:
			if it, err := s.Store.NextItem(ctx, id); err == nil && it.LaneID != 0 {
				candidates = append(candidates, it.LaneID)
			}
		case store.TopicRun:
			if r, err := s.Store.Run(ctx, id); err == nil && r.LaneID != 0 {
				candidates = append(candidates, r.LaneID)
			}
		}
	}
	for _, id := range candidates {
		if haveLane[id] || store.LaneTopic(ctx, s.Store, id) != t.ID {
			continue
		}
		haveLane[id] = true
		lv, closed := s.topicLinkView(ctx, store.TopicLink{TopicID: t.ID, Kind: store.TopicLane, Ref: strconv.FormatInt(id, 10), By: store.TopicLinkShepherd})
		links = append(links, store.TopicLink{TopicID: t.ID, Kind: store.TopicLane, Ref: lv.Ref, By: store.TopicLinkShepherd})
		v.Links = append(v.Links, lv)
		work++
		if !closed {
			open++
		}
	}
	// A decision an agent holds in a run of a linked lane belongs to the topic by that
	// link, as Shepherd tags by existing links: derived here, never stored, so it cannot
	// disagree with the lane.
	if ds, err := s.Store.Decisions(ctx, store.DecisionOpen); err == nil && len(ds) > 0 {
		have := map[string]bool{}
		laneOf := map[int64]bool{}
		for _, l := range links {
			have[l.Kind+":"+l.Ref] = true
			if l.Kind == store.TopicLane {
				id, _ := strconv.ParseInt(l.Ref, 10, 64)
				laneOf[id] = true
			}
		}
		for _, d := range ds {
			ref := strconv.FormatInt(d.ID, 10)
			if have[store.TopicDecision+":"+ref] {
				continue
			}
			if run, err := s.Store.Run(ctx, d.RunID); err != nil || !laneOf[run.LaneID] {
				continue
			}
			if other, err := s.Store.TopicOf(ctx, store.TopicDecision, ref); err == nil && other.TopicID != t.ID {
				continue
			}
			lv, _ := s.topicLinkView(ctx, store.TopicLink{TopicID: t.ID, Kind: store.TopicDecision, Ref: ref, By: store.TopicLinkShepherd, Created: d.Created})
			v.Links = append(v.Links, lv)
			v.Activity = later(v.Activity, lv.Time)
			v.Waits++
		}
	}
	switch t.Status {
	case store.TopicOpen:
		v.Settled = work > 0 && open == 0 && v.Waits == 0
	case store.TopicCleared:
		v.Reopen = v.Waits > 0 || approvedIdle
	}
	// Newest links first within a kind is not worth a second order: keep link order, and
	// let the clients group.
	return v
}

func (s *Server) listTopics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ts, err := s.Store.Topics(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	all := r.URL.Query().Get("all") != ""
	out := api.TopicList{Topics: []api.TopicView{}}
	for _, t := range ts {
		if t.Status == store.TopicCleared && !all {
			continue
		}
		out.Topics = append(out.Topics, s.topicView(ctx, t))
	}
	// Open topics first, those that need the person at the top, then by activity.
	sort.SliceStable(out.Topics, func(i, j int) bool {
		a, b := out.Topics[i], out.Topics[j]
		if (a.Status == store.TopicOpen) != (b.Status == store.TopicOpen) {
			return a.Status == store.TopicOpen
		}
		if (a.Waits > 0) != (b.Waits > 0) {
			return a.Waits > 0
		}
		return a.Activity.After(b.Activity)
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) topicID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("bad topic id"))
		return 0, false
	}
	return id, true
}

func (s *Server) getTopic(w http.ResponseWriter, r *http.Request) {
	id, ok := s.topicID(w, r)
	if !ok {
		return
	}
	t, err := s.Store.Topic(r.Context(), id)
	if err != nil {
		s.topicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.topicView(r.Context(), t))
}

// cleanTopicText checks and redacts a title and summary.
func cleanTopicText(title, summary *string) error {
	if title != nil {
		*title = strings.TrimSpace(redact.String(*title))
		switch {
		case *title == "":
			return errors.New("a topic needs a title")
		case len([]rune(*title)) > maxTopicTitle || strings.ContainsAny(*title, "\r\n"):
			return fmt.Errorf("a title is one line of at most %d characters", maxTopicTitle)
		}
	}
	if summary != nil {
		*summary = strings.TrimSpace(redact.String(*summary))
		if len([]rune(*summary)) > maxTopicSummary {
			return fmt.Errorf("a summary is at most %d characters", maxTopicSummary)
		}
	}
	return nil
}

// linkBy is who a link is recorded as: the person's, or the desk's.
func linkBy(r *http.Request) string {
	if personsNext(r) {
		return store.TopicLinkPerson
	}
	return store.TopicLinkDesk
}

// checkTopicRef validates a reference. The thing's existence is not checked for turns and
// feed rows, which the daemon has no cheap lookup for; the others must exist.
func (s *Server) checkTopicRef(ctx context.Context, ref *api.TopicRef) error {
	if !store.ValidTopicKind(ref.Kind) {
		return fmt.Errorf("kind is one of %s, not %q", strings.Join(store.TopicKinds, ", "), ref.Kind)
	}
	n, err := strconv.ParseInt(ref.Ref, 10, 64)
	if err != nil || n <= 0 {
		return fmt.Errorf("ref %q is not an id", ref.Ref)
	}
	ref.Ref = strconv.FormatInt(n, 10)
	var e error
	switch ref.Kind {
	case store.TopicLane:
		_, e = s.Store.Lane(ctx, n)
	case store.TopicNextItem:
		_, e = s.Store.NextItem(ctx, n)
	case store.TopicDecision:
		_, e = s.Store.Decision(ctx, n)
	case store.TopicRequest:
		_, e = s.Store.Request(ctx, n)
	case store.TopicRun:
		_, e = s.Store.Run(ctx, n)
	}
	if e != nil {
		return fmt.Errorf("%s %d: %w", ref.Kind, n, e)
	}
	return nil
}

// linkAllowed validates a reference and says whether the caller may tie it to a topic.
// The desk may tag a thing that has no topic or is already in this one (topic 0 is a new
// topic: nothing is in it); moving it elsewhere is the person's.
func (s *Server) linkAllowed(ctx context.Context, r *http.Request, topic int64, ref *api.TopicRef) (int, error) {
	if err := s.checkTopicRef(ctx, ref); err != nil {
		return http.StatusBadRequest, err
	}
	// The desk files the turn it is in; an earlier turn's filing is the person's.
	if p := principalOf(ctx); ref.Kind == store.TopicTurn && p.Role == RoleDesk && p.Turn > 0 && ref.Ref != strconv.FormatInt(p.Turn, 10) {
		return http.StatusForbidden, fmt.Errorf("the front desk files only its own turn, #%d", p.Turn)
	}
	if linkBy(r) != store.TopicLinkPerson {
		if cur, err := s.Store.TopicOf(ctx, ref.Kind, ref.Ref); err == nil {
			if t, err := s.Store.Topic(ctx, cur.TopicID); err == nil && t.ID != topic {
				return http.StatusForbidden, errTopicNotPersons
			}
		}
	}
	return 0, nil
}

// fileTurn files the turn the desk's token was minted for under the topic its tool just
// named, so "topic_set id" alone files the turn in progress. A turn already in another
// topic stays there: moving it is the person's.
func (s *Server) fileTurn(ctx context.Context, r *http.Request, topic int64) {
	p := principalOf(ctx)
	if p.Role != RoleDesk || p.Turn <= 0 {
		return
	}
	ref := strconv.FormatInt(p.Turn, 10)
	if cur, err := s.Store.TopicOf(ctx, store.TopicTurn, ref); err == nil && cur.TopicID != topic {
		return
	}
	if _, err := s.Store.LinkTopic(ctx, store.TopicLink{TopicID: topic, Kind: store.TopicTurn, Ref: ref, By: linkBy(r)}); err != nil {
		s.Log.Warn("file the desk's turn", "topic", topic, "turn", p.Turn, "err", err)
	}
}

// linkTopic ties a validated thing to a topic by the caller.
func (s *Server) linkTopic(ctx context.Context, r *http.Request, topic int64, ref api.TopicRef) (int, error) {
	if _, err := s.Store.LinkTopic(ctx, store.TopicLink{TopicID: topic, Kind: ref.Kind, Ref: ref.Ref, By: linkBy(r)}); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

func (s *Server) addTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req api.TopicAdd
	if !decode(w, r, &req) {
		return
	}
	if err := cleanTopicText(&req.Title, &req.Summary); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// A title that repeats an open topic's is refused with the existing id, so near
	// duplicates do not pile up.
	if ts, err := s.Store.Topics(ctx); err == nil {
		for _, t := range ts {
			if t.Status == store.TopicOpen && strings.EqualFold(t.Title, req.Title) {
				writeError(w, http.StatusConflict, fmt.Errorf("%w: topic %d is already called %q; use it", store.ErrConflict, t.ID, t.Title))
				return
			}
		}
	}
	if req.Link != nil {
		if code, err := s.linkAllowed(ctx, r, 0, req.Link); err != nil {
			writeError(w, code, err)
			return
		}
	}
	src := store.TopicByPerson
	if principalOf(ctx).Role == RoleDesk {
		src = store.TopicByDesk
	}
	t, err := s.Store.AddTopic(ctx, store.Topic{Title: req.Title, Summary: req.Summary, Source: src})
	if err != nil {
		s.fail(w, err)
		return
	}
	if req.Link != nil {
		if code, err := s.linkTopic(ctx, r, t.ID, *req.Link); err != nil {
			s.Log.Warn("link a new topic", "topic", t.ID, "err", err)
			writeError(w, code, err)
			return
		}
	}
	if req.Link == nil || req.Link.Kind != store.TopicTurn {
		s.fileTurn(ctx, r, t.ID)
	}
	s.Log.Info("topic added", "topic", t.ID, "source", src)
	writeJSON(w, http.StatusOK, s.topicView(ctx, t))
}

func (s *Server) patchTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.topicID(w, r)
	if !ok {
		return
	}
	var req api.TopicUpdate
	if !decode(w, r, &req) {
		return
	}
	if err := cleanTopicText(req.Title, req.Summary); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Status != nil && *req.Status != store.TopicOpen && *req.Status != store.TopicCleared {
		writeError(w, http.StatusBadRequest, fmt.Errorf("status is open or cleared, not %q", *req.Status))
		return
	}
	if p := principalOf(ctx); req.Link == nil && req.Title == nil && req.Summary == nil && req.Status == nil && req.MergeInto == 0 && (p.Role != RoleDesk || p.Turn <= 0) {
		writeError(w, http.StatusBadRequest, errors.New("say what to tie: --turn, --feed, --item, --lane, --run, --decision or --request, or --summary"))
		return
	}
	if (req.Title != nil || req.Status != nil || req.MergeInto != 0) && !personsNext(r) {
		writeError(w, http.StatusForbidden, errTopicNotPersons)
		return
	}
	t, err := s.Store.Topic(ctx, id)
	if err != nil {
		s.topicError(w, err)
		return
	}
	if req.Link != nil {
		if code, err := s.linkAllowed(ctx, r, t.ID, req.Link); err != nil {
			writeError(w, code, err)
			return
		}
		if code, err := s.linkTopic(ctx, r, t.ID, *req.Link); err != nil {
			writeError(w, code, err)
			return
		}
	}
	if req.Title != nil || req.Summary != nil || req.Status != nil {
		if req.Status != nil && *req.Status == store.TopicCleared && t.Status == store.TopicCleared {
			req.Status = nil
		}
		if t, err = s.Store.UpdateTopic(ctx, t.ID, store.TopicPatch{Title: req.Title, Summary: req.Summary, Status: req.Status}); err != nil {
			s.topicError(w, err)
			return
		}
	}
	if req.MergeInto != 0 {
		if t, err = s.Store.MergeTopics(ctx, t.ID, req.MergeInto); err != nil {
			s.topicError(w, err)
			return
		}
	}
	if req.Title == nil && req.Status == nil && req.MergeInto == 0 && (req.Link == nil || req.Link.Kind != store.TopicTurn) {
		s.fileTurn(ctx, r, t.ID)
	}
	s.Log.Info("topic changed", "topic", t.ID, "status", t.Status)
	writeJSON(w, http.StatusOK, s.topicView(ctx, t))
}

func (s *Server) topicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err)
	default:
		s.fail(w, err)
	}
}
