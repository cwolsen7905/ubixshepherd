package chat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The dock is the part of the live region that triages: what needs the person first,
// then what is broken, waiting for review, at work, and done since they last looked.
// It is built from the daemon's lists on every draw, never from the feed: a feed item
// does not say whether a run succeeded.

// group is an attention state; lower is more urgent.
type group int

const (
	groupNeedsYou group = iota
	groupBroken
	groupReview
	groupWorking
	groupDone
	groupCount
)

var groupNames = [groupCount]string{"needs you", "broken", "to review", "working", "done unseen"}

// groupMarks are the dock's glyphs, one per group, read without colour.
var groupMarks = [groupCount]eventMark{
	{"?", toneWarn},
	{"✗", toneBroken},
	{"◆", toneAccent},
	{"●", toneAccent},
	{"✓", func(p palette) lipgloss.Style { return p.ok }},
}

// dockMaxRows caps the dock, counts included, however tall the terminal.
const dockMaxRows = 8

// dockItem is one line of the dock.
type dockItem struct {
	group    group
	glyph    string // overrides the group's, as for a request
	lane     string // as shown: repo:lane when lane names clash
	agent    string
	mr       api.LaneView // for the badge; MR 0 has none
	detail   string
	at       time.Time // orders items in a group
	decision int64     // the decision, for one that needs you
}

// dockItems is everything the dock could show, most urgent first.
func (m *Model) dockItems() []dockItem {
	now := m.clock()
	clash := laneClashes(m.lanes)
	name := func(repo, lane string) string {
		if clash[lane] && repo != "" {
			return repo + ":" + lane
		}
		return lane
	}
	var items []dockItem
	for _, d := range m.decisions {
		items = append(items, dockItem{
			group: groupNeedsYou, lane: name(d.Repo, d.Lane), agent: d.Agent, at: d.Created, decision: d.ID,
			detail: fmt.Sprintf("decision %d: %s%s", d.ID, oneLine(d.Question), waited(now, d.Created)),
		})
	}
	for _, q := range m.requests {
		items = append(items, dockItem{
			group: groupNeedsYou, glyph: eventMarks[api.EventRequestAttention].glyph, lane: name(q.Repo, q.FromLane), agent: q.FromAgent, at: q.Created,
			detail: fmt.Sprintf("request %d needs routing: %s%s", q.ID, oneLine(q.Message), waited(now, q.Created)),
		})
	}

	// The latest run of each lane says what the lane is doing.
	latest := map[int64]api.RunView{}
	for _, r := range m.runs {
		if l, ok := latest[r.LaneID]; !ok || r.ID > l.ID {
			latest[r.LaneID] = r
		}
	}
	for _, l := range m.lanes {
		it := dockItem{lane: name(l.Repo, l.Name), mr: l}
		run, hasRun := latest[l.ID]
		if hasRun {
			it.agent = run.Agent
		}
		running := hasRun && run.State == store.RunRunning
		mrOpen := l.MR != 0 && l.MRState != api.MRStateMerged && l.MRState != api.MRStateClosed
		failedRun := hasRun && (run.State == store.RunFailed || run.State == store.RunInterrupted)
		failedPipeline := mrOpen && l.PipelineStatus == api.PipelineFailed
		switch {
		case l.State == store.LaneClosed:
			if l.MRState != api.MRStateMerged || l.Closed == nil || !m.unseen(fmt.Sprintf("lane:%d", l.ID), *l.Closed) {
				continue
			}
			it.group, it.at, it.detail = groupDone, *l.Closed, "merged"
		case failedRun || failedPipeline:
			it.group = groupBroken
			if failedRun {
				it.at = endedAt(run)
				it.detail = fmt.Sprintf("run %d %s", run.ID, run.State)
			} else {
				it.at = l.Created
				it.detail = "pipeline failed"
			}
			if running {
				it.detail += ", fixing"
			}
		case running:
			it.group, it.at = groupWorking, run.Started
			it.detail = fmt.Sprintf("run %d · %s", run.ID, age(now.Sub(run.Started)))
		case mrOpen && l.PipelineStatus == api.PipelinePassed:
			it.group, it.at, it.detail = groupReview, l.Created, "waiting for a merge"
		case hasRun && run.Ended != nil && m.unseen(fmt.Sprintf("run:%d", run.ID), *run.Ended):
			it.group, it.at = groupDone, *run.Ended
			it.detail = fmt.Sprintf("run %d %s", run.ID, short(run.State))
		default:
			continue
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.group != b.group {
			return a.group < b.group
		}
		if a.group == groupDone {
			return a.at.After(b.at) // the latest first
		}
		return a.at.Before(b.at) // waiting longest first
	})
	return items
}

// outcomeStates is the run state a feed outcome reports. An interrupted run may also
// have been stopped by the person; the runs list says which at the next poll.
var outcomeStates = map[string]string{
	kindRunPassed:      store.RunSucceeded,
	kindRunFailed:      store.RunFailed,
	kindRunInterrupted: store.RunInterrupted,
}

// noteOutcome lets the dock show a run's end as soon as the feed reports it, instead of
// at the next poll. The runs list stays the source of truth: the next poll replaces this,
// and an older daemon, which sends run_ended without an outcome, changes nothing here.
func (m *Model) noteOutcome(it store.FeedItem, event string) {
	state, ok := outcomeStates[event]
	if !ok || it.Ref == 0 {
		return
	}
	for i, r := range m.runs {
		if r.ID == it.Ref && r.State == store.RunRunning {
			ended := it.Created
			m.runs[i].State, m.runs[i].Ended = state, &ended
		}
	}
}

