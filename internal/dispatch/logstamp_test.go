package dispatch

import (
	"bytes"
	"testing"
	"time"
)

func stampClock() func() time.Time {
	t := time.Date(2026, 10, 28, 21, 27, 3, 0, time.Local)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func TestStampWriter(t *testing.T) {
	for _, c := range []struct {
		name   string
		writes []string
		want   string
	}{
		{"one line", []string{"→ Bash {}\n"}, "21:27:04 → Bash {}\n"},
		{"partial writes join", []string{"hel", "lo ", "there\n"}, "21:27:04 hello there\n"},
		{"multi-line write", []string{"a\nb\nc\n"}, "21:27:04 a\n21:27:05 b\n21:27:06 c\n"},
		{"held tail", []string{"a\nb"}, "21:27:04 a\n"},
		{"blank lines bare", []string{"a\n\nb\n"}, "21:27:04 a\n\n21:27:06 b\n"},
		{"header keeps form", []string{"\n# shepherd: run 1 succeeded\n"}, "\n# shepherd: run 1 succeeded at 21:27:05\n"},
		{"header with a time", []string{"# started 10:00:00\n"}, "# started 10:00:00\n"},
	} {
		var b bytes.Buffer
		w := NewStampWriter(&b, stampClock())
		for _, s := range c.writes {
			if n, err := w.Write([]byte(s)); err != nil || n != len(s) {
				t.Fatalf("%s: Write = %d, %v", c.name, n, err)
			}
		}
		if got := b.String(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStampWriterFlush(t *testing.T) {
	var b bytes.Buffer
	w := NewStampWriter(&b, stampClock())
	w.Write([]byte("done"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if want := "21:27:04 done\n"; b.String() != want {
		t.Errorf("%q, want %q", b.String(), want)
	}
	if err := w.Flush(); err != nil || b.Len() != len("21:27:04 done\n") {
		t.Errorf("second flush wrote more: %q, %v", b.String(), err)
	}
}

func TestStripStamp(t *testing.T) {
	for in, want := range map[string]string{
		"21:27:03 → Bash {}": "→ Bash {}",
		"→ Bash {}":          "→ Bash {}",
		"99:99:99 text":      "99:99:99 text",
		"21:27:03":           "21:27:03",
		"":                   "",
	} {
		if got := StripStamp(in); got != want {
			t.Errorf("StripStamp(%q) = %q, want %q", in, got, want)
		}
	}
}
