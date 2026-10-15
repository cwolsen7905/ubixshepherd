// Package waiting collects what needs the person's action into one list: open decisions,
// requests between lanes that need routing, lane merge requests held on their approval, lanes with work to ship, and Next-up items
// proposed to them. It reads the store and the lanes' git state; nothing is stored for
// it, so the list is as fresh as what it reads.
package waiting

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// heldJob lists the require-approval job statuses that hold a merge: not started, waiting
// to be played, blocked, or failed for want of a sign-off.
var heldJob = map[string]bool{"pending": true, "manual": true, "blocked": true, "failed": true, "created": true, "scheduled": true}

// Collect reads the list. workspaceID 0 means every workspace; Next-up items belong to no
// workspace and are always included. It stops at the first store error.
func Collect(ctx context.Context, st store.Store, conf config.Config, workspaceID int64) (api.Waiting, error) {
	out := api.Waiting{Items: []api.WaitingItem{}}
	repos, err := allRepos(ctx, st, workspaceID)
	if err != nil {
		return out, err
	}
	laneOf := map[int64]store.Lane{}
	repoOf := map[int64]store.Repo{}
	for _, rp := range repos {
		lanes, err := st.Lanes(ctx, rp.ID)
		if err != nil {
			return out, err
		}
		for _, l := range lanes {
			laneOf[l.ID] = l
			repoOf[l.ID] = rp
		}
	}

	// Decisions: ours through the run's lane. One whose run or lane cannot be found is
	// kept when no workspace was asked for.
	ds, err := st.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		return out, err
	}
	openDecision := map[int64]bool{}
	for _, d := range ds {
		it := api.WaitingItem{
			Kind: api.WaitingDecision, Action: "answer", DecisionID: d.ID,
			Title: oneLine(d.Question), Since: d.Created,
		}
		run, err := st.Run(ctx, d.RunID)
		if err == nil {
			if l, ok := laneOf[run.LaneID]; ok {
				openDecision[l.ID] = true
				it.Lane, it.LaneID, it.Repo = l.Name, l.ID, repoOf[l.ID].Name
			} else if workspaceID != 0 {
				continue
			}
		} else if workspaceID != 0 {
			continue
		}
		it.Detail = fmt.Sprintf("decision %d: %s", d.ID, it.Title)
		out.Items = append(out.Items, it)
		out.Counts.Decisions++
	}

	// Answered decisions, by the lane their run belongs to, for scope widenings.
	answered, err := st.Decisions(ctx, store.DecisionAnswered)
	if err != nil {
		return out, err
	}
	runLane := map[int64]int64{}
	for _, d := range answered {
		if run, err := st.Run(ctx, d.RunID); err == nil {
			runLane[d.RunID] = run.LaneID
		}
	}

	laneIDs := make([]int64, 0, len(laneOf))
	for id := range laneOf {
		laneIDs = append(laneIDs, id)
	}
	sort.Slice(laneIDs, func(i, j int) bool { return laneIDs[i] < laneIDs[j] })
	for _, id := range laneIDs {
		l, rp := laneOf[id], repoOf[id]
		if l.State != store.LaneOpen {
			continue
		}
		running := false
		if busy, err := st.Runs(ctx, l.ID, store.RunRunning, 1); err == nil && len(busy) > 0 {
			running = true
		}
		lf, _ := st.LaneForge(ctx, l.ID)
		if it, ok := mrItem(l, rp, lf, running); ok {
			out.Items = append(out.Items, it)
			out.Counts.MRs++
		}
		if running {
			continue
		}
		if it, ok := blockedItem(ctx, st, l, rp, openDecision[l.ID]); ok {
			out.Items = append(out.Items, it)
			out.Counts.Blocked++
		}
		for _, d := range answered {
			if runLane[d.RunID] != l.ID {
				continue
			}
			if it, ok := scopeItem(l, rp, d); ok {
				out.Items = append(out.Items, it)
				out.Counts.Scope++
			}
		}
		if it, ok := shipItem(ctx, conf, l, rp); ok {
			out.Items = append(out.Items, it)
			out.Counts.Ship++
		}
	}

	items, err := st.NextItems(ctx)
	if err != nil {
		return out, err
	}
	for _, it := range items {
		if it.State == store.NextApproved && it.LaneID != 0 {
			if wi, ok := closedNextItem(ctx, st, it); ok {
				out.Items = append(out.Items, wi)
				out.Counts.Next++
			}
			continue
		}
		if it.Source == store.NextByPerson || it.Source == store.NextByChat || it.State != store.NextPlanned {
			continue
		}
		// A linked lane that is open makes the item started: nothing waits on it.
		if it.LaneID != 0 {
			if l, err := st.Lane(ctx, it.LaneID); err == nil && (l.State == store.LaneOpen || l.State == store.LaneOpening) {
				continue
			}
		}
		out.Items = append(out.Items, api.WaitingItem{
			Kind: api.WaitingNext, Action: "review", Repo: it.Repo, NextID: it.ID,
			Title: oneLine(it.Title), Detail: fmt.Sprintf("proposed by the %s: %s", it.Source, oneLine(it.Title)),
			Since: it.Created,
		})
		out.Counts.Next++
	}

	// Requests between lanes that Shepherd could not route by rule: the person, or the
	// front desk, has to choose. Ours through the asking run's lane, like decisions.
	reqs, err := st.Requests(ctx, store.RequestNeedsRouting)
	if err != nil {
		return out, err
	}
	for _, q := range reqs {
		msg := oneLine(q.Message)
		it := api.WaitingItem{
			Kind: api.WaitingRequest, Action: "route", RequestID: q.ID,
			Title: msg, Since: q.Created,
		}
		run, err := st.Run(ctx, q.FromRun)
		if err == nil {
			if l, ok := laneOf[run.LaneID]; ok {
				it.Lane, it.LaneID, it.Repo = l.Name, l.ID, repoOf[l.ID].Name
			} else if workspaceID != 0 {
				continue
			}
		} else if workspaceID != 0 {
			continue
		}
		it.Detail = fmt.Sprintf("request %d needs routing: %s", q.ID, msg)
		if q.Note != "" {
			it.Detail += " (" + oneLine(q.Note) + ")"
		}
		out.Items = append(out.Items, it)
		out.Counts.Requests++
	}

	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Since.Before(out.Items[j].Since) })
	out.Seal()
	return out, nil
}