// unseen says whether something that finished at t is news to the person: it finished
// since the chat started or since they last cleared the dock, and they have not opened it.
func (m *Model) unseen(key string, t time.Time) bool {
	return t.After(m.seenUntil) && !m.seen[key]
}

// markSeen notes that the person has looked at something, such as a run's log.
func (m *Model) markSeen(key string) {
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	m.seen[key] = true
}

// clearDone marks everything finished so far as seen.
func (m *Model) clearDone() {
	m.seenUntil = m.clock()
	m.seen = nil
}

// laneClashes is the lane names that more than one repo uses.
func laneClashes(lanes []api.LaneView) map[string]bool {
	repos := map[string]string{}
	clash := map[string]bool{}
	for _, l := range lanes {
		if r, ok := repos[l.Name]; ok && r != l.Repo {
			clash[l.Name] = true
		}
		repos[l.Name] = l.Repo
	}
	return clash
}

// counts is how many items each group holds.
func counts(items []dockItem) [groupCount]int {
	var n [groupCount]int
	for _, it := range items {
		n[it.group]++
	}
	return n
}

// countsText is the groups that hold anything, most urgent first: "1 needs you · 2 broken".
func countsText(n [groupCount]int) string {
	var parts []string
	for g, c := range n {
		if c > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c, groupNames[g]))
		}
	}
	return strings.Join(parts, " · ")
}

// mrBadge is a lane's merge request as a badge, "!34 ✓", and the tone to draw it in;
// "" for a lane with no merge request.
func mrBadge(l api.LaneView) (string, lipgloss.Style) {
	if l.MR == 0 {
		return "", pal.muted
	}
	n := fmt.Sprintf("!%d", l.MR)
	switch l.MRState {
	case api.MRStateMerged:
		return n + " merged", pal.ok
	case api.MRStateClosed:
		return n + " closed", pal.muted
	}
	switch l.PipelineStatus {
	case api.PipelinePassed:
		return n + " ✓", pal.ok
	case api.PipelineFailed:
		return n + " ✗", pal.bad
	case api.PipelinePending, api.PipelineRunning:
		return n + " …", pal.muted
	case api.PipelineCanceled, api.PipelineSkipped:
		return n + " " + l.PipelineStatus, pal.muted
	}
	return n, pal.muted
}

// dockBudget is the rows the dock may take, counts included: the smaller of
// dockMaxRows and a third of the terminal, and at least the counts.
func (m *Model) dockBudget() int {
	return max(1, min(dockMaxRows, m.height/3))
}

// dockRows draws the dock within its budget: the counts, then the items, most urgent
// first, with "+N more" for what does not fit. Nothing at all when nothing is there.
func (m *Model) dockRows(items []dockItem) []string {
	if len(items) == 0 {
		return nil
	}
	rows := []string{m.countsRow(items)}
	room := m.dockBudget() - 1
	show := len(items)
	more := 0
	if show > room {
		// Keep a row for "+N more" when there is room for an item as well.
		show = max(room-1, 0)
		if room == 1 {
			show = 1
		}
		more = len(items) - show
	}
	for _, it := range items[:show] {
		rows = append(rows, m.itemRow(it))
	}
	if more > 0 && room > 1 {
		rows = append(rows, styleInfo.Render(fmt.Sprintf("  +%d more", more)))
	}
	return rows
}

func (m *Model) countsRow(items []dockItem) string {
	n := counts(items)
	var parts []string
	for g, c := range n {
		if c == 0 {
			continue
		}
		st := pal.muted
		switch group(g) {
		case groupNeedsYou:
			st = pal.warn
		case groupBroken:
			st = pal.broken
		}
		parts = append(parts, st.Render(fmt.Sprintf("%d %s", c, groupNames[g])))
	}
	return strings.Join(parts, pal.muted.Render(" · "))
}

func (m *Model) itemRow(it dockItem) string {
	mk := groupMarks[it.group]
	glyph := mk.glyph
	if it.glyph != "" {
		glyph = it.glyph
	}
	var parts []string
	if it.lane != "" {
		parts = append(parts, styleLane(it.lane).Render(it.lane))
	}
	if it.agent != "" {
		parts = append(parts, styleAgent(it.agent).Render(it.agent))
	}
	if b, st := mrBadge(it.mr); b != "" {
		parts = append(parts, st.Render(b))
	}
	if it.detail != "" {
		parts = append(parts, pal.muted.Render(it.detail))
	}
	lead := "  "
	if it.decision != 0 && it.decision == m.focusDecision {
		lead = styleSel.Render("›") + " "
	}
	return lead + mk.tone(pal).Render(glyph) + " " + strings.Join(parts, "  ")
}

func endedAt(r api.RunView) time.Time {
	if r.Ended != nil {
		return *r.Ended
	}
	return r.Started
}

// waited is how long something has waited, as a suffix: " · 3m".
func waited(now, since time.Time) string {
	if since.IsZero() || now.Before(since) {
		return ""
	}
	return " · " + age(now.Sub(since))
}

// age is a short duration: 40s, 3m, 2h, 4d.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
