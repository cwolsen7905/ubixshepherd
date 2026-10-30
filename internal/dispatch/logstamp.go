package dispatch

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// StampLayout is the local 24-hour time a run log line starts with.
const StampLayout = "15:04:05"

var hasClock = regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`)

// StampWriter writes to w one line at a time, putting the local time at which the line
// was completed in front of it: "21:27:03 → Bash {...}". A partial write is held until
// its newline arrives (Flush writes a final unterminated line). Blank lines pass
// through bare. Everything written with Write is the agent's text and is stamped like
// any other line, whatever it starts with. Shepherd's own notes go through Note
// instead: "# shepherd: ..." keeps that form, with " at 21:27:03" on its end, so no
// agent line can pass for one. It is safe for concurrent use.
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
	if len(bytes.TrimSpace(line)) == 0 {
		return append(out, line...)
	}
	out = append(out, t.Format(StampLayout)...)
	out = append(out, ' ')
	return append(out, line...)
}

// Note writes one of Shepherd's own lines, such as "# shepherd: run 1 succeeded", with
// the time on its end. Any held partial line is written first.
func (s *StampWriter) Note(line string) error {
	if err := s.Flush(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line = strings.TrimRight(line, "\n")
	if !hasClock.MatchString(line) {
		line += " at " + s.now().Format(StampLayout)
	}
	_, err := io.WriteString(s.w, line+"\n")
	return err
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
