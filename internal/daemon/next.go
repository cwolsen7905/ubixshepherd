package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// maxNextTitle and maxNextNote bound an item: a line to name the work, a paragraph to
// say more.
const (
	maxNextTitle = 200
	maxNextNote  = 2000
)

// personsNext reports whether the caller may choose an item's state or order. Like an
// answer to a decision it is the person's: an agent's token never reaches these routes,
// and the front desk does it only in a turn the person started.
func personsNext(r *http.Request) bool {
	p := principalOf(r.Context())
	return p.Role != RoleDesk || p.Human
}

var errNotPersons = errors.New("the front desk may approve, defer, drop, finish or reorder an item only in a turn the person started; propose it and bring it to them")

// nextView joins an item with its lane and what the forge last said, and derives its
// state. It reads only; settleNext persists a merge it sees.
func (s *Server) nextView(ctx context.Context, it store.NextItem) api.NextItemView {
	v := api.NextItemView{
		ID: it.ID, Title: it.Title, Repo: it.Repo, Plan: it.Plan, Lane: it.Lane,
		State: it.State, Stored: it.State, Rank: it.Rank, Source: it.Source, Note: it.Note,
		MR: it.MR, Created: it.Created, Updated: it.Updated, DoneAt: it.DoneAt,
		Proposed: it.Source != store.NextByPerson && it.Source != store.NextByChat && it.State == store.NextPlanned,
	}
	if it.LaneID == 0 {
		return v
	}
	lane, err := s.Store.Lane(ctx, it.LaneID)
	if err != nil {
		return v
	}
	lf, _ := s.Store.LaneForge(ctx, lane.ID)
	v.LaneID, v.LaneName, v.LaneState = lane.ID, lane.Name, lane.State
	if repo, err := s.Store.Repo(ctx, lane.RepoID); err == nil {
		v.LaneRepo = repo.Name
	}
	if lf.MR != 0 {
		v.MR, v.MRURL, v.MRState = "!"+strconv.Itoa(lf.MR), lf.MRURL, lf.MRState
	}
	if lane.State == store.LaneOpen {
		if runs, err := s.Store.Runs(ctx, lane.ID, store.RunRunning, 1); err == nil && len(runs) > 0 {
			v.Running = true
		}
	}
	st := store.Settle(it, &lane, lf)
	v.State, v.Why = st.State, st.Why
	return v
}

// settleNext is the merge-closure hook: an item whose linked lane closed on a merged
// request becomes done, with the time the lane closed. The watcher closes the lane and
// records the merge without knowing of items, so the daemon settles them when the list is
// read or an item changes; nothing keeps a second status.
func (s *Server) settleNext(ctx context.Context, items []store.NextItem) []store.NextItem {
	for i, it := range items {
		if it.LaneID == 0 || it.State == store.NextDone || it.State == store.NextDropped {
			continue
		}
		lane, err := s.Store.Lane(ctx, it.LaneID)
		if err != nil {
			continue
		}
		lf, _ := s.Store.LaneForge(ctx, lane.ID)
		if st := store.Settle(it, &lane, lf); !st.Merged {
			continue
		}
		done := store.NextDone
		patch := store.NextPatch{State: &done}
		if lane.Closed != nil {
			patch.DoneAt = lane.Closed
		}
		if lf.MR != 0 && it.MR == "" {
			mr := "!" + strconv.Itoa(lf.MR)
			patch.MR = &mr
		}
		upd, err := s.Store.UpdateNextItem(ctx, it.ID, patch)
		if err != nil {
			s.Log.Warn("settle a next item", "item", it.ID, "err", err)
			continue
		}
		s.Log.Info("next item done: its lane's merge request merged", "item", it.ID, "lane", lane.Name)
		s.postFeed(ctx, store.FeedPlan, it.ID, fmt.Sprintf("next-up item %d done: %s (lane %s merged)", it.ID, upd.Title, lane.Name))
		items[i] = upd
	}
	return items
}

