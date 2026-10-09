package chat

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

var (
	dockStart = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	dockNow   = dockStart.Add(10 * time.Minute)
)

func at(d time.Duration) *time.Time { t := dockStart.Add(d); return &t }

func lane(id int64, name string, closed *time.Time) api.LaneView {
	l := api.LaneView{Lane: store.Lane{ID: id, Name: name, State: store.LaneOpen, Created: dockStart.Add(-time.Hour)}, Repo: "app"}
	if closed != nil {
		l.State, l.Closed = store.LaneClosed, closed
	}
	return l
}

func withMR(l api.LaneView, mr int, state, pipeline string) api.LaneView {
	l.MR, l.MRState, l.PipelineStatus = mr, state, pipeline
	return l
}

func run(id, laneID int64, agent, state string, ended *time.Time) api.RunView {
	return api.RunView{Run: store.Run{ID: id, LaneID: laneID, Agent: agent, State: state, Started: dockStart.Add(-time.Minute * time.Duration(id)), Ended: ended}}
}

// dockModel is a chat that started at dockStart, looked at at dockNow, with a lane in
// every state the dock knows.
func dockModel() *Model {
	m, _, _ := newTestModel()
	m.seenUntil = dockStart
	m.clock = func() time.Time { return dockNow }
	m.lanes = []api.LaneView{
		withMR(lane(1, "feat/review", nil), 34, api.MRStateOpen, api.PipelinePassed),
		withMR(lane(2, "fix/pipeline", nil), 35, api.MRStateOpen, api.PipelineFailed),
		lane(3, "feat/working", nil),
		lane(4, "feat/failed", nil),
		lane(5, "feat/done", nil),
		lane(6, "feat/old", nil),
		withMR(lane(7, "feat/merged", at(2*time.Minute)), 36, api.MRStateMerged, api.PipelinePassed),
		withMR(lane(8, "feat/merged-before", at(-time.Minute)), 37, api.MRStateMerged, api.PipelinePassed),
		lane(9, "feat/idle", nil),
	}
	m.runs = []api.RunView{
		run(1, 1, "claude", store.RunSucceeded, at(-5*time.Minute)),
		run(2, 2, "claude", store.RunRunning, nil),
		run(3, 3, "copilot", store.RunRunning, nil),
		run(4, 4, "cursor", store.RunFailed, at(3*time.Minute)),
		run(5, 5, "claude", store.RunSucceeded, at(4*time.Minute)),
		run(6, 6, "claude", store.RunSucceeded, at(-2*time.Minute)),
		// An earlier failure in lane 5 is superseded by its later success.
		{Run: store.Run{ID: 0, LaneID: 5, Agent: "claude", State: store.RunFailed}},
	}
	m.decisions = []api.DecisionView{
		{Decision: store.Decision{ID: 11, Question: "newer?", Created: dockNow.Add(-time.Minute)}, Agent: "copilot", Lane: "feat/working", Repo: "app"},
		{Decision: store.Decision{ID: 10, Question: "OK to change\nthe public API?", Created: dockNow.Add(-3 * time.Minute)}, Agent: "claude", Lane: "feat/review", Repo: "app"},
	}
	m.requests = []api.RequestView{
		{Request: store.Request{ID: 5, Message: "who owns auth?", State: store.RequestNeedsRouting, Created: dockNow.Add(-2 * time.Minute)}, FromAgent: "cursor", FromLane: "feat/failed", Repo: "app"},
	}
	return m
}

func describe(items []dockItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s %s %s", groupNames[it.group], it.lane, it.detail))
	}
	return out
}

