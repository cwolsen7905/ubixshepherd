// Package muster is the upkeep pass: it looks over artifacts, lanes, runs, next-up
// items, questions and decisions for what has gone stale, and either tidies it or
// raises it as a finding for the person's "To tidy" group.
//
// A fix that is safe and can be undone is applied on its own, as policy allows, with a
// feed line. Every other fix waits as a finding with a one-click action. Every finding
// and every applied fix is a store.MusterItem with its reason, so the log is the list,
// and an applied fix can be undone for a number of days. The checks are code: they read
// the store and the disk, and never call a model.
package muster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Fix names. A finding's Fix is one of these, applied by Pass.Apply.
const (
	FixArtifactGone    = "artifact.gone"     // mark a missing file's artifact gone
	FixArtifactSettle  = "artifact.settle"   // apply a closed lane's artifact intent
	FixArtifactKeepAll = "artifact.keep-all" // keep every artifact in a batch
	FixRunInterrupt    = "run.interrupt"     // a run whose process is dead
	FixLaneScope       = "lane.scope"        // apply an approved scope change
	FixNextReapprove   = "next.reapprove"    // back to approved, link to the lane cleared
	FixNextDone        = "next.done"         // mark an item done
	FixNextDrop        = "next.drop"         // drop an item
	FixQuestionClose   = "question.outdated" // close a question as out of date
	FixQuestionSkip    = "question.skip"     // mark a question skipped (stale)
	FixDecisionClose   = "decision.outdated" // close a decision as out of date
	FixAck             = ""                  // nothing to do but see it
)

// Probe is what the pass asks the machine: a seam so tests need not build processes.
type Probe struct {
	// Exists reports whether a path is there.
	Exists func(path string) bool
	// Branch reports whether a branch exists in the repo at path.
	Branch func(ctx context.Context, repoPath, branch string) bool
	// Alive reports whether a process is running.
	Alive func(pid int) bool
	// InBranch reports whether the branch holds the file.
	InBranch func(ctx context.Context, repoPath, branch, file string) bool
	// Snapshot reports whether a stored copy of the artifact's content exists.
	Snapshot func(artifactID int64, sha string) bool
	// Implemented says whether the requirements an item cites are now implemented, and
	// which. Nil: the pass cannot know, and proposes nothing.
	Implemented func(ctx context.Context, it store.NextItem) (bool, string)
}

// DefaultProbe asks the real machine. snapshotDir is where artifact copies are kept; ""
// means none are.
func DefaultProbe(snapshotDir string) Probe {
	return Probe{
		Exists: func(p string) bool { _, err := os.Stat(p); return err == nil },
		Branch: func(ctx context.Context, repoPath, branch string) bool {
			return git.RefExists(ctx, repoPath, "refs/heads/"+branch)
		},
		Alive: processAlive,
		InBranch: func(ctx context.Context, repoPath, branch, file string) bool {
			return git.Ok(ctx, repoPath, "cat-file", "-e", "refs/heads/"+branch+":"+file)
		},
		Snapshot: func(id int64, sha string) bool {
			if snapshotDir == "" || sha == "" {
				return false
			}
			st, err := os.Stat(fmt.Sprintf("%s/%d/%s", snapshotDir, id, sha))
			return err == nil && st.Mode().IsRegular()
		},
	}
}

// fillProbe gives every probe the pass does not set its real implementation.
func (p *Pass) fillProbe() {
	d := DefaultProbe("")
	if p.Probe.Exists == nil {
		p.Probe.Exists = d.Exists
	}
	if p.Probe.Branch == nil {
		p.Probe.Branch = d.Branch
	}
	if p.Probe.Alive == nil {
		p.Probe.Alive = d.Alive
	}
	if p.Probe.InBranch == nil {
		p.Probe.InBranch = d.InBranch
	}
	if p.Probe.Snapshot == nil {
		p.Probe.Snapshot = d.Snapshot
	}
}