// mrItem is a lane's open merge request that only the person can move: its pipeline is
// held (or failed) at the require-approval job with no approval, or it failed elsewhere
// and no run is working on it. The first is approved from the web page; the second is
// only shown.
func mrItem(l store.Lane, rp store.Repo, lf store.LaneForge, running bool) (api.WaitingItem, bool) {
	if lf.MR == 0 || merged(lf) || strings.EqualFold(lf.MRState, "closed") {
		return api.WaitingItem{}, false
	}
	it := api.WaitingItem{Kind: api.WaitingMR, Repo: rp.Name, Lane: l.Name, LaneID: l.ID, MR: lf.MR}
	switch {
	case !lf.Approved && heldJob[lf.RequireApproval] && (lf.PipelineStatus != "failed" || lf.RequireApproval == "failed"):
		it.Action = "approve"
		it.Title = fmt.Sprintf("approve MR !%d, then retry its approval job", lf.MR)
		it.Detail = "approve it, then retry the require-approval job"
		it.Since = firstTime(lf.PipelineFinished, lf.PipelineStarted, lf.MRUpdated, l.Created)
	case lf.PipelineStatus == "failed" && !running:
		it.Action = "open"
		it.Title = fmt.Sprintf("MR !%d: pipeline failed", lf.MR)
		it.Detail = "pipeline failed"
		it.Since = firstTime(lf.PipelineFinished, lf.PipelineStarted, lf.MRUpdated, l.Created)
	default:
		return api.WaitingItem{}, false
	}
	return it, true
}

// shipItem is an open lane with commits not on its branch at origin, in a repo whose
// push is not the agent's. With push: shepherd the person's word ships it; with
// push: human they push it themselves, and the item only says so.
func shipItem(ctx context.Context, conf config.Config, l store.Lane, rp store.Repo) (api.WaitingItem, bool) {
	push := conf.Profile(rp.Name).Autonomy.Push
	if push != config.Shepherd && push != config.Human {
		return api.WaitingItem{}, false
	}
	n := dispatch.Unpushed(ctx, l)
	if n == 0 {
		return api.WaitingItem{}, false
	}
	it := api.WaitingItem{
		Kind: api.WaitingShip, Repo: rp.Name, Lane: l.Name, LaneID: l.ID,
		Since: l.Created,
	}
	plural := "s"
	if n == 1 {
		plural = ""
	}
	if push == config.Shepherd {
		it.Action = "ship"
		it.Title = fmt.Sprintf("ship %d commit%s", n, plural)
		it.Detail = fmt.Sprintf("%d commit%s not pushed; ship it", n, plural)
	} else {
		it.Action = "open"
		it.Title = fmt.Sprintf("push %d commit%s", n, plural)
		it.Detail = fmt.Sprintf("%d commit%s not pushed; push it yourself", n, plural)
	}
	return it, true
}

func allRepos(ctx context.Context, st store.Store, workspaceID int64) ([]store.Repo, error) {
	var wss []store.Workspace
	if workspaceID != 0 {
		wss = []store.Workspace{{ID: workspaceID}}
	} else {
		var err error
		if wss, err = st.Workspaces(ctx); err != nil {
			return nil, err
		}
	}
	var out []store.Repo
	for _, w := range wss {
		rs, err := st.Repos(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:159]) + "…"
	}
	return s
}

// merged says the forge has merged the lane's request: its state, or the merge commit
// Shepherd recorded, either of which outlives a stale pipeline status.
func merged(lf store.LaneForge) bool {
	return strings.EqualFold(lf.MRState, "merged") || lf.MergeSHA != ""
}

