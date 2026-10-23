package watch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/mergerule"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shadowTimeout bounds one lane's shadow pass: a few git reads and one store write.
var shadowTimeout = 2 * time.Minute

// shadowLane is the shadow pass's state for one lane. mu is held while a pass runs, so a
// pass for a lane never overlaps the next one for it; seen is what the last pass looked at.
type shadowLane struct {
	mu   sync.Mutex
	seen string
}

type shadowLaneKey struct{}

// shadowFeed is the Observer's feed: it puts the line in the feed of the lane whose id the
// pass carries in its context.
func (w *Watcher) shadowFeed(ctx context.Context, _, msg string) {
	if id, ok := ctx.Value(shadowLaneKey{}).(int64); ok {
		w.feed(ctx, store.FeedMR, id, "%s", msg)
	}
}

// EnableShadow turns on the merge rules' shadow pass (internal/mergerule): for each open
// lane merge request the watcher reads, it records what a shadow merge rule would have
// done. It only reads and records: it never merges, approves, retries or comments. The
// pass runs apart from the poll, one at a time per lane and never queued, so a slow git
// read cannot hold up a poll. Call it before Run.
func (w *Watcher) EnableShadow(o *mergerule.Observer) {
	o.Feed = w.shadowFeed
	o.Rec = boundedRecorder{o.Rec}
	if o.Log == nil {
		o.Log = w.Log
	}
	w.shadow = o
}

// boundedRecorder gives each write the pass's time limit. The observer detaches a write
// from its pass's context so a shutdown does not lose a row, which also drops the pass's
// deadline: this puts a deadline back and stops waiting for a write that ignores it, so a
// stuck store cannot keep a lane's pass (and its lock) forever.
type boundedRecorder struct{ crook.Recorder }

func (b boundedRecorder) Record(ctx context.Context, e crook.Entry) error {
	if b.Recorder == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, shadowTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Recorder.Record(ctx, e) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("record shadow verdict: %w", ctx.Err())
	}
}

// loadShadowLinks reads the next-up items once for the poll and keeps the ids of the
// lanes they are linked to, so a pass does not read the whole list for each lane.
func (w *Watcher) loadShadowLinks(ctx context.Context) {
	if w.shadow == nil {
		return
	}
	items, err := w.Store.NextItems(ctx)
	if err != nil {
		return // keep the last list: a failed read changes nothing
	}
	linked := map[int64]bool{}
	for _, it := range items {
		if it.LaneID != 0 && it.State != store.NextDropped {
			linked[it.LaneID] = true
		}
	}
	w.shadowMu.Lock()
	w.shadowLinked = linked
	w.shadowMu.Unlock()
}

// WaitShadow waits for the shadow passes in flight, for tests and shutdown.
func (w *Watcher) WaitShadow() { w.shadowWG.Wait() }

func (w *Watcher) shadowState(id int64) *shadowLane {
	w.shadowMu.Lock()
	defer w.shadowMu.Unlock()
	if w.shadowLanes == nil {
		w.shadowLanes = map[int64]*shadowLane{}
	}
	s := w.shadowLanes[id]
	if s == nil {
		s = &shadowLane{}
		w.shadowLanes[id] = s
	}
	return s
}

// shadowObserve starts a shadow pass for an open request. A pass already running for the
// lane is not queued behind: the next poll looks again.
func (w *Watcher) shadowObserve(ctx context.Context, lane store.Lane, mr *forge.MR, reviewOK bool) {
	if w.shadow == nil || mr.State != "opened" || mr.SHA == "" {
		return
	}
	s := w.shadowState(lane.ID)
	if !s.mu.TryLock() {
		return
	}
	snap := *mr
	if mr.Pipeline != nil {
		p := *mr.Pipeline
		snap.Pipeline = &p
	}
	w.shadowGo(ctx, lane, func(ctx context.Context) error {
		defer s.mu.Unlock()
		return w.shadowRun(ctx, s, lane, &snap, reviewOK)
	})
}