// Pass is the upkeep pass.
type Pass struct {
	Store store.Store
	// Conf returns the live config; nil means the defaults.
	Conf  func() config.Config
	Log   *slog.Logger
	Clock func() time.Time
	Probe Probe
	// Gate says whether the pass may apply a fix on its own. The pass only offers it
	// fixes that are safe and can be undone; Gate is where policy (Crook) can still say
	// no, and then the fix waits as a finding. Nil allows them.
	Gate func(ctx context.Context, fix, repo string) (ok bool, reason string)

	// suspect holds runs seen with a dead process on the last pass, so a run that is
	// only now ending is not cut short (run.go).
	suspect map[int64]bool
}

func (p *Pass) now() time.Time {
	if p.Clock != nil {
		return p.Clock()
	}
	return time.Now()
}

func (p *Pass) conf() config.Config {
	if p.Conf != nil {
		return p.Conf()
	}
	return config.Default()
}

func (p *Pass) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

// candidate is a finding a check made, before it is stored.
type candidate struct {
	key         string
	kind        string
	subjectKind string
	subjectID   int64
	repo        string
	reason      string
	fix         string
	fixArg      any
	// auto: apply on its own when Crook allows. safe and undoable go to Crook's rule;
	// an auto fix that cannot be undone has no undo window.
	auto     bool
	undoable bool
}

// run is one pass in progress: which finding keys the checks saw, and the kinds they
// ran to the end, so an open finding that stopped being true can be cleared.
type run struct {
	p    *Pass
	seen map[string]bool
	ran  map[string]bool
}

// Run does one pass. A check that fails is logged and the rest go on. It returns the
// number of items it raised or applied.
func (p *Pass) Run(ctx context.Context) int {
	if p.conf().Muster.Off {
		return 0
	}
	p.fillProbe()
	r := &run{p: p, seen: map[string]bool{}, ran: map[string]bool{}}
	n := 0
	for _, c := range []struct {
		name  string
		check func(context.Context, *run) (int, error)
	}{
		{"artifacts", checkArtifacts},
		{"board", checkBoard},
		{"next", checkNext},
		{"inbox", checkInbox},
	} {
		got, err := c.check(ctx, r)
		n += got
		if err != nil {
			p.log().Warn("muster: check failed", "check", c.name, "err", err)
		}
	}
	n += r.clear(ctx)
	return n
}

// raise stores a finding, and applies it when it is an auto fix Crook allows. The same
// finding is raised once, whatever became of it.
func (r *run) raise(ctx context.Context, c candidate) (int, error) {
	r.seen[c.key] = true
	arg := ""
	if c.fixArg != nil {
		b, err := json.Marshal(c.fixArg)
		if err != nil {
			return 0, err
		}
		arg = string(b)
	}
	it, created, err := r.p.Store.AddMuster(ctx, store.MusterItem{
		Key: c.key, Kind: c.kind, SubjectKind: c.subjectKind, SubjectID: c.subjectID, Repo: c.repo,
		Reason: redact.String(c.reason), Fix: c.fix, FixArg: arg, Auto: c.auto,
	})
	if err != nil {
		return 0, err
	}
	if !created {
		// A finding still waiting says how things stand now, not as they were first seen.
		if it.State == store.MusterOpen && it.Reason != redact.String(c.reason) {
			why := redact.String(c.reason)
			r.p.Store.UpdateMuster(ctx, it.ID, store.MusterPatch{Reason: &why, State: nil})
		}
		return 0, nil
	}
	if c.auto && c.fix != "" {
		ok := true
		if r.p.Gate != nil {
			ok, _ = r.p.Gate(ctx, c.fix, c.repo)
		}
		if ok {
			if err := r.p.apply(ctx, it, c.undoable); err != nil {
				r.p.log().Warn("muster: apply a fix", "kind", c.kind, "subject", c.subjectID, "err", err)
				return 1, nil
			}
			return 1, nil
		}
	}
	r.p.feed(ctx, "muster: to tidy: %s", c.reason)
	return 1, nil
}

