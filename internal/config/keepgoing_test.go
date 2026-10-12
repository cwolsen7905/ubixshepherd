package config

import (
	"strings"
	"testing"
	"time"
)

func TestKeepGoingDefaultsAndOverrides(t *testing.T) {
	c, err := Parse([]byte("keep_going:\n  cap: 12.5\n  max_runs: 3\n  idle_hours: 1.5\n  day_end: \"18:30\"\n  lane_starts: 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	k := c.KeepGoing
	if k.CapUSD() != 12.5 || k.Runs() != 3 || k.Idle() != 90*time.Minute || k.StartsPerLane() != 5 || k.DayEnd != "18:30" {
		t.Fatalf("overrides: %+v", k)
	}
	def, err := Parse([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	k = def.KeepGoing
	if k.CapUSD() != 5 || k.Runs() != 2 || k.Idle() != 4*time.Hour || k.StartsPerLane() != 3 {
		t.Fatalf("defaults: %+v", k)
	}
}

func TestKeepGoingValidation(t *testing.T) {
	for in, want := range map[string]string{
		"keep_going:\n  cap: -1\n":            "keep_going.cap",
		"keep_going:\n  max_runs: -2\n":       "keep_going.max_runs",
		"keep_going:\n  idle_hours: -1\n":     "keep_going.idle_hours",
		"keep_going:\n  lane_starts: -1\n":    "keep_going.lane_starts",
		"keep_going:\n  day_end: \"25:00\"\n": "keep_going.day_end",
		"keep_going:\n  day_end: \"6pm\"\n":   "keep_going.day_end",
		"keep_going:\n  nope: 1\n":            "nope",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want an error naming %s, got %v", in, want, err)
		}
	}
	for _, ok := range []string{"00:00", "24:00", "23:59", ""} {
		if _, err := Parse([]byte("keep_going:\n  day_end: \"" + ok + "\"\n")); err != nil {
			t.Errorf("day_end %q refused: %v", ok, err)
		}
	}
}

func TestKeepGoingDayEnd(t *testing.T) {
	loc := time.FixedZone("x", -5*3600)
	now := time.Date(2026, 10, 11, 17, 0, 0, 0, loc)
	for _, c := range []struct {
		end  string
		want time.Time
	}{
		{"", time.Date(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"24:00", time.Date(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"18:30", time.Date(2026, 10, 11, 18, 30, 0, 0, loc)},
		{"09:00", time.Date(2026, 10, 12, 9, 0, 0, 0, loc)},
	} {
		if got := (KeepGoing{DayEnd: c.end}).DayEndAt(now); !got.Equal(c.want) {
			t.Errorf("day_end %q: %v, want %v", c.end, got, c.want)
		}
	}
}
