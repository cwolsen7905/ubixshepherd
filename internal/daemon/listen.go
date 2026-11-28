package daemon

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// publicPollEvery is how often the public listener checks whether a config reload
// changed daemon.public. The reload path (SIGHUP, the settings route) does not know of
// the listener, so the listener watches the config in force instead.
const publicPollEvery = 500 * time.Millisecond

// publicListener runs hosted mode's listener for Run: it opens it when daemon.public is
// configured, and closes and re-opens it when a reload changes that section. Without
// daemon.public it does nothing at all.
type publicListener struct {
	s    *Server
	home string

	mu     sync.Mutex
	cur    *config.Public
	cancel context.CancelFunc
	done   chan struct{}

	watchStop chan struct{}
	watchDone chan struct{}
}

// startPublic prepares and starts the public listener for the daemon whose home is home.
// A daemon.public that cannot be prepared (no TLS or proxy, a bad key file) is an error
// that stops the start. Stop ends it.
func (s *Server) startPublic(ctx context.Context, home string) (*publicListener, error) {
	p := &publicListener{s: s, home: home, watchStop: make(chan struct{}), watchDone: make(chan struct{})}
	if err := p.open(ctx); err != nil {
		return nil, err
	}
	go p.watch(ctx)
	return p, nil
}

// open starts the listener for the config in force, if it names one.
func (p *publicListener) open(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pub := p.s.LiveConfig().Daemon.Public
	p.cur = clonePublic(pub)
	if !pub.Enabled() {
		return nil
	}
	if err := p.s.PreparePublic(ctx, p.home); err != nil {
		return err
	}
	h := p.s.hostedNow()
	lctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.cancel, p.done = cancel, done
	go func() {
		defer close(done)
		err := p.s.ServePublic(lctx)
		// Not serving any more, whatever the reason: stop reporting an address.
		if h != nil {
			h.addr.Store(nil)
		}
		if err != nil {
			p.s.Log.Error("public listener stopped", "err", err)
			p.s.db(context.Background()).AddFeed(context.Background(), store.FeedSession, fmt.Sprintf("the public listener is not serving: %v", err), 0)
		}
	}()
	return nil
}

// closeListener ends the running listener and waits for its drain.
func (p *publicListener) closeListener() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// watch re-opens the listener when a reload changed daemon.public. A change that cannot
// be prepared leaves the listener closed and says so: it never falls back to serving
// the old settings.
func (p *publicListener) watch(ctx context.Context) {
	defer close(p.watchDone)
	t := time.NewTicker(publicPollEvery)
	defer t.Stop()
	for {
		select {
		case <-p.watchStop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
		}
		p.mu.Lock()
		same := reflect.DeepEqual(p.cur, p.s.LiveConfig().Daemon.Public)
		p.mu.Unlock()
		if same {
			continue
		}
		p.s.Log.Info("daemon.public changed; restarting the public listener")
		p.closeListener()
		// Forget the old listener's state whatever comes next, so a new setting that
		// cannot be prepared leaves nothing that still reports the old address.
		p.s.web.hosted.mu.Lock()
		p.s.web.hosted.h = nil
		p.s.web.hosted.mu.Unlock()
		if err := p.open(ctx); err != nil {
			p.s.Log.Error("public listener not restarted", "err", err)
			p.s.db(context.Background()).AddFeed(context.Background(), store.FeedSession, fmt.Sprintf("the public listener is closed: %v", err), 0)
		}
	}
}

// Stop ends the watcher and the listener, and waits for the listener's drain.
func (p *publicListener) Stop() {
	if p == nil {
		return
	}
	close(p.watchStop)
	<-p.watchDone
	p.closeListener()
}

func clonePublic(p *config.Public) *config.Public {
	if p == nil {
		return nil
	}
	c := *p
	c.TrustedProxies = append([]string(nil), p.TrustedProxies...)
	return &c
}
