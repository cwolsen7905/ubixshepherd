package muster_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/muster"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
	"github.com/ubixsys/ubixshepherd/internal/watch"
)

// TestWatcherRunsThePassEachTick checks the wiring: the watcher's Check runs the pass.
func TestWatcherRunsThePassEachTick(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	it, _ := st.AddNextItem(ctx, store.NextItem{Title: "no repo here", State: store.NextApproved})

	w := &watch.Watcher{Store: st, Log: slog.Default(), Interval: time.Hour, Muster: &muster.Pass{Store: st}}
	w.Check(ctx)
	got, _ := st.MusterItems(ctx, store.MusterFilter{State: store.MusterOpen})
	if len(got) != 1 || got[0].Kind != muster.KindNextNoRepo || got[0].SubjectID != it.ID {
		t.Fatalf("after one tick: %+v", got)
	}

	// Without a pass, the watcher does as it did.
	st.AddNextItem(ctx, store.NextItem{Title: "another without one", State: store.NextApproved})
	(&watch.Watcher{Store: st, Log: slog.Default(), Interval: time.Hour}).Check(ctx)
	if got, _ := st.MusterItems(ctx, store.MusterFilter{}); len(got) != 1 {
		t.Errorf("a watcher with no pass made %d items", len(got))
	}
}
