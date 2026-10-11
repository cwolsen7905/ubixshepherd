package fold

import (
	"context"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestCommitClock(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var clock CommitClock
	open := func(name, scope string) store.Lane {
		l, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: name, Scope: []string{scope}})
		if err != nil {
			t.Fatal(err)
		}
		return l.Lane
	}
	fresh := open("fresh", "docs/**")
	busy := open("busy", "src/**")
	times := func() map[int64]time.Time {
		t.Helper()
		lanes, err := f.fold.Store.Lanes(ctx, f.repo.ID)
		if err != nil {
			t.Fatal(err)
		}
		return clock.Times(ctx, f.repo.Path, lanes)
	}

	if got := times(); len(got) != 0 {
		t.Errorf("fresh lanes have commit times: %v", got)
	}
	f.commit(busy.Worktree, "src/a.go")
	got := times()
	at, ok := got[busy.ID]
	if !ok || time.Since(at) > time.Minute || time.Since(at) < -time.Minute {
		t.Fatalf("busy commit time = %v (%v)", at, got)
	}
	if _, ok := got[fresh.ID]; ok {
		t.Error("a lane with no commits of its own has a commit time")
	}

	// The base moving does not make a lane that never committed look active.
	f.commit(f.repo.Path, "other.txt")
	gitT(t, f.repo.Path, "push", "-q", "origin", "HEAD:main")
	if _, ok := times()[fresh.ID]; ok {
		t.Error("base moved: fresh lane has a commit time")
	}

	// A later commit moves the time (and only then is git log run again).
	time.Sleep(1100 * time.Millisecond)
	f.commit(busy.Worktree, "src/b.go")
	if next := times()[busy.ID]; !next.After(at) {
		t.Errorf("second commit %v not after first %v", next, at)
	}
}
