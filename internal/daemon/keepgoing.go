package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// FeedKeepGoing is the feed kind for keep-going mode turning on or off, and for a lane
// handed back by the loop guard. The feed's closed set maps it to an info event until
// the thread and history learn it (docs/keep-going.md, section 7).
const FeedKeepGoing = "keep_going"

// The autonomous actions Allow gates. Each is already authorised elsewhere (an approved
// Next-up item, a repo's autonomy, the fix loop); keep-going only lets the desk take it
// without being asked.
const (
	KeepStartItem = "start_item" // open a lane for the first approved item and run it
	KeepContinue  = "continue"   // continue a run whose next step is approved
	KeepFix       = "fix"        // follow up a failed pipeline
	KeepRerun     = "rerun"      // re-run a run a daemon restart interrupted
	KeepScope     = "scope"      // widen a lane's scope an answered decision named
)

// keepGoingTick is how often the daemon looks at the limits with nothing else prompting
// it. Run ends and feed items look sooner.
const keepGoingTick = time.Minute

// Decision is Allow's answer: when OK is false, Reason says why, for the desk to relay.
type Decision struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// KeepGoing is the daemon's side of keep-going mode: the one place that decides whether
// an autonomous action may go ahead, and the one that turns the mode off at a limit. A
// prompt is not a limit, so none of this is left to the desk's model. State is in the
// store, so a restart keeps it on and re-checks it first.
type KeepGoing struct {
	s *Server
	// Now is the clock; tests move it.
	Now func() time.Time
	mu  sync.Mutex
	// told remembers the lanes the loop guard already announced today, so the person is
	// told once.
	told map[string]bool
}

// KeepGoing returns the server's keep-going controller.
func (s *Server) KeepGoing() *KeepGoing {
	s.keepOnce.Do(func() { s.keep = &KeepGoing{s: s, Now: time.Now, told: map[string]bool{}} })
	return s.keep
}

func (k *KeepGoing) day(t time.Time) string { return t.Format("2006-01-02") }

// spentOn is a day's total spend, credits at the configured price.
func (k *KeepGoing) spentOn(ctx context.Context, day string, cfg config.Config) (float64, error) {
	by, err := k.s.Store.SpendOn(ctx, day)
	if err != nil {
		return 0, err
	}
	var credit float64
	if cfg.Daemon.CreditUSD != nil {
		credit = *cfg.Daemon.CreditUSD
	}
	total := 0.0
	for _, sp := range by {
		total += sp.USD + sp.Credits*credit
	}
	return total, nil
}

// spentSince is how far the day's spend has grown since the mode was turned on.
func (k *KeepGoing) spentSince(ctx context.Context, row store.KeepGoing, now time.Time, cfg config.Config) (float64, error) {
	if !row.On || row.Since.IsZero() {
		return 0, nil
	}
	start := row.Since.In(now.Location())
	d := time.Date(start.Year(), start.Month(), start.Day(), 12, 0, 0, 0, now.Location())
	last := k.day(now)
	total := 0.0
	for i := 0; i < 366; i++ {
		day := k.day(d)
		if day > last {
			break
		}
		t, err := k.spentOn(ctx, day, cfg)
		if err != nil {
			return 0, err
		}
		total += t
		d = d.AddDate(0, 0, 1)
	}
	if over := total - row.SpentAtStart; over > 0 {
		return over, nil
	}
	return 0, nil
}

// running counts the runs running in the workspace's lanes.
func (k *KeepGoing) running(ctx context.Context, ws int64) (int, error) {
	repos, err := k.s.Store.Repos(ctx, ws)
	if err != nil {
		return 0, err
	}
	mine := map[int64]bool{}
	for _, r := range repos {
		mine[r.ID] = true
	}
	runs, err := k.s.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, run := range runs {
		lane, err := k.s.Store.Lane(ctx, run.LaneID)
		if err == nil && mine[lane.RepoID] {
			n++
		}
	}
	return n, nil
}

// limits are the session's effective limits: the row's overrides over the config's.
type keepLimits struct {
	cap  float64
	runs int
	idle time.Duration
}

func limitsOf(cfg config.KeepGoing, row store.KeepGoing) keepLimits {
	l := keepLimits{cap: cfg.CapUSD(), runs: cfg.Runs(), idle: cfg.Idle()}
	if row.Cap > 0 {
		l.cap = row.Cap
	}
	if row.MaxRuns > 0 {
		l.runs = row.MaxRuns
	}
	if row.IdleHours > 0 {
		l.idle = time.Duration(row.IdleHours * float64(time.Hour))
	}
	return l
}