func (s *Server) listNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items, err := s.Store.NextItems(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	items = s.settleNext(ctx, items)
	all := r.URL.Query().Get("all") != ""
	out := api.NextList{Items: []api.NextItemView{}}
	if r.URL.Query().Get("matrix") != "0" {
		out.Matrix = s.matrixRows(ctx, items)
	}
	// Effective state decides the order shown: approved, then planned, deferred, with
	// started items among the rest by their stored place.
	for _, it := range items {
		v := s.nextView(ctx, it)
		if !all && (v.State == store.NextDone || v.State == store.NextDropped) {
			continue
		}
		if out.NextUp == 0 && v.State == store.NextApproved && v.LaneID == 0 {
			out.NextUp = v.ID
		}
		out.Items = append(out.Items, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad item id"))
		return
	}
	it, err := s.Store.NextItem(ctx, id)
	if err != nil {
		s.nextError(w, err)
		return
	}
	it = s.settleNext(ctx, []store.NextItem{it})[0]
	writeJSON(w, http.StatusOK, s.nextView(ctx, it))
}

// cleanNext checks and redacts an item's text. Plan is a path under the repo, never
// absolute or leaving it.
func cleanNext(title, note, plan *string) error {
	if title != nil {
		*title = strings.TrimSpace(redact.String(*title))
		switch {
		case *title == "":
			return errors.New("an item needs a title")
		case len(*title) > maxNextTitle || strings.ContainsAny(*title, "\r\n"):
			return fmt.Errorf("a title is one line of at most %d bytes", maxNextTitle)
		}
	}
	if note != nil {
		*note = redact.String(*note)
		if len(*note) > maxNextNote {
			return fmt.Errorf("a note is at most %d bytes", maxNextNote)
		}
	}
	if plan != nil && *plan != "" {
		p := path.Clean(strings.ReplaceAll(*plan, `\`, "/"))
		if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
			return errors.New("plan is a path under the repo, such as docs/lane-diff.md")
		}
		*plan = p
	}
	return nil
}

func validChosen(state string) bool {
	switch state {
	case store.NextPlanned, store.NextApproved, store.NextDeferred, store.NextDropped, store.NextDone:
		return true
	}
	return false
}

func (s *Server) addNext(w http.ResponseWriter, r *http.Request) {
	var req api.NextAdd
	if !decode(w, r, &req) {
		return
	}
	if err := cleanNext(&req.Title, &req.Note, &req.Plan); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.State == "" {
		req.State = store.NextPlanned
	}
	if !validChosen(req.State) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("state is planned, approved, deferred, dropped or done, not %q", req.State))
		return
	}
	if req.State != store.NextPlanned && !personsNext(r) {
		writeError(w, http.StatusForbidden, errNotPersons)
		return
	}
	src := store.NextByPerson
	if principalOf(r.Context()).Role == RoleDesk {
		src = store.NextByChat
		if req.Propose {
			src = store.NextByDesk
		}
	} else if req.Propose {
		writeError(w, http.StatusForbidden, errors.New("propose is for the front desk"))
		return
	}
	it, err := s.Store.AddNextItem(r.Context(), store.NextItem{
		Title: req.Title, Repo: req.Repo, Plan: req.Plan, Lane: req.Lane, Note: req.Note,
		State: req.State, Source: src,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("next item added", "item", it.ID, "state", it.State, "source", it.Source)
	s.postFeed(r.Context(), store.FeedPlan, it.ID, fmt.Sprintf("next-up item %d added by %s as %s: %s", it.ID, it.Source, it.State, it.Title))
	writeJSON(w, http.StatusOK, s.nextView(r.Context(), it))
}

func (s *Server) patchNext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad item id"))
		return
	}
	var req api.NextUpdate
	if !decode(w, r, &req) {
		return
	}
	if err := cleanNext(req.Title, req.Note, req.Plan); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.State != nil && !validChosen(*req.State) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("state is planned, approved, deferred, dropped or done, not %q (started is derived)", *req.State))
		return
	}
	if (req.State != nil || req.Move != "") && !personsNext(r) {
		writeError(w, http.StatusForbidden, errNotPersons)
		return
	}
	if req.LaneID != nil && *req.LaneID != 0 {
		if _, err := s.Store.Lane(ctx, *req.LaneID); err != nil {
			s.nextError(w, fmt.Errorf("lane %d: %w", *req.LaneID, err))
			return
		}
	}
	before, err := s.Store.NextItem(ctx, id)
	if err != nil {
		s.nextError(w, err)
		return
	}
	patch := store.NextPatch{Title: req.Title, Repo: req.Repo, Plan: req.Plan, Lane: req.Lane, MR: req.MR,
		Note: req.Note, State: req.State, LaneID: req.LaneID}
	it, err := s.Store.UpdateNextItem(ctx, id, patch)
	if err != nil {
		s.nextError(w, err)
		return
	}
	if req.Move != "" {
		if it, err = s.Store.MoveNextItem(ctx, id, req.Move); err != nil {
			s.nextError(w, err)
			return
		}
	}
	s.Log.Info("next item changed", "item", id, "state", it.State)
	if it.State != before.State {
		if verb, ok := planVerbs[it.State]; ok {
			s.postFeed(ctx, store.FeedPlan, id, fmt.Sprintf("next-up item %d %s: %s", id, verb, it.Title))
		}
	}
	writeJSON(w, http.StatusOK, s.nextView(ctx, it))
}

// planVerbs are the stored states whose arrival is told on the feed; a return to
// planned, a move and a note edit are not.
var planVerbs = map[string]string{
	store.NextApproved: "approved",
	store.NextDeferred: "deferred",
	store.NextDropped:  "dropped",
	store.NextDone:     "marked done",
}

func (s *Server) nextError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err)
	case strings.HasPrefix(err.Error(), "move to"):
		writeError(w, http.StatusBadRequest, err)
	default:
		s.fail(w, err)
	}
}

// linkLaneItem links the item a lane was opened for. item is the one named at open (0
// for none); without it, an open item of the lane's repo whose name hint equals the
// lane's name is linked when exactly one matches. It returns a line for the opener, or
// "" when nothing was linked.
func (s *Server) linkLaneItem(ctx context.Context, lane store.Lane, repoName string, item int64) (string, error) {
	if item == 0 {
		items, err := s.Store.NextItems(ctx)
		if err != nil {
			return "", err
		}
		var match []store.NextItem
		for _, it := range items {
			if it.Lane == lane.Name && it.Repo == repoName && it.LaneID == 0 && openNextState(it.State) {
				match = append(match, it)
			}
		}
		if len(match) != 1 {
			return "", nil
		}
		item = match[0].ID
	}
	repo := repoName
	patch := store.NextPatch{LaneID: &lane.ID}
	it, err := s.Store.NextItem(ctx, item)
	if err != nil {
		return "", err
	}
	if it.Repo == "" {
		patch.Repo = &repo
	}
	if it.Lane == "" {
		patch.Lane = &lane.Name
	}
	if _, err := s.Store.UpdateNextItem(ctx, item, patch); err != nil {
		return "", err
	}
	return fmt.Sprintf("linked to next-up item %d: %s", item, it.Title), nil
}

// openNextState is a state an item can be in and still be started: not finished, not
// dropped.
func openNextState(state string) bool {
	return state == store.NextPlanned || state == store.NextApproved || state == store.NextDeferred
}

// checkLaneItem refuses opening a lane for an item that cannot take one, before the lane
// exists: unknown, done or dropped, or linked to a lane still open.
func (s *Server) checkLaneItem(ctx context.Context, item int64) error {
	it, err := s.Store.NextItem(ctx, item)
	if err != nil {
		return fmt.Errorf("item %d: %w", item, err)
	}
	if it.State == store.NextDone || it.State == store.NextDropped {
		return fmt.Errorf("%w: item %d is %s", store.ErrConflict, item, it.State)
	}
	if it.LaneID != 0 {
		if l, err := s.Store.Lane(ctx, it.LaneID); err == nil && l.State != store.LaneClosed {
			return fmt.Errorf("%w: item %d already has lane %s open", store.ErrConflict, item, l.Name)
		}
	}
	return nil
}

// waiting answers GET /v1/waiting: what needs the person's action, in one list. The
// optional workspace_id narrows the lanes to one workspace.
func (s *Server) waiting(w http.ResponseWriter, r *http.Request) {
	var ws int64
	if v := r.URL.Query().Get("workspace_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("bad workspace_id"))
			return
		}
		ws = id
	}
	out, err := waiting.Collect(r.Context(), s.Store, s.LiveConfig(), ws)
	if err != nil {
		s.fail(w, err)
		return
	}
	qs, err := s.Store.Questions(r.Context(), ws, store.QuestionOpen)
	if err != nil {
		s.fail(w, err)
		return
	}
	out.AddQuestions(qs)
	if held, err := s.mergedArtifactItems(r.Context(), ws); err != nil {
		s.Log.Warn("waiting: merged lanes with artifacts", "err", err)
	} else if len(held) > 0 {
		out.AddItems(held)
	}
	writeJSON(w, http.StatusOK, out)
}