func TestDockOrdersByWhatNeedsThePerson(t *testing.T) {
	m := dockModel()
	got := describe(m.dockItems())
	want := []string{
		"needs you feat/review decision 10: OK to change the public API? · 3m",
		"needs you feat/failed request 5 needs routing: who owns auth? · 2m",
		"needs you feat/working decision 11: newer? · 1m",
		"broken fix/pipeline pipeline failed, fixing", // known longer: the lane is older
		"broken feat/failed run 4 failed",
		"to review feat/review waiting for a merge",
		"working feat/working run 3 · 13m",
		"done unseen feat/done run 5 done",
		"done unseen feat/merged merged",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dock:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := countsText(counts(m.dockItems())); n != "3 needs you · 2 broken · 1 to review · 1 working · 2 done unseen" {
		t.Errorf("counts = %q", n)
	}
	if countsText(counts(nil)) != "" {
		t.Error("counts of nothing are not empty")
	}
}

func TestDockForgetsWhatThePersonHasSeen(t *testing.T) {
	m := dockModel()
	typeLine(t, m, "/log 5")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	for _, d := range describe(m.dockItems()) {
		if strings.Contains(d, "run 5") {
			t.Errorf("opened run still unseen: %s", d)
		}
	}
	if counts(m.dockItems())[groupDone] != 1 {
		t.Fatalf("dock: %q", describe(m.dockItems()))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if n := counts(m.dockItems()); n[groupDone] != 0 || n[groupBroken] != 2 {
		t.Errorf("after Ctrl-G: %q", describe(m.dockItems()))
	}
	// Something finishing later is news again.
	later := dockNow.Add(time.Minute)
	m.clock = func() time.Time { return later }
	m.runs[1] = run(2, 2, "claude", store.RunSucceeded, &later)
	if n := counts(m.dockItems()); n[groupDone] != 0 {
		// Lane 2's pipeline still failed: broken outranks done.
		t.Errorf("a broken lane counted as done: %q", describe(m.dockItems()))
	}
	m.runs[2] = run(3, 3, "copilot", store.RunSucceeded, &later)
	m.clock = func() time.Time { return later.Add(time.Second) }
	if n := counts(m.dockItems()); n[groupDone] != 1 {
		t.Errorf("a run finished after Ctrl-G is not unseen: %q", describe(m.dockItems()))
	}
}

func TestMRBadges(t *testing.T) {
	defer usePalette(pal)
	usePalette(newPalette(false))
	for _, c := range []struct {
		l    api.LaneView
		want string
	}{
		{lane(1, "x", nil), ""},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, ""), "!34"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelinePassed), "!34 ✓"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelineFailed), "!34 ✗"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelineRunning), "!34 …"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelinePending), "!34 …"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelineCanceled), "!34 canceled"},
		{withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelineSkipped), "!34 skipped"},
		{withMR(lane(1, "x", nil), 34, api.MRStateUnknown, api.PipelineUnknown), "!34"},
		{withMR(lane(1, "x", nil), 34, api.MRStateMerged, api.PipelineFailed), "!34 merged"},
		{withMR(lane(1, "x", nil), 34, api.MRStateClosed, api.PipelinePassed), "!34 closed"},
	} {
		if got, _ := mrBadge(c.l); got != c.want {
			t.Errorf("%d %q %q: badge %q, want %q", c.l.MR, c.l.MRState, c.l.PipelineStatus, got, c.want)
		}
	}
	usePalette(newPalette(true))
	if _, st := mrBadge(withMR(lane(1, "x", nil), 34, api.MRStateOpen, api.PipelineFailed)); st.GetForeground() != pal.bad.GetForeground() {
		t.Error("a failed pipeline's badge is not drawn as failed")
	}
}

func TestDockRowsShowBadgesAndClashingLanes(t *testing.T) {
	defer usePalette(pal)
	usePalette(newPalette(false))
	m := dockModel()
	web := withMR(lane(20, "feat/review", nil), 40, api.MRStateOpen, api.PipelineRunning)
	web.Repo = "web"
	m.lanes = append(m.lanes, web)
	m.runs = append(m.runs, run(20, 20, "copilot", store.RunRunning, nil))
	for _, want := range []string{
		"? app:feat/review  claude  decision 10",
		"✗ fix/pipeline  claude  !35 ✗  pipeline failed, fixing",
		"◆ app:feat/review  claude  !34 ✓  waiting for a merge",
		"● web:feat/review  copilot  !40 …  run 20",
	} {
		if !strings.Contains(strings.Join(dockLines(m), "\n"), want) {
			t.Errorf("dock lacks %q:\n%s", want, strings.Join(dockLines(m), "\n"))
		}
	}
}

// dockLines is every line of the dock, uncapped, as text.
func dockLines(m *Model) []string {
	items := m.dockItems()
	rows := []string{ansi.Strip(m.countsRow(items))}
	for _, it := range items {
		rows = append(rows, ansi.Strip(m.itemRow(it)))
	}
	return rows
}