// evaluate turns the mode off when a limit has passed, saving the row and telling the
// feed. The caller holds k.mu. It reports whether it turned it off.
func (k *KeepGoing) evaluate(ctx context.Context, row *store.KeepGoing) (bool, error) {
	if !row.On {
		return false, nil
	}
	now := k.Now()
	cfg := k.s.LiveConfig()
	lim := limitsOf(cfg.KeepGoing, *row)
	reason := ""
	switch {
	case !now.Before(cfg.KeepGoing.DayEndAt(row.Since.In(now.Location()))):
		reason = store.KeepOffDayEnd
	case !row.LastAction.IsZero() && now.Sub(row.LastAction) >= lim.idle:
		reason = store.KeepOffIdle
	}
	if reason == "" {
		if b := cfg.Daemon.Budget; b != nil && *b > 0 && cfg.Daemon.BudgetCapMode() == config.CapHard {
			today, err := k.spentOn(ctx, k.day(now), cfg)
			if err != nil {
				return false, err
			}
			if today >= *b {
				reason = store.KeepOffBudget
			}
		}
	}
	if reason == "" {
		spent, err := k.spentSince(ctx, *row, now, cfg)
		if err != nil {
			return false, err
		}
		if spent >= lim.cap {
			reason = store.KeepOffCap
		}
	}
	if reason == "" {
		return false, nil
	}
	return true, k.turnOff(ctx, row, reason, store.KeepByLimit)
}

func (k *KeepGoing) turnOff(ctx context.Context, row *store.KeepGoing, reason, by string) error {
	row.On, row.ReasonOff, row.OffAt, row.By = false, reason, k.Now().UTC(), by
	if err := k.s.Store.SetKeepGoing(ctx, *row); err != nil {
		return err
	}
	k.s.postFeed(ctx, FeedKeepGoing, 0, "keep-going is off: "+offText(reason))
	return nil
}

// offText says a reason in words, for the feed and the desk's next event.
func offText(reason string) string {
	switch reason {
	case store.KeepOffCap:
		return "it reached its spend cap"
	case store.KeepOffBudget:
		return "the daily budget was crossed"
	case store.KeepOffIdle:
		return "nothing was started or answered for too long"
	case store.KeepOffDayEnd:
		return "the day ended"
	case store.KeepOffPerson:
		return "the person turned it off"
	}
	return reason
}

// Enable turns the mode on for a workspace as by (cli, web or desk), or refreshes the
// session's overrides when it is already on. It records the day's spend so far, so the
// cap counts only what the mode itself spends.
func (k *KeepGoing) Enable(ctx context.Context, ws int64, by string, set api.KeepGoingSet) (store.KeepGoing, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	row, err := k.s.Store.KeepGoing(ctx, ws)
	if err != nil {
		return row, err
	}
	// A row left on past a limit is off before it is turned on again.
	if _, err := k.evaluate(ctx, &row); err != nil {
		return row, err
	}
	now := k.Now()
	cfg := k.s.LiveConfig()
	if !row.On {
		spent, err := k.spentOn(ctx, k.day(now), cfg)
		if err != nil {
			return row, err
		}
		row = store.KeepGoing{WorkspaceID: ws, On: true, Since: now.UTC(), By: by, SpentAtStart: spent}
	}
	row.Cap, row.MaxRuns, row.IdleHours = set.Cap, set.MaxRuns, set.IdleHours
	row.LastAction = now.UTC()
	if err := k.s.Store.SetKeepGoing(ctx, row); err != nil {
		return row, err
	}
	lim := limitsOf(cfg.KeepGoing, row)
	k.s.postFeed(ctx, FeedKeepGoing, 0, fmt.Sprintf("keep-going is on (%s): up to $%.2f, %d runs at once, idle %s, until %s",
		by, lim.cap, lim.runs, lim.idle, cfg.KeepGoing.DayEndAt(now).Format("15:04")))
	if _, err := k.evaluate(ctx, &row); err != nil {
		return row, err
	}
	return row, nil
}

// Disable turns the mode off. It is always allowed, and a no-op when already off.
func (k *KeepGoing) Disable(ctx context.Context, ws int64, by string) (store.KeepGoing, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	row, err := k.s.Store.KeepGoing(ctx, ws)
	if err != nil || !row.On {
		return row, err
	}
	return row, k.turnOff(ctx, &row, store.KeepOffPerson, by)
}

