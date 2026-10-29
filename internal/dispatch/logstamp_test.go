package dispatch

import (
	"bytes"
	"context"
	"os"
	"strings"
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
		{"header with a time", []string{"# shepherd: started 10:00:00\n"}, "# shepherd: started 10:00:00\n"},
		{"agent heading is agent text", []string{"# Plan\n"}, "21:27:04 # Plan\n"},
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

func TestRunLogLinesCarryATime(t *testing.T) {
	f := newFixture(t, "quick")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "task"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.wait(t, run.ID)
	b, err := os.ReadFile(run.Log)
	if err != nil {
		t.Fatal(err)
	}
	timed := 0
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		switch {
		case l == "" || strings.HasPrefix(l, "# shepherd run "), strings.HasPrefix(l, "# task:"), strings.HasPrefix(l, "# model:"), strings.HasPrefix(l, "# policy:"):
			// the header written before the agent starts
		case strings.HasPrefix(l, "# shepherd:"):
			if !hasClock.MatchString(l) {
				t.Errorf("header without a time: %q", l)
			}
		case StripStamp(l) == l:
			t.Errorf("line without a time: %q", l)
		default:
			timed++
		}
	}
	if timed == 0 {
		t.Errorf("no timed agent lines in:\n%s", b)
	}
}