func TestDockHeightIsCapped(t *testing.T) {
	m := dockModel()
	items := m.dockItems() // 9 of them
	for _, c := range []struct {
		height   int
		rows     int
		lastLine string
	}{
		{40, dockMaxRows, "+3 more"}, // counts, 6 items, +3 more
		{12, 4, "+7 more"},           // a third of the terminal: counts, 2 items, +7 more
		{6, 2, "decision 10"},        // counts and the most urgent item
		{4, 1, "needs you"},          // the counts alone
		{1, 1, "needs you"},
	} {
		m.Update(tea.WindowSizeMsg{Width: 120, Height: c.height})
		rows := m.dockRows(items)
		if len(rows) != c.rows {
			t.Errorf("height %d: %d rows, want %d:\n%s", c.height, len(rows), c.rows, strings.Join(rows, "\n"))
			continue
		}
		if last := ansi.Strip(rows[len(rows)-1]); !strings.Contains(last, c.lastLine) {
			t.Errorf("height %d: last row %q, want %q", c.height, last, c.lastLine)
		}
		if c.rows > 1 && !strings.Contains(ansi.Strip(rows[1]), "decision 10") {
			t.Errorf("height %d: the most urgent item is not first: %q", c.height, ansi.Strip(rows[1]))
		}
	}
}

func TestEmptyDockTakesNoRoom(t *testing.T) {
	m, _, _ := newTestModel()
	before := strings.Count(m.View(), "\n")
	if rows := m.dockRows(m.dockItems()); rows != nil {
		t.Fatalf("empty dock drew %q", rows)
	}
	m.lanes = []api.LaneView{lane(1, "feat/x", nil)}
	m.runs = []api.RunView{run(1, 1, "claude", store.RunSucceeded, at(-time.Hour))}
	if strings.Count(m.View(), "\n") != before {
		t.Errorf("a lane with nothing to say took room:\n%s", m.View())
	}
}

func TestLiveRegionWithTheDockFitsSmallTerminals(t *testing.T) {
	m := dockModel()
	for _, size := range [][2]int{{40, 24}, {40, 10}, {40, 5}, {20, 4}, {80, 3}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View()
		lines := strings.Split(v, "\n")
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %q is %d wide", size[0], size[1], l, w)
			}
		}
		// The status, the counts and the input always show; past that the dock gives way.
		if size[1] >= 4 && len(lines) > size[1] {
			t.Errorf("%dx%d: live region is %d lines:\n%s", size[0], size[1], len(lines), v)
		}
		if !strings.Contains(v, "needs you") || !strings.Contains(v, "›") {
			t.Errorf("%dx%d lacks the counts or the input:\n%s", size[0], size[1], v)
		}
	}
}

func TestAges(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Second: "5s", 3 * time.Minute: "3m", 5 * time.Hour: "5h", 72 * time.Hour: "3d",
	} {
		if got := age(d); got != want {
			t.Errorf("age(%s) = %q, want %q", d, got, want)
		}
	}
}

// A run's outcome in the feed shows in the dock at once; the runs list still has the
// last word, and run_ended (an older daemon) leaves the run as the list said.
func TestFeedOutcomeShowsBeforeThePoll(t *testing.T) {
	for _, c := range []struct {
		kind, want string
	}{
		{kindRunPassed, "done unseen feat/working run 3 done"},
		{kindRunFailed, "broken feat/working run 3 failed"},
		{kindRunInterrupted, "broken feat/working run 3 interrupted"},
		{kindRunQuota, "working feat/working run 3 · 13m"},
		{kindCommit, "working feat/working run 3 · 13m"},
		{store.FeedRunEnded, "working feat/working run 3 · 13m"},
	} {
		m := dockModel()
		m.lastFeed = 0
		m.Update(feedMsg(api.Feed{Last: 1, Items: []store.FeedItem{{ID: 1, Kind: c.kind, Ref: 3, Created: dockNow, Text: "run 3"}}}))
		if !strings.Contains(strings.Join(describe(m.dockItems()), "\n"), c.want) {
			t.Errorf("%s: dock lacks %q:\n%s", c.kind, c.want, strings.Join(describe(m.dockItems()), "\n"))
		}
		// The next poll is the truth: here the person had stopped it.
		runs := append([]api.RunView(nil), dockModel().runs...)
		runs[2] = run(3, 3, "copilot", store.RunStopped, &dockNow)
		m.Update(panelMsg{panelUpdated: true, lanes: m.lanes, runs: runs, decisions: m.decisions, requests: m.requests})
		if !strings.Contains(strings.Join(describe(m.dockItems()), "\n"), "done unseen feat/working run 3 stopped") {
			t.Errorf("%s: the poll did not win:\n%s", c.kind, strings.Join(describe(m.dockItems()), "\n"))
		}
	}
}
