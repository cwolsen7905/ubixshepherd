package muster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/waiting"
)

// Board kinds of finding.
const (
	KindLaneGone     = "lane.gone"
	KindLaneStale    = "lane.stale"
	KindLaneMRClosed = "lane.mr-closed"
	KindLaneScope    = "lane.scope"
	KindRunDead      = "run.dead"
)

// graceNewRun is how long a run is left alone before its process is judged dead: its pid
// may not be recorded yet.
const graceNewRun = time.Minute

// checkBoard keeps the board true over the open lanes of every workspace:
//   - a lane whose worktree or branch is gone is flagged;
//   - a run marked running whose process is dead becomes interrupted (seen dead on two
//     passes in a row, so a run only now ending is not cut short);
//   - a lane with no activity for the stale days is flagged stale;
//   - a lane whose merge request was closed without merging asks;
//   - an approved scope change the lane never got is offered to apply.
//
// Retrying a push that failed on the network belongs to the dispatcher, not here.
func checkBoard(ctx context.Context, r *run) (int, error) {
	p := r.p
	for _, k := range []string{KindLaneGone, KindLaneStale, KindLaneMRClosed, KindLaneScope, KindRunDead} {
		r.ran[k] = true
	}
	wss, err := p.Store.Workspaces(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	staleDays := p.conf().Muster.Stale()
	now := p.now()
	dead := map[int64]bool{}
	answered, err := p.Store.Decisions(ctx, store.DecisionAnswered)
	if err != nil {
		return 0, err
	}
	var scopeAsks []store.Decision
	for _, d := range answered {
		if strings.Contains(strings.ToLower(d.Question), "scope") {
			scopeAsks = append(scopeAsks, d)
		}
	}
	runLane := map[int64]int64{}
	for _, ws := range wss {
		repos, err := p.Store.Repos(ctx, ws.ID)
		if err != nil {
			return n, err
		}
		for _, rp := range repos {
			lanes, err := p.Store.Lanes(ctx, rp.ID)
			if err != nil {
				return n, err
			}
			activity, _ := p.Store.LaneActivity(ctx, rp.ID)
			for _, l := range lanes {
				if l.State != store.LaneOpen {
					continue
				}
				got, err := r.checkLane(ctx, rp, l, activity, staleDays, now, dead)
				n += got
				if err != nil {
					return n, err
				}
			}
			for _, d := range scopeAsks {
				lid, ok := runLane[d.RunID]
				if !ok {
					if run, err := p.Store.Run(ctx, d.RunID); err == nil {
						lid = run.LaneID
					}
					runLane[d.RunID] = lid
				}
				for _, l := range lanes {
					if l.ID != lid || l.State != store.LaneOpen {
						continue
					}
					missing := waiting.ScopeMissing(l, d)
					if len(missing) == 0 {
						continue
					}
					got, err := r.raise(ctx, candidate{
						key:  fmt.Sprintf("%s:%d:%s", KindLaneScope, d.ID, strings.Join(missing, ",")),
						kind: KindLaneScope, subjectKind: "lane", subjectID: l.ID, repo: rp.Name,
						reason: fmt.Sprintf("decision %d approved widening lane %s to %s, but its scope is still %s",
							d.ID, l.Name, strings.Join(missing, ", "), strings.Join(l.Scope, ", ")),
						fix: FixLaneScope, fixArg: map[string][]string{"paths": missing},
					})
					n += got
					if err != nil {
						return n, err
					}
				}
			}
		}
	}
	p.suspect = dead
	return n, nil
}

func (r *run) checkLane(ctx context.Context, rp store.Repo, l store.Lane, activity map[int64]store.Activity, staleDays int, now time.Time, dead map[int64]bool) (int, error) {
	p := r.p
	n := 0
	var gone []string
	if l.Worktree != "" && !p.Probe.Exists(l.Worktree) {
		gone = append(gone, "worktree "+l.Worktree)
	}
	if l.Branch != "" && p.Probe.Branch != nil && !p.Probe.Branch(ctx, rp.Path, l.Branch) {
		gone = append(gone, "branch "+l.Branch)
	}
	if len(gone) > 0 {
		got, err := r.raise(ctx, candidate{
			key: fmt.Sprintf("%s:%d:%s", KindLaneGone, l.ID, strings.Join(gone, "+")), kind: KindLaneGone,
			subjectKind: "lane", subjectID: l.ID, repo: rp.Name,
			reason: fmt.Sprintf("lane %s is open but its %s is gone", l.Name, strings.Join(gone, " and ")),
		})
		n += got
		if err != nil {
			return n, err
		}
	}

	running := false
	runs, err := p.Store.Runs(ctx, l.ID, store.RunRunning, 20)
	if err != nil {
		return n, err
	}
	for _, rn := range runs {
		if rn.PID <= 0 || now.Sub(rn.Started) < graceNewRun {
			running = true
			continue
		}
		if p.Probe.Alive != nil && p.Probe.Alive(rn.PID) {
			running = true
			continue
		}
		dead[rn.ID] = true
		if !p.suspect[rn.ID] {
			running = true // first sighting: wait for the next pass
			continue
		}
		got, err := r.raise(ctx, candidate{
			key: fmt.Sprintf("%s:%d", KindRunDead, rn.ID), kind: KindRunDead,
			subjectKind: "run", subjectID: rn.ID, repo: rp.Name,
			reason: fmt.Sprintf("run %d in lane %s is marked running but its process (%d) is gone; it is now interrupted", rn.ID, l.Name, rn.PID),
			fix:    FixRunInterrupt, auto: true,
		})
		n += got
		if err != nil {
			return n, err
		}
	}

	if lf, err := p.Store.LaneForge(ctx, l.ID); err == nil && strings.EqualFold(lf.MRState, "closed") && lf.MergeSHA == "" {
		got, err := r.raise(ctx, candidate{
			key: fmt.Sprintf("%s:%d:%d", KindLaneMRClosed, l.ID, lf.MR), kind: KindLaneMRClosed,
			subjectKind: "lane", subjectID: l.ID, repo: rp.Name,
			reason: fmt.Sprintf("the merge request for lane %s was closed without merging: reopen it, or close the lane", l.Name),
		})
		n += got
		if err != nil {
			return n, err
		}
	}

	if staleDays > 0 && !running {
		last := l.Created
		if a, ok := activity[l.ID]; ok && a.At.After(last) {
			last = a.At
		}
		if now.Sub(last) >= time.Duration(staleDays)*24*time.Hour {
			got, err := r.raise(ctx, candidate{
				key: fmt.Sprintf("%s:%d:%s", KindLaneStale, l.ID, last.UTC().Format("2006-01-02")), kind: KindLaneStale,
				subjectKind: "lane", subjectID: l.ID, repo: rp.Name,
				reason: fmt.Sprintf("lane %s has had no activity since %s (%d days)", l.Name, last.UTC().Format("2006-01-02"), int(now.Sub(last).Hours()/24)),
			})
			n += got
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
}
