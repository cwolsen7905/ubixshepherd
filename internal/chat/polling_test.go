package chat

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestAdaptivePollingIntervals(t *testing.T) {
	m, _, _ := newTestModel()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	m.lastActivity = now.Add(-31 * time.Second)
	if got := m.feedInterval(now); got != 2*time.Second {
		t.Fatalf("recent idle feed interval = %s, want 2s", got)
	}
	m.lastActivity = now.Add(-90 * time.Second)
	if got := m.feedInterval(now); got != maxIdleFeedInterval {
		t.Fatalf("idle feed interval = %s, want %s", got, maxIdleFeedInterval)
	}
	if got := m.panelInterval(now); got != idlePanelInterval {
		t.Fatalf("idle panel interval = %s, want %s", got, idlePanelInterval)
	}

	m.runs = []api.RunView{{Run: store.Run{State: store.RunRunning}}}
	if got := m.feedInterval(now); got != activeFeedInterval {
		t.Fatalf("active feed interval = %s, want %s", got, activeFeedInterval)
	}
	if got := m.panelInterval(now); got != activePanelInterval {
		t.Fatalf("active panel interval = %s, want %s", got, activePanelInterval)
	}
}

func TestFeedItemAndKeypressResetFeedPollingActivity(t *testing.T) {
	m, _, _ := newTestModel()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	m.lastActivity = now.Add(-2 * time.Minute)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !m.lastActivity.Equal(now) || m.feedInterval(now) != activeFeedInterval {
		t.Fatal("keypress did not restore active feed polling")
	}

	m.lastActivity = now.Add(-2 * time.Minute)
	m.lastFeed = 0
	m.Update(feedMsg(api.Feed{
		Last:  1,
		Items: []store.FeedItem{{ID: 1, Kind: store.FeedRunStarted, Text: "run started"}},
	}))
	if !m.lastActivity.Equal(now) || m.feedInterval(now) != activeFeedInterval {
		t.Fatal("new feed item did not restore active feed polling")
	}
}

func TestSessionsPollAtMostOncePerMinute(t *testing.T) {
	m, _, a := newTestModel()
	base := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	now := base
	m.clock = func() time.Time { return now }
	m.lastSessionsPoll = base
	m.lastPanelPoll = base

	m.Update(m.pollPanelAt(false, base.Add(30*time.Second))())
	if a.sessCalls != 0 {
		t.Fatalf("panel refresh unexpectedly polled sessions %d times", a.sessCalls)
	}
	if m.sessionsDue(base.Add(59 * time.Second)) {
		t.Fatal("sessions became due before one minute")
	}
	if !m.sessionsDue(base.Add(time.Minute)) {
		t.Fatal("sessions were not due after one minute")
	}
	m.Update(m.pollSessionsAt(base.Add(time.Minute))())
	if a.sessCalls != 1 {
		t.Fatalf("sessions calls = %d, want 1", a.sessCalls)
	}
}

func TestBlurUsesIdleRatesAndFocusRefreshesImmediately(t *testing.T) {
	m, _, a := newTestModel()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	m.lastSessionsPoll = now

	_, blurCmd := m.Update(tea.BlurMsg{})
	if m.focused || m.feedInterval(now) != maxIdleFeedInterval || m.panelInterval(now) != idlePanelInterval {
		t.Fatal("blur did not drop polling to idle rates")
	}
	if blurCmd == nil {
		t.Fatal("blur did not reschedule polling")
	}

	_, focusCmd := m.Update(tea.FocusMsg{})
	if !m.focused || !m.lastActivity.Equal(now) {
		t.Fatal("focus did not restore active state")
	}
	if !m.lastPanelPoll.Equal(now) {
		t.Fatal("focus did not schedule an immediate panel refresh")
	}
	msg := focusCmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 3 {
		t.Fatalf("focus commands = %#v, want feed, panel and timer", msg)
	}
	batch[0]()
	m.Update(batch[1]())
	if a.feedCalls != 1 || a.laneCalls != 1 || a.runCalls != 1 || a.spendCalls != 1 {
		t.Fatalf("focus did not refresh immediately: feed=%d lanes=%d runs=%d spend=%d", a.feedCalls, a.laneCalls, a.runCalls, a.spendCalls)
	}
	if a.sessCalls != 0 {
		t.Fatalf("focus refreshed sessions before due: calls=%d", a.sessCalls)
	}
}