// blockedItem is an open lane, no run going, whose latest run (not dismissed) ended with
// a report of status blocked as its last word. The agent's summary is shown. A lane
// with an open decision already shows as that decision.
func blockedItem(ctx context.Context, st store.Store, l store.Lane, rp store.Repo, hasDecision bool) (api.WaitingItem, bool) {
	if hasDecision {
		return api.WaitingItem{}, false
	}
	runs, err := st.Runs(ctx, l.ID, "", 1)
	if err != nil || len(runs) == 0 || runs[0].Dismissed != nil {
		return api.WaitingItem{}, false
	}
	evs, err := st.Events(ctx, runs[0].ID)
	if err != nil {
		return api.WaitingItem{}, false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Kind != dispatch.EventReport {
			continue
		}
		if e.Status != "blocked" {
			return api.WaitingItem{}, false
		}
		sum := oneLine(e.Text)
		return api.WaitingItem{
			Kind: api.WaitingBlocked, Action: "open", Repo: rp.Name, Lane: l.Name, LaneID: l.ID,
			Title:  fmt.Sprintf("run %d reported blocked: %s", runs[0].ID, sum),
			Detail: fmt.Sprintf("run %d blocked: %s", runs[0].ID, sum),
			Since:  firstTime(e.Created, l.Created),
		}, true
	}
	return api.WaitingItem{}, false
}

// scopeItem is an answered decision on an open lane whose question asked to widen the
// lane's scope: it mentions scope and names repo paths the lane's scope does not cover,
// and the answer is an affirmative (leads with yes, approve, ok, ...). Free text is read
// no further than that, so a hedged answer is not taken as approval; the signal clears
// the moment the scope covers every named path.
func scopeItem(l store.Lane, rp store.Repo, d store.Decision) (api.WaitingItem, bool) {
	if !strings.Contains(strings.ToLower(d.Question), "scope") || !affirmative(d.Answer) {
		return api.WaitingItem{}, false
	}
	var missing []string
	for _, p := range pathsIn(d.Question) {
		if !scope.Any(l.Scope, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return api.WaitingItem{}, false
	}
	list := strings.Join(missing, ", ")
	since := d.Created
	if d.Answered != nil {
		since = *d.Answered
	}
	return api.WaitingItem{
		Kind: api.WaitingScope, Action: "open", Repo: rp.Name, Lane: l.Name, LaneID: l.ID, DecisionID: d.ID,
		Title:  oneLine(fmt.Sprintf("approved scope widening not applied: %s", list)),
		Detail: oneLine(fmt.Sprintf("decision %d approved widening scope to %s; lane scope is still %s", d.ID, list, strings.Join(l.Scope, ", "))),
		Since:  since,
	}, true
}

var negations = []string{"no", "n", "don't", "dont", "deny", "denied", "reject", "rejected", "decline", "never", "keep", "stay", "not"}
var affirmatives = []string{"yes", "y", "yep", "yeah", "approve", "approved", "ok", "okay", "go", "widen", "allow", "allowed", "agreed", "agree", "sure", "granted", "accept", "accepted", "do-it", "proceed"}

func affirmative(answer string) bool {
	words := strings.Fields(strings.ToLower(strings.Trim(answer, " \t\n*_`\"'.!")))
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if w == "not" || w == "don't" || w == "dont" {
			return false
		}
	}
	first := strings.Trim(words[0], ",.:;!*`\"'")
	for _, n := range negations {
		if first == n {
			return false
		}
	}
	for _, a := range affirmatives {
		if first == a {
			return true
		}
	}
	return false
}

// pathsIn returns the repo-relative paths a question names: tokens with a slash or a
// file extension, trimmed of quoting and punctuation, without URLs.
func pathsIn(q string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Fields(q) {
		tok = strings.Trim(tok, "`'\"()[],;:!?*")
		tok = strings.TrimRight(tok, ".")
		if tok == "" || strings.Contains(tok, "://") || strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "~") {
			continue
		}
		tok = strings.TrimPrefix(tok, "./")
		if !strings.Contains(tok, "/") && !(strings.Contains(tok, ".") && len(tok) > 3 && !strings.HasSuffix(tok, ".")) {
			continue
		}
		if strings.ContainsAny(tok, "@=<>") || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// closedNextItem is an approved Next-up item whose linked lane closed without the
// forge's merge. Shepherd's own record of a close can be stale after a squash merge, so
// the forge's merge state and the recorded merge commit both clear it; so does a lane
// that is still open (the item is started).
func closedNextItem(ctx context.Context, st store.Store, it store.NextItem) (api.WaitingItem, bool) {
	l, err := st.Lane(ctx, it.LaneID)
	if err != nil || l.State != store.LaneClosed {
		return api.WaitingItem{}, false
	}
	lf, _ := st.LaneForge(ctx, l.ID)
	if merged(lf) {
		return api.WaitingItem{}, false
	}
	since := firstTime(ptrTime(l.Closed), it.Updated, it.Created)
	return api.WaitingItem{
		Kind: api.WaitingNext, Action: "review", Repo: it.Repo, Lane: l.Name, LaneID: l.ID, NextID: it.ID,
		Title:  oneLine(it.Title),
		Detail: fmt.Sprintf("approved, but lane %s closed without a merge: %s", l.Name, oneLine(it.Title)),
		Since:  since,
	}, true
}

func ptrTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
