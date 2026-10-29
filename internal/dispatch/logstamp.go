package dispatch

import (
	"bytes"
	"io"
	"regexp"
	"sync"
	"time"
)

// StampLayout is the local 24-hour time a run log line starts with.
const StampLayout = "15:04:05"

var hasClock = regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`)

// StampWriter writes to w one line at a time, putting the local time at which the line
// was completed in front of it: "21:27:03 → Bash {...}". A partial write is held until
// its newline arrives (Flush writes a final unterminated line). Blank lines pass
// through bare. Shepherd's own note ("# shepherd: ...") keeps its form and gets
// " at 21:27:03" on its end instead, unless it already shows a time. An agent's own
// "# heading" is agent text and is stamped like any other line. It is safe for concurrent use.
type StampWriter struct {
	mu  sync.Mutex
	w   io.Writer
	now func() time.Time
	buf []byte
}

// NewStampWriter wraps w. now is the clock; nil means time.Now.
func NewStampWriter(w io.Writer, now func() time.Time) *StampWriter {
	if now == nil {
		now = time.Now
	}
	return &StampWriter{w: w, now: now}
}

// Write reports len(p) once the complete lines in it are written.
func (s *StampWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	var out []byte
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		out = s.stamp(out, s.buf[:i], s.now())
		out = append(out, '\n')
		s.buf = s.buf[i+1:]
	}
	if len(s.buf) == 0 {
		s.buf = nil
	}
	if len(out) > 0 {
		if _, err := s.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes a held unterminated line, stamped now, with its newline.
func (s *StampWriter) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return nil
	}
	out := append(s.stamp(nil, s.buf, s.now()), '\n')
	s.buf = nil
	_, err := s.w.Write(out)
	return err
}

func (s *StampWriter) stamp(out, line []byte, t time.Time) []byte {
	clock := t.Format(StampLayout)
	switch {
	case len(bytes.TrimSpace(line)) == 0:
		return append(out, line...)
	case bytes.HasPrefix(line, []byte("# shepherd:")):
		out = append(out, line...)
		if !hasClock.Match(line) {
			out = append(out, " at "+clock...)
		}
		return out
	}
	out = append(out, clock...)
	out = append(out, ' ')
	return append(out, line...)
}

// StripStamp returns line without the time StampWriter put in front of it, so a reader
// accepts old logs (no time) and new ones alike. It strips one time only, and every line
// StampWriter writes has one, so agent text that itself starts with a clock reads back
// whole. Only a line from an old log, which has no stamp, can lose a leading clock.
func StripStamp(line string) string {
	if len(line) > len(StampLayout) && line[len(StampLayout)] == ' ' {
		if _, err := time.Parse(StampLayout, line[:len(StampLayout)]); err == nil {
			return line[len(StampLayout)+1:]
		}
	}
	return line
}
