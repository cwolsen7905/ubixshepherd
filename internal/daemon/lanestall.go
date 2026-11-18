package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// FeedLaneStall is the feed kind of a lane stall alert. The event mapping does not list
// it, so clients show it as info until it is mapped.
const FeedLaneStall = "lane_stall"

// DefaultLaneStallAfter is how long an open lane may sit with unpushed commits or a
// failed pipeline, and no run, before the person is told.
const DefaultLaneStallAfter = 4 * time.Hour

// LaneStallOptions tunes the check. The zero value is the default.
type LaneStallOptions struct {
	// After is the quiet time that makes a lane stalled; 0 means DefaultLaneStallAfter.
	After time.Duration
	// Now is the clock; nil is time.Now. Tests set it.
	Now func() time.Time
	// Unpushed counts a lane's commits not on its branch at origin; an error means git
	// could not say, and the lane is left as it was for that pass. Nil asks git
	// (unpushedCount). Tests set it.
	Unpushed func(ctx context.Context, l store.Lane) (int, error)
}

// LaneStall alerts, once per stall, on an open lane that has unpushed commits or a
// failed pipeline and has had no run for a while. A stall ends, and may alert again
// later, when a run starts or the condition goes away.
type LaneStall struct {
	s    *Server
	opts LaneStallOptions
	mu   sync.Mutex
	told map[int64]string // lane id to the stall already announced (its cause)
}

// NewLaneStall returns the check for a server.
func NewLaneStall(s *Server, opts LaneStallOptions) *LaneStall {
	return &LaneStall{s: s, opts: opts.withDefaults(), told: map[int64]string{}}
}

