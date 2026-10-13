// Package update checks whether a newer Shepherd release exists. The daemon owns the
// check: it asks GitHub's releases API about once a day, caches the answer and serves it
// to every client, so no client calls GitHub itself. It never downloads or installs
// anything; installing it is install.go (docs/update-check.md).
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Channels: beta takes every release, stable skips pre-releases.
const (
	ChannelBeta   = "beta"
	ChannelStable = "stable"
)

// Defaults for a Checker's fields left at zero.
const (
	// DefaultURL lists the project's releases, pre-releases included.
	DefaultURL = "https://api.github.com/repos/ubixsys/ubixshepherd/releases?per_page=30"
	// DefaultInterval is how long after an attempt, good or failed, the next one is made.
	DefaultInterval = 24 * time.Hour
	// DefaultFirstDelay is how long after start the first attempt waits, so a daemon
	// that is started and stopped quickly never calls out.
	DefaultFirstDelay = time.Minute
	// maxBody bounds what one answer may weigh; maxNotes bounds the changelog kept.
	maxBody  = 4 << 20
	maxNotes = 8000
)

// Asset is a file attached to a release. The check keeps them so the upgrade step can
// pick its archive without asking again.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size,omitempty"`
}

// Release is one published release.
type Release struct {
	// Version is the tag, as published (v0.5.0-beta.3).
	Version    string    `json:"version"`
	URL        string    `json:"url"`
	Notes      string    `json:"notes,omitempty"`
	Prerelease bool      `json:"prerelease,omitempty"`
	Published  time.Time `json:"published,omitzero"`
	Assets     []Asset   `json:"assets,omitempty"`
}

// Status is what the daemon knows about updates right now.
type Status struct {
	// Enabled is update.check; a disabled check keeps whatever it last found.
	Enabled bool
	Channel string
	// Current is this build's version; CurrentKnown says it is a semantic version, so it
	// can be compared. A dev build or source install is not.
	Current      string
	CurrentKnown bool
	// Latest is the newest release on the channel; nil before a check has succeeded.
	Latest *Release
	// Available says Latest is newer than a known Current. It is false when Current is
	// unknown: nothing was compared.
	Available bool
	// Dismissed says the person dismissed Latest's version.
	Dismissed bool
	// CheckedAt is the last successful check; LastError the last failed one, cleared by
	// a good one.
	CheckedAt time.Time
	LastError string
	// FailedVersion is the release an upgrade last failed for, and FailedWhy why;
	// RolledBack says the new binary started badly and the old one was put back.
	FailedVersion, FailedWhy string
	RolledBack               bool
}

// Checker polls the releases API and holds the answer. The zero value is not usable:
// set Current and Settings, then call Run.
type Checker struct {
	// URL is the releases list; "" is DefaultURL. Tests point it at a fake server.
	URL    string
	Client *http.Client
	// Current is the daemon's build version.
	Current string
	// Settings reports update.check and update.channel as now configured, so a reload
	// applies without a restart.
	Settings func() (enabled bool, channel string)
	// StatePath is where a dismissal is kept; "" keeps it in memory only.
	StatePath string
	// Interval and FirstDelay default to DefaultInterval and DefaultFirstDelay.
	Interval, FirstDelay time.Duration
	// Tick is how often Run looks at the settings and the clock; zero is the shorter of
	// Interval and ten minutes.
	Tick time.Duration
	Log  *slog.Logger
	Now  func() time.Time

	mu        sync.Mutex
	releases  []Release // every published non-draft release with a semantic version
	checkedAt time.Time
	attempted time.Time
	lastErr   string
	dismissed string
	failed    state // an upgrade that failed or was rolled back, kept so auto-upgrade does not retry it
	loaded    bool
}

type state struct {
	Dismissed string `json:"dismissed,omitempty"`
	// Failed is the version an upgrade was tried for and did not take; FailedWhy says why.
	// RolledBack marks a failure that was a binary restored after a bad start.
	Failed     string `json:"failed,omitempty"`
	FailedWhy  string `json:"failed_why,omitempty"`
	RolledBack bool   `json:"rolled_back,omitempty"`
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Run checks until ctx ends: first after FirstDelay, then every Interval. A check that
// fails is logged at debug and tried again at the next interval; one that is switched off
// by update.check is skipped, and picked up again when the setting is turned back on.
func (c *Checker) Run(ctx context.Context) {
	first := c.FirstDelay
	if first <= 0 {
		first = DefaultFirstDelay
	}
	interval := c.interval()
	// Wake often enough to notice a reload that turns the check on, and let an attempt
	// wait out its interval.
	tick := c.Tick
	if tick <= 0 {
		tick = min(interval, 10*time.Minute)
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if enabled, _ := c.Settings(); enabled && c.due() {
			if err := c.Check(ctx); err != nil && ctx.Err() == nil {
				c.log().Debug("update check failed; trying again at the next interval", "err", err)
			}
		}
		t.Reset(tick)
	}
}

func (c *Checker) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultInterval
}

