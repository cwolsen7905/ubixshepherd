package daemon

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/desk"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

// A run ending in the feed starts a keep-going pass, but the feed poll does not wait for
// it: with the drive stuck in a pass, the poll still reads the feed and returns.
func TestFeedPollDoesNotWaitForTheDrive(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := NewServer(st, config.Default(), "/cfg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.DeskAgent = nopDesk{}
	var passes atomic.Int32
	started, release := make(chan struct{}, 1), make(chan struct{})
	s.KeepGoing().pass = func(context.Context) {
		if passes.Add(1) == 1 {
			started <- struct{}{}
		}
		<-release
	}
	// The hub and the drive are built by hand, not started with background(), so the
	// test is the hub's only poller and the feed row is read exactly once.
	s.desks = desk.NewManager(desk.Options{
		Store: st, Agent: nopDesk{}, Tokens: s, Log: s.Log,
		Wake: func() string { return config.WakeNever }, Config: s.LiveConfig,
	})
	t.Cleanup(s.desks.Close)
	s.hub = newFeedHub(s)
	quit, done := make(chan struct{}), make(chan struct{})
	go func() { defer close(done); s.KeepGoing().run(quit) }()
	t.Cleanup(func() { <-done })
	t.Cleanup(func() { close(quit) })
	t.Cleanup(func() { close(release) }) // runs first: a stuck pass must return

	if err := st.AddFeed(ctx, store.FeedRunEnded, "run ended", 1); err != nil {
		t.Fatal(err)
	}
	polled := make(chan struct{})
	go func() { s.hub.poll(); close(polled) }()
	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("the feed poll is waiting on the keep-going drive")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("a run ending did not start a drive pass")
	}
	// The drive is stuck mid-pass; the next run ending is still read at once.
	if err := st.AddFeed(ctx, store.FeedRunFailed, "run failed", 1); err != nil {
		t.Fatal(err)
	}
	polled = make(chan struct{})
	go func() { s.hub.poll(); close(polled) }()
	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("the feed poll blocked behind a slow drive pass")
	}
}
