package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// KeepGoing holds the limits of keep-going mode. A zero value takes its default, so a
// file that says nothing gets the conservative ones; the CLI's flags override cap, runs
// and idle hours for one session.
type KeepGoing struct {
	// Cap is the dollars the mode may spend after it is turned on; 0 is DefaultKeepCap.
	Cap float64 `yaml:"cap,omitempty" json:"cap,omitempty"`
	// MaxRuns is how many runs may be running while it is on, counting every run in the
	// workspace; 0 is DefaultKeepRuns.
	MaxRuns int `yaml:"max_runs,omitempty" json:"max_runs,omitempty"`
	// IdleHours is how long with nothing started or answered before it turns itself
	// off; 0 is DefaultKeepIdleHours.
	IdleHours float64 `yaml:"idle_hours,omitempty" json:"idle_hours,omitempty"`
	// DayEnd is the local time of day it turns itself off, HH:MM; "" is midnight.
	DayEnd string `yaml:"day_end,omitempty" json:"day_end,omitempty"`
	// LaneStarts is how many autonomous starts a lane may have in a day before it is
	// skipped; 0 is DefaultKeepLaneStarts.
	LaneStarts int `yaml:"lane_starts,omitempty" json:"lane_starts,omitempty"`
}

// Keep-going's defaults, from docs/keep-going.md.
const (
	DefaultKeepCap        = 5.0
	DefaultKeepRuns       = 2
	DefaultKeepIdleHours  = 4.0
	DefaultKeepLaneStarts = 3
)

// CapUSD is the spend cap in dollars.
func (k KeepGoing) CapUSD() float64 {
	if k.Cap > 0 {
		return k.Cap
	}
	return DefaultKeepCap
}

// Runs is the concurrent-run limit.
func (k KeepGoing) Runs() int {
	if k.MaxRuns > 0 {
		return k.MaxRuns
	}
	return DefaultKeepRuns
}

// Idle is how long the mode may sit with nothing started or answered.
func (k KeepGoing) Idle() time.Duration {
	h := k.IdleHours
	if h <= 0 {
		h = DefaultKeepIdleHours
	}
	return time.Duration(h * float64(time.Hour))
}

// StartsPerLane is the per-lane, per-day limit on autonomous starts.
func (k KeepGoing) StartsPerLane() int {
	if k.LaneStarts > 0 {
		return k.LaneStarts
	}
	return DefaultKeepLaneStarts
}

// DayEndAt is the first moment at or after now that is the day's end, in now's zone.
// Midnight ("", 00:00 or 24:00) is the next one; a time that has passed today is
// tomorrow's.
func (k KeepGoing) DayEndAt(now time.Time) time.Time {
	h, m, _ := parseClock(k.DayEnd)
	end := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !end.After(now) {
		end = time.Date(now.Year(), now.Month(), now.Day()+1, h, m, 0, 0, now.Location())
	}
	return end
}

// parseClock reads HH:MM; "" and 24:00 are midnight.
func parseClock(s string) (h, m int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	hs, ms, ok := strings.Cut(s, ":")
	if !ok || len(hs) != 2 || len(ms) != 2 {
		return 0, 0, fmt.Errorf("%q is not HH:MM", s)
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || m < 0 || m > 59 || h < 0 || h > 24 || (h == 24 && m != 0) {
		return 0, 0, fmt.Errorf("%q is not a time of day (HH:MM, 00:00 to 24:00)", s)
	}
	return h % 24, m, nil
}

func (k KeepGoing) validate() []error {
	var errs []error
	if k.Cap < 0 {
		errs = append(errs, fmt.Errorf("keep_going.cap: %v cannot be negative", k.Cap))
	}
	if k.MaxRuns < 0 {
		errs = append(errs, fmt.Errorf("keep_going.max_runs: %d cannot be negative", k.MaxRuns))
	}
	if k.IdleHours < 0 {
		errs = append(errs, fmt.Errorf("keep_going.idle_hours: %v cannot be negative", k.IdleHours))
	}
	if k.LaneStarts < 0 {
		errs = append(errs, fmt.Errorf("keep_going.lane_starts: %d cannot be negative", k.LaneStarts))
	}
	if _, _, err := parseClock(k.DayEnd); err != nil {
		errs = append(errs, fmt.Errorf("keep_going.day_end: %w", err))
	}
	return errs
}