// clear marks open findings the checks no longer see as cleared, for the kinds that ran.
func (r *run) clear(ctx context.Context) int {
	open, err := r.p.Store.MusterItems(ctx, store.MusterFilter{State: store.MusterOpen})
	if err != nil {
		return 0
	}
	n := 0
	for _, it := range open {
		if !r.ran[it.Kind] || r.seen[it.Key] {
			continue
		}
		st := store.MusterCleared
		why := it.Reason + " (no longer true)"
		if _, err := r.p.Store.UpdateMuster(ctx, it.ID, store.MusterPatch{State: &st, Reason: &why}); err == nil {
			n++
		}
	}
	return n
}

func (p *Pass) feed(ctx context.Context, format string, a ...any) {
	if err := p.Store.AddFeed(ctx, store.FeedMuster, redact.String(fmt.Sprintf(format, a...)), 0); err != nil {
		p.log().Warn("muster: feed", "err", err)
	}
}

// ErrNotOpen is why Apply refuses an item: it is not an open finding.
var ErrNotOpen = errors.New("this item is not an open finding")

// Apply does an open finding's fix, at the person's click. A finding with no fix is
// only acknowledged (it becomes applied with nothing changed).
func (p *Pass) Apply(ctx context.Context, id int64) (store.MusterItem, error) {
	it, err := p.Store.MusterItem(ctx, id)
	if err != nil {
		return it, err
	}
	if it.State != store.MusterOpen {
		return it, ErrNotOpen
	}
	if err := p.apply(ctx, it, undoableFix(it.Fix)); err != nil {
		return it, err
	}
	return p.Store.MusterItem(ctx, id)
}

// Dismiss leaves an open finding alone; it is not raised again.
func (p *Pass) Dismiss(ctx context.Context, id int64) (store.MusterItem, error) {
	it, err := p.Store.MusterItem(ctx, id)
	if err != nil {
		return it, err
	}
	if it.State != store.MusterOpen {
		return it, ErrNotOpen
	}
	st := store.MusterDismissed
	return p.Store.UpdateMuster(ctx, id, store.MusterPatch{State: &st})
}

// apply runs the fix, records what it changed, and writes the feed line.
func (p *Pass) apply(ctx context.Context, it store.MusterItem, undoable bool) error {
	before, err := p.do(ctx, it)
	if err != nil {
		return err
	}
	t := p.now().UTC()
	st := store.MusterApplied
	patch := store.MusterPatch{State: &st, Before: &before, Applied: &t}
	if undoable {
		until := t.Add(time.Duration(p.conf().Muster.Undo()) * 24 * time.Hour)
		patch.UndoUntil = &until
	}
	if _, err := p.Store.UpdateMuster(ctx, it.ID, patch); err != nil {
		return err
	}
	who := "applied"
	if it.Auto {
		who = "tidied"
	}
	p.feed(ctx, "muster: %s: %s", who, it.Reason)
	return nil
}

// ErrNotUndoable is why Undo refuses an item.
var ErrNotUndoable = errors.New("this fix cannot be undone, or its undo window is over")

// Undo takes an applied fix back, within its window.
func (p *Pass) Undo(ctx context.Context, id int64) (store.MusterItem, error) {
	it, err := p.Store.MusterItem(ctx, id)
	if err != nil {
		return it, err
	}
	if it.State != store.MusterApplied || it.UndoUntil == nil || p.now().After(*it.UndoUntil) {
		return it, ErrNotUndoable
	}
	if err := p.undo(ctx, it); err != nil {
		return it, err
	}
	st := store.MusterUndone
	it, err = p.Store.UpdateMuster(ctx, id, store.MusterPatch{State: &st})
	if err == nil {
		p.feed(ctx, "muster: undone: %s", it.Reason)
	}
	return it, err
}