// shadowMerged tells the pass the person merged the request. It waits for a pass running
// for the lane, so the outcome row follows the verdict it compares with.
func (w *Watcher) shadowMerged(ctx context.Context, lane store.Lane, mr *forge.MR) {
	if w.shadow == nil {
		return
	}
	s := w.shadowState(lane.ID)
	mrn, url, sha, merge := mr.IID, mr.URL, mr.SHA, mr.MergeSHA
	w.shadowGo(ctx, lane, func(ctx context.Context) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		repo, err := w.Store.Repo(ctx, lane.RepoID)
		if err != nil {
			return err
		}
		return w.shadow.Merged(ctx, mergerule.Input{Repo: repo.Name, Lane: lane.Name, Head: sha, MR: mrn, MRURL: url}, merge)
	})
}

// shadowGo runs fn apart from the poll with a time limit. The pass lives on the context
// Check was given, which Run takes from the daemon and cancels only at shutdown, not on a
// context that ends when the poll returns; a caller that gave Check such a context would
// cancel its passes with it. Whatever goes wrong is logged
// and records nothing: a shadow pass that fails leaves the repo person-merged.
func (w *Watcher) shadowGo(ctx context.Context, lane store.Lane, fn func(context.Context) error) {
	ctx = context.WithValue(ctx, shadowLaneKey{}, lane.ID)
	w.shadowWG.Add(1)
	go func() {
		defer w.shadowWG.Done()
		ctx, cancel := context.WithTimeout(ctx, shadowTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				w.Log.Error("watch: shadow merge rule panicked", "lane", lane.Name, "panic", fmt.Sprint(r))
			}
		}()
		if err := fn(ctx); err != nil {
			w.Log.Warn("watch: shadow merge rule", "lane", lane.Name, "err", redact.String(err.Error()))
		}
	}()
}

// shadowRun gathers a request's facts from proof (the store's gate record, the forge's
// pipeline, git) and asks the observer. It skips work when nothing it reads has changed
// since the last pass.
func (w *Watcher) shadowRun(ctx context.Context, s *shadowLane, lane store.Lane, mr *forge.MR, reviewOK bool) error {
	repo, err := w.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return err
	}
	base := "origin/" + lane.Base
	baseSHA, err := git.Run(ctx, repo.Path, "rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil {
		baseSHA = "" // the observer reports an unreadable base once
	}
	in := mergerule.Input{
		Repo: repo.Name, Lane: lane.Name, Dir: repo.Path, Base: base, Head: mr.SHA,
		MR: mr.IID, MRURL: mr.URL, OpenThreads: -1, AIReview: "unreviewed",
	}
	if p := mr.Pipeline; p != nil && p.SHA == mr.SHA {
		in.Pipeline = p.Status // a pipeline for another commit says nothing about this head
	}
	if reviewOK {
		in.AIReview = "ok"
	}
	w.shadowMu.Lock()
	in.ItemLinked = w.shadowLinked[lane.ID] // requirements and approved checks are not recorded yet: the rule cannot be met
	w.shadowMu.Unlock()
	if g, err := w.Store.LastGateResult(ctx, lane.ID); err == nil && g != nil {
		in.GateSHA, in.GatePassed = g.SHA, g.Passed
	}
	key := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%t|%t", mr.IID, mr.SHA, baseSHA, in.Pipeline, in.GateSHA, in.AIReview, in.GatePassed, in.ItemLinked)
	if key == s.seen {
		return nil
	}
	if _, err := w.shadow.Observe(ctx, in); err != nil {
		return err
	}
	s.seen = key
	return nil
}

// reviewPassed is whether the AI review job passed on the request's head pipeline.
func (w *Watcher) reviewPassed(lane int64, mr *forge.MR) bool {
	ok := w.reviewRead[lane].ok
	return ok != 0 && mr.Pipeline != nil && mr.Pipeline.ID == ok
}