// Touch counts a message from the person as activity, so idle time counts from it.
func (k *KeepGoing) Touch(ctx context.Context, ws int64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	row, err := k.s.Store.KeepGoing(ctx, ws)
	if err != nil || !row.On {
		return
	}
	row.LastAction = k.Now().UTC()
	if err := k.s.Store.SetKeepGoing(ctx, row); err != nil {
		k.s.Log.Warn("keep-going: record activity", "err", err)
	}
}

// Evaluate checks one workspace's limits, turning the mode off if one is spent, and
// returns the row as it then stands.
func (k *KeepGoing) Evaluate(ctx context.Context, ws int64) (store.KeepGoing, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	row, err := k.s.Store.KeepGoing(ctx, ws)
	if err != nil {
		return row, err
	}
	_, err = k.evaluate(ctx, &row)
	return row, err
}

// EvaluateAll checks every workspace: on the timer, when a run ends, and at start, so
// a restart past a limit turns the mode off before the desk's first turn.
func (k *KeepGoing) EvaluateAll(ctx context.Context) {
	wss, err := k.s.Store.Workspaces(ctx)
	if err != nil {
		k.s.Log.Warn("keep-going: list workspaces", "err", err)
		return
	}
	for _, ws := range wss {
		if _, err := k.Evaluate(ctx, ws.ID); err != nil {
			k.s.Log.Warn("keep-going: evaluate", "workspace", ws.ID, "err", err)
		}
	}
}

