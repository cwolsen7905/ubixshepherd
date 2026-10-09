package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// RepeatWindow is how long an identical warning or error is held back after it was
// logged. The first is logged at once; the next one after the window carries a
// `repeated` attribute counting those held back since.
const RepeatWindow = 10 * time.Minute

// maxRepeatKeys bounds what the handler remembers; past it, entries older than the
// window are forgotten.
const maxRepeatKeys = 1024

// dedupHandler passes every record to inner, except a warning or error identical to one
// logged within the window: a poller whose server is unreachable fails the same way for
// every item every tick, and each failure would otherwise be a line. Identical means the
// same level, message and attributes.
type dedupHandler struct {
	inner  slog.Handler
	prefix string // the attributes and groups added by With, which tell loggers apart
	st     *dedupState
}

type dedupState struct {
	mu     sync.Mutex
	window time.Duration
	now    func() time.Time
	seen   map[string]*repeat
}

type repeat struct {
	last    time.Time
	dropped int
}

func newDedupHandler(inner slog.Handler) *dedupHandler {
	return &dedupHandler{inner: inner, st: &dedupState{window: RepeatWindow, now: time.Now, seen: map[string]*repeat{}}}
}

func (h *dedupHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *dedupHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn {
		return h.inner.Handle(ctx, r)
	}
	var key strings.Builder
	fmt.Fprintf(&key, "%d|%s|%s", r.Level, r.Message, h.prefix)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&key, "|%s=%v", a.Key, a.Value)
		return true
	})
	if n, log := h.st.admit(key.String()); !log {
		return nil
	} else if n > 0 {
		r = r.Clone()
		r.AddAttrs(slog.Int("repeated", n))
	}
	return h.inner.Handle(ctx, r)
}

// admit says whether a record with this key is logged now, and how many identical ones
// were held back since the last that was.
func (s *dedupState) admit(key string) (dropped int, log bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	e, ok := s.seen[key]
	if ok && now.Sub(e.last) < s.window {
		e.dropped++
		return 0, false
	}
	if !ok {
		if len(s.seen) >= maxRepeatKeys {
			for k, old := range s.seen {
				if now.Sub(old.last) >= s.window {
					delete(s.seen, k)
				}
			}
		}
		e = &repeat{}
		s.seen[key] = e
	}
	dropped, e.last, e.dropped = e.dropped, now, 0
	return dropped, true
}

func (h *dedupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var p strings.Builder
	p.WriteString(h.prefix)
	for _, a := range attrs {
		fmt.Fprintf(&p, "|%s=%v", a.Key, a.Value)
	}
	return &dedupHandler{inner: h.inner.WithAttrs(attrs), prefix: p.String(), st: h.st}
}

func (h *dedupHandler) WithGroup(name string) slog.Handler {
	return &dedupHandler{inner: h.inner.WithGroup(name), prefix: h.prefix + "|group=" + name, st: h.st}
}
