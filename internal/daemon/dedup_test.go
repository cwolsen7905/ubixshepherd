package daemon

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func dedupLogger(buf *bytes.Buffer, now *time.Time) *slog.Logger {
	h := newDedupHandler(slog.NewTextHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	h.st.now = func() time.Time { return *now }
	return slog.New(h)
}

func TestRepeatedErrorsCollapse(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	log := dedupLogger(&buf, &now)
	const timeout = "dial tcp 192.0.2.1:443: i/o timeout"

	// Three lanes fail every minute for 25 minutes.
	for minute := 0; minute < 25; minute++ {
		for _, lane := range []string{"a", "b", "c"} {
			log.Error("watch: merge request", "lane", lane, "err", timeout)
		}
		now = now.Add(time.Minute)
	}
	got := buf.String()
	// Once at the start, at 10 minutes and at 20 minutes, per lane.
	if n := strings.Count(got, "msg=\"watch: merge request\""); n != 9 {
		t.Errorf("%d lines for 75 failures, want 9:\n%s", n, got)
	}
	for _, want := range []string{"lane=a err=\"" + timeout + "\"\n", "lane=b err=\"" + timeout + "\" repeated=9\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "repeated=9"); n != 6 {
		t.Errorf("%d lines carry the count, want 6 (3 lanes, 2 summaries):\n%s", n, got)
	}
}

func TestDifferentErrorsAreNotCollapsed(t *testing.T) {
	var buf bytes.Buffer
	now := time.Now()
	log := dedupLogger(&buf, &now)
	log.Error("watch", "err", "timeout")
	log.Error("watch", "err", "connection refused")        // another error
	log.Warn("watch", "err", "timeout")                    // another level
	log.With("lane", "x").Error("watch", "err", "timeout") // another logger
	if n := strings.Count(buf.String(), "\n"); n != 4 {
		t.Errorf("%d lines, want 4:\n%s", n, buf.String())
	}
}

// Info and debug records are never held back, and a failure that recurs after the
// window is logged again even with nothing counted.
func TestInfoIsNeverCollapsed(t *testing.T) {
	var buf bytes.Buffer
	now := time.Now()
	log := dedupLogger(&buf, &now)
	for i := 0; i < 5; i++ {
		log.Info("tick")
	}
	log.Error("down")
	now = now.Add(RepeatWindow)
	log.Error("down")
	if n := strings.Count(buf.String(), "\n"); n != 7 {
		t.Errorf("%d lines, want 7:\n%s", n, buf.String())
	}
	if strings.Contains(buf.String(), "repeated") {
		t.Errorf("a count with nothing held back:\n%s", buf.String())
	}
}

func TestDedupForgetsOldKeys(t *testing.T) {
	var buf bytes.Buffer
	now := time.Now()
	h := newDedupHandler(slog.NewTextHandler(&buf, nil))
	h.st.now = func() time.Time { return now }
	for i := 0; i < maxRepeatKeys; i++ {
		h.st.admit(string(rune('a'+i%26)) + strings.Repeat("x", i))
	}
	now = now.Add(RepeatWindow)
	h.st.admit("fresh")
	if len(h.st.seen) != 1 {
		t.Errorf("%d keys remembered, want 1", len(h.st.seen))
	}
}