// withDefaults fills what opts leaves unset.
func (opts LaneStallOptions) withDefaults() LaneStallOptions {
	if opts.After <= 0 {
		opts.After = DefaultLaneStallAfter
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Unpushed == nil {
		opts.Unpushed = unpushedCount
	}
	return opts
}

// setOptions replaces the options in place, keeping what was announced.
func (ls *LaneStall) setOptions(opts LaneStallOptions) {
	ls.mu.Lock()
	ls.opts = opts.withDefaults()
	ls.mu.Unlock()
}

// options is the options as they stand now.
func (ls *LaneStall) options() LaneStallOptions {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.opts
}

// laneStalls holds each server's check, so wiring needs no field on Server.
var laneStalls sync.Map // *Server to *LaneStall

// CheckLaneStalls is the one call that wires the check in: run it on each pass of the
// daemon's periodic work. The check is built on first use with the default options;
// to tune them, call SetLaneStallOptions before the first pass.
func (s *Server) CheckLaneStalls(ctx context.Context) {
	v, _ := laneStalls.LoadOrStore(s, NewLaneStall(s, LaneStallOptions{}))
	v.(*LaneStall).Check(ctx)
}

// SetLaneStallOptions sets the options of the server's check, creating it if need be.
// It keeps what the check had announced, and never loses to a pass racing it.
func (s *Server) SetLaneStallOptions(opts LaneStallOptions) {
	v, _ := laneStalls.LoadOrStore(s, NewLaneStall(s, opts))
	v.(*LaneStall).setOptions(opts)
}

// Check looks at every open lane in every workspace.
func (ls *LaneStall) Check(ctx context.Context) {
	wss, err := ls.s.Store.Workspaces(ctx)
	if err != nil {
		ls.s.Log.Warn("lane stall: list workspaces", "err", err)
		return
	}
	live := map[int64]bool{}
	for _, ws := range wss {
		repos, err := ls.s.Store.Repos(ctx, ws.ID)
		if err != nil {
			ls.s.Log.Warn("lane stall: list repos", "workspace", ws.ID, "err", err)
			return // forget nothing: a failed read must not re-arm the alerts
		}
		for _, rp := range repos {
			lanes, err := ls.s.Store.Lanes(ctx, rp.ID)
			if err != nil {
				ls.s.Log.Warn("lane stall: list lanes", "repo", rp.Name, "err", err)
				return
			}
			for _, l := range lanes {
				if l.State != store.LaneOpen {
					continue
				}
				live[l.ID] = true
				ls.checkLane(ctx, rp, l)
			}
		}
	}
	ls.mu.Lock()
	for id := range ls.told { // a lane that closed or vanished
		if !live[id] {
			delete(ls.told, id)
		}
	}
	ls.mu.Unlock()
}

func (ls *LaneStall) checkLane(ctx context.Context, rp store.Repo, l store.Lane) {
	opts := ls.options()
	runs, err := ls.s.Store.Runs(ctx, l.ID, "", 1)
	if err != nil {
		ls.s.Log.Warn("lane stall: read runs", "lane", l.Name, "err", err)
		return
	}
	quiet := l.Created
	if len(runs) > 0 {
		r := runs[0]
		if r.State == store.RunRunning {
			ls.clear(l.ID)
			return
		}
		quiet = r.Started
		if r.Ended != nil {
			quiet = *r.Ended
		}
	}
	if opts.Now().Sub(quiet) < opts.After {
		ls.clear(l.ID) // a run started or ended inside the window: not stalled
		return
	}
	cause, err := ls.cause(ctx, l, opts)
	if err != nil {
		// Unknown is not healthy: keep what was announced and look again next pass.
		ls.s.Log.Warn("lane stall: read lane state", "lane", l.Name, "err", err)
		return
	}
	if cause == "" {
		ls.clear(l.ID)
		return
	}
	ls.mu.Lock()
	seen := ls.told[l.ID]
	ls.told[l.ID] = cause
	ls.mu.Unlock()
	if seen != "" {
		return
	}
	ls.s.postFeed(ctx, FeedLaneStall, l.ID, redact.String(fmt.Sprintf(
		"lane %s in %s has had no run for %s and %s: open it, then push, fix or close it.",
		l.Name, rp.Name, opts.Now().Sub(quiet).Round(time.Minute), cause)))
}

// cause says what holds the lane, or "" when nothing does; an error when a read failed,
// since a lane that could not be read is not known to be healthy.
func (ls *LaneStall) cause(ctx context.Context, l store.Lane, opts LaneStallOptions) (string, error) {
	var parts []string
	n, err := opts.Unpushed(ctx, l)
	if err != nil {
		return "", err
	}
	if n > 0 {
		parts = append(parts, fmt.Sprintf("%d commit(s) are not pushed", n))
	}
	lf, err := ls.s.Store.LaneForge(ctx, l.ID)
	if err != nil {
		return "", err
	}
	if lf.MR != 0 && lf.PipelineStatus == "failed" && !mrDone(lf) {
		parts = append(parts, fmt.Sprintf("the pipeline for MR !%d failed", lf.MR))
	}
	switch len(parts) {
	case 0:
		return "", nil
	case 1:
		return parts[0], nil
	}
	return parts[0] + " and " + parts[1], nil
}

// unpushedCount is dispatch.Unpushed, which reads 0 when git fails, with the failures
// that matter here (a worktree that is gone, one git cannot read) reported as errors.
func unpushedCount(ctx context.Context, l store.Lane) (int, error) {
	if _, err := os.Stat(l.Worktree); err != nil {
		return 0, err
	}
	if _, err := git.Run(ctx, l.Worktree, "rev-parse", "--verify", "HEAD"); err != nil {
		return 0, err
	}
	return dispatch.Unpushed(ctx, l), nil
}

func (ls *LaneStall) clear(id int64) {
	ls.mu.Lock()
	delete(ls.told, id)
	ls.mu.Unlock()
}

// mrDone says the lane's request is merged or closed, so no pipeline of it is waited on.
// It matches the waiting list's reading of the same fields.
func mrDone(lf store.LaneForge) bool {
	return strings.EqualFold(lf.MRState, "merged") || strings.EqualFold(lf.MRState, "closed") || lf.MergeSHA != ""
}