// due says an attempt is past its interval.
func (c *Checker) due() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempted.IsZero() || c.now().Sub(c.attempted) >= c.interval()
}

// Check asks the releases API once and caches what it finds. On an error the previous
// answer stays.
func (c *Checker) Check(ctx context.Context) error {
	rels, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempted = c.now()
	if err != nil {
		c.lastErr = err.Error()
		return err
	}
	c.releases, c.checkedAt, c.lastErr = rels, c.attempted, ""
	return nil
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	URL        string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Published  string `json:"published_at"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (c *Checker) fetch(ctx context.Context) ([]Release, error) {
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	hc := c.Client
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "shepherd-update-check/"+c.Current)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("releases API refused the request (%s): rate limited?", resp.Status)
		}
		return nil, fmt.Errorf("releases API answered %s", resp.Status)
	}
	var raw []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("releases API answer: %w", err)
	}
	var out []Release
	for _, g := range raw {
		if g.Draft {
			continue
		}
		v, ok := parseSemver(g.Tag)
		if !ok {
			continue
		}
		r := Release{Version: g.Tag, URL: g.URL, Notes: g.Body, Prerelease: g.Prerelease || v.prerelease()}
		if len(r.Notes) > maxNotes {
			r.Notes = r.Notes[:maxNotes] + "\n..."
		}
		r.Published, _ = time.Parse(time.RFC3339, g.Published)
		for _, a := range g.Assets {
			r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
		}
		out = append(out, r)
	}
	return out, nil
}

// Status is the cached answer against the settings now in force.
func (c *Checker) Status() Status {
	enabled, channel := c.Settings()
	if channel != ChannelStable {
		channel = ChannelBeta
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	st := Status{Enabled: enabled, Channel: channel, Current: c.Current, CheckedAt: c.checkedAt, LastError: c.lastErr,
		FailedVersion: c.failed.Failed, FailedWhy: c.failed.FailedWhy, RolledBack: c.failed.RolledBack}
	cur, known := parseSemver(c.Current)
	st.CurrentKnown = known
	var best *Release
	var bestV semver
	for i := range c.releases {
		r := &c.releases[i]
		if channel == ChannelStable && r.Prerelease {
			continue
		}
		v, _ := parseSemver(r.Version)
		if best == nil || v.compare(bestV) > 0 {
			best, bestV = r, v
		}
	}
	if best != nil {
		cp := *best
		st.Latest = &cp
		st.Available = known && bestV.compare(cur) > 0
		st.Dismissed = c.dismissed == best.Version
	}
	return st
}

// Dismiss hides the notice for version until a newer one is published. It is stored, so
// a restart keeps it.
func (c *Checker) Dismiss(version string) error {
	if _, ok := parseSemver(version); !ok {
		return fmt.Errorf("%q is not a release version", version)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	if c.StatePath != "" {
		next := c.failed
		next.Dismissed = version
		if err := writeState(c.StatePath, next); err != nil {
			return err
		}
	}
	c.dismissed = version
	return nil
}

// MarkFailed records that upgrading to version failed (or was rolled back), so an
// automatic upgrade does not try it again. A newer release is tried afresh.
func (c *Checker) MarkFailed(version, why string, rolledBack bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	c.failed = state{Failed: version, FailedWhy: why, RolledBack: rolledBack}
	return c.saveLocked()
}

// ClearFailed forgets a recorded failure, as a new attempt by the person does.
func (c *Checker) ClearFailed() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	if c.failed.Failed == "" {
		return nil
	}
	c.failed = state{}
	return c.saveLocked()
}

func (c *Checker) saveLocked() error {
	if c.StatePath == "" {
		return nil
	}
	next := c.failed
	next.Dismissed = c.dismissed
	return writeState(c.StatePath, next)
}

func (c *Checker) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.StatePath == "" {
		return
	}
	b, err := os.ReadFile(c.StatePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.log().Warn("update state unreadable; treating it as empty", "err", err)
		}
		return
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		c.log().Warn("update state unreadable; treating it as empty", "err", err)
		return
	}
	c.dismissed = s.Dismissed
	c.failed = state{Failed: s.Failed, FailedWhy: s.FailedWhy, RolledBack: s.RolledBack}
}

// writeState replaces the file whole, so a crash leaves the old one or the new one.
func writeState(path string, s state) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