// Allow says whether the front desk may take an autonomous action now: one in a turn the
// person did not start. It refuses when the mode is off, when the concurrent-run limit
// is reached, or when the lane has had its starts for the day, and it counts an allowed
// start. Lane 0 is for an action with no lane yet (an item's first start). The caller
// relays Reason as the tool's result; the repo's own autonomy and the item's approval
// are checked where the action runs.
func (k *KeepGoing) Allow(ctx context.Context, ws int64, action string, lane int64) Decision {
	switch action {
	case KeepStartItem, KeepContinue, KeepFix, KeepRerun, KeepScope:
	default:
		return Decision{Reason: fmt.Sprintf("%q is not an action keep-going covers", action)}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	refuse := func(format string, a ...any) Decision { return Decision{Reason: fmt.Sprintf(format, a...)} }
	row, err := k.s.Store.KeepGoing(ctx, ws)
	if err != nil {
		return refuse("keep-going state unreadable: %v", err)
	}
	if _, err := k.evaluate(ctx, &row); err != nil {
		return refuse("keep-going state unreadable: %v", err)
	}
	if !row.On {
		if row.ReasonOff != "" {
			return refuse("keep-going is off: %s", offText(row.ReasonOff))
		}
		return refuse("keep-going is off")
	}
	cfg := k.s.LiveConfig()
	lim := limitsOf(cfg.KeepGoing, row)
	counts := action != KeepScope
	if counts {
		n, err := k.running(ctx, ws)
		if err != nil {
			return refuse("runs unreadable: %v", err)
		}
		if n >= lim.runs {
			return refuse("%d of %d runs are running; keep-going starts nothing past that", n, lim.runs)
		}
	}
	day := k.day(k.Now())
	if counts && lane != 0 {
		n, err := k.s.Store.LaneStarts(ctx, lane, day)
		if err != nil {
			return refuse("lane starts unreadable: %v", err)
		}
		if max := cfg.KeepGoing.StartsPerLane(); n >= max {
			key := strconv.FormatInt(lane, 10) + "/" + day
			if !k.told[key] {
				k.told[key] = true
				k.s.postFeed(ctx, FeedKeepGoing, lane, fmt.Sprintf("keep-going handed lane %d back: %d autonomous starts today", lane, n))
			}
			return refuse("lane %d has had its %d autonomous starts today; it is handed back to the person", lane, max)
		}
		if _, err := k.s.Store.CountLaneStart(ctx, lane, day); err != nil {
			return refuse("lane starts unwritable: %v", err)
		}
	}
	row.LastAction = k.Now().UTC()
	if err := k.s.Store.SetKeepGoing(ctx, row); err != nil {
		return refuse("keep-going state unwritable: %v", err)
	}
	return Decision{OK: true}
}

// Status builds the view of a workspace's mode after checking its limits.
func (k *KeepGoing) Status(ctx context.Context, ws int64) (api.KeepGoingStatus, error) {
	row, err := k.Evaluate(ctx, ws)
	if err != nil {
		return api.KeepGoingStatus{}, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.Now()
	cfg := k.s.LiveConfig()
	lim := limitsOf(cfg.KeepGoing, row)
	out := api.KeepGoingStatus{
		WorkspaceID: ws, On: row.On, ReasonOff: row.ReasonOff, OffAt: row.OffAt,
		CapUSD: lim.cap, MaxRuns: lim.runs, IdleHours: lim.idle.Hours(), LaneStarts: cfg.KeepGoing.StartsPerLane(),
	}
	if row.On {
		out.Since, out.By, out.LastAction = row.Since, row.By, row.LastAction
		out.DayEnd = cfg.KeepGoing.DayEndAt(row.Since.In(now.Location()))
		out.ReasonOff, out.OffAt = "", time.Time{}
	}
	if out.SpentUSD, err = k.spentSince(ctx, row, now, cfg); err != nil {
		return out, err
	}
	if out.SpentTodayUSD, err = k.spentOn(ctx, k.day(now), cfg); err != nil {
		return out, err
	}
	if b := cfg.Daemon.Budget; b != nil {
		out.BudgetUSD = *b
	}
	if out.RunsNow, err = k.running(ctx, ws); err != nil {
		return out, err
	}
	return out, nil
}

// run checks the limits on a timer until quit closes.
func (k *KeepGoing) run(quit <-chan struct{}) {
	t := time.NewTicker(keepGoingTick)
	defer t.Stop()
	for {
		select {
		case <-quit:
			return
		case <-t.C:
			k.EvaluateAll(context.Background())
		}
	}
}

var errKeepGoingOn = errors.New("keep-going is turned on by the person: not by an agent, and by the front desk only in a turn the person started; off is always allowed")

// pickWorkspace finds the workspace a request names, or the only one, answering the
// request itself when it cannot.
func (s *Server) pickWorkspace(w http.ResponseWriter, r *http.Request, wsID int64) (store.Workspace, bool) {
	if wsID == 0 {
		wsID, _ = strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	}
	wss, err := s.Store.Workspaces(r.Context())
	if err != nil {
		s.fail(w, err)
		return store.Workspace{}, false
	}
	for _, ws := range wss {
		if ws.ID == wsID || (wsID == 0 && len(wss) == 1) {
			return ws, true
		}
	}
	switch {
	case wsID != 0:
		writeError(w, http.StatusNotFound, fmt.Errorf("no workspace %d", wsID))
	case len(wss) == 0:
		writeError(w, http.StatusConflict, errors.New("no workspace yet: run shepherd init"))
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("workspace_id is required: this daemon has %d workspaces", len(wss)))
	}
	return store.Workspace{}, false
}

// keepGoing answers GET and POST /v1/keep-going (api/keepgoing.go). Turning it on is
// the person's: the operator's CLI and a signed-in browser may, the front desk only in a
// turn the person started, and an agent's token never. Off is always allowed.
func (s *Server) keepGoing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	k := s.KeepGoing()
	if r.Method == http.MethodGet {
		ws, ok := s.pickWorkspace(w, r, 0)
		if !ok {
			return
		}
		st, err := k.Status(ctx, ws.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
		return
	}
	var req api.KeepGoingSet
	if !decode(w, r, &req) {
		return
	}
	if req.Cap < 0 || req.MaxRuns < 0 || req.IdleHours < 0 {
		writeError(w, http.StatusBadRequest, errors.New("cap, max_runs and idle_hours cannot be negative"))
		return
	}
	ws, ok := s.pickWorkspace(w, r, req.WorkspaceID)
	if !ok {
		return
	}
	p := principalOf(ctx)
	by := store.KeepByCLI
	switch p.Role {
	case RoleWeb:
		by = store.KeepByWeb
	case RoleDesk:
		by = store.KeepByDesk
	}
	if req.On && (p.Role == RoleWorker || (p.Role == RoleDesk && !p.Human)) {
		writeError(w, http.StatusForbidden, errKeepGoingOn)
		return
	}
	var err error
	if req.On {
		_, err = k.Enable(ctx, ws.ID, by, req)
	} else {
		_, err = k.Disable(ctx, ws.ID, by)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	st, err := k.Status(ctx, ws.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
