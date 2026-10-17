package chat

import (
	"encoding/json"
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The dock knows projects through GET /v1/projects: which repos a project holds, how many
// lanes are open and what it has spent. Running and waiting are counted from the dock's
// own rows, so a summary always agrees with what is listed.

const (
	// settingDockGroup is "project" when the expanded dock groups by project.
	settingDockGroup = "dock.group"
	// settingDockCollapsed is the collapsed projects as a JSON list of names; "" is Unassigned.
	settingDockCollapsed = "dock.collapsed"

	// budgetWarnAt is the share of a project's cap at which the dock warns.
	budgetWarnAt = 0.8
)

// dockPrefsMsg is the dock's saved choices, as read at start.
type dockPrefsMsg struct {
	group     bool
	collapsed []string
}

// loadDockPrefs reads the dock's choices. A daemon too old to keep them leaves the
// defaults: flat, nothing collapsed.
func (m *Model) loadDockPrefs() tea.Cmd {
	return func() tea.Msg {
		g, err := m.api.Setting(m.ctx, settingDockGroup)
		if err != nil {
			return nil
		}
		var names []string
		if c, err := m.api.Setting(m.ctx, settingDockCollapsed); err == nil && c != "" {
			json.Unmarshal([]byte(c), &names)
		}
		return dockPrefsMsg{group: g == "project", collapsed: names}
	}
}

// saveDockPrefs stores the dock's choices in the daemon's settings.
func (m *Model) saveDockPrefs() tea.Cmd {
	group := ""
	if m.dockGroup {
		group = "project"
	}
	names := []string{}
	for n, on := range m.collapsed {
		if on {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	raw, _ := json.Marshal(names)
	return func() tea.Msg {
		if err := m.api.SetSetting(m.ctx, settingDockGroup, group); err != nil {
			return errorMsg{err}
		}
		if err := m.api.SetSetting(m.ctx, settingDockCollapsed, string(raw)); err != nil {
			return errorMsg{err}
		}
		return nil
	}
}

// projectOf is the project a repo is in, "" for Unassigned or when projects are unknown.
func (m *Model) projectOf(repo string) string {
	for _, p := range m.projects {
		if p.Unassigned {
			continue
		}
		for _, r := range p.Repos {
			if r == repo {
				return p.Name
			}
		}
	}
	return ""
}

// qualifyProjects says whether item rows carry their project: only when more than one
// project (Unassigned counts as one) has items.
func qualifyProjects(items []dockItem) bool {
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.project] = true
	}
	return len(seen) > 1
}

// budgetRows are the projects near or at their cap, one row each while it lasts: broken
// for a hard cap that is reached (it holds runs), warn from 80% of any cap.
func (m *Model) budgetRows() []string {
	var rows []string
	for _, p := range m.projects {
		b := p.Budget
		if p.Unassigned || b == nil || b.Amount <= 0 {
			continue
		}
		share := b.Spent / b.Amount
		text := fmt.Sprintf("%s  budget %.0f%%: $%.2f of $%.2f this %s", p.Name, share*100, b.Spent, b.Amount, b.Period)
		switch {
		case b.Cap == "hard" && share >= 1:
			rows = append(rows, "  "+pal.broken.Render("✗ "+text+", runs held"))
		case share >= budgetWarnAt:
			rows = append(rows, "  "+pal.warn.Render("$ "+text))
		}
	}
	return rows
}

// groupSummary is a project's header line: "shop  3 open · 1 running · 1 waiting · $1.20".
func groupSummary(name string, p api.ProjectSummary, items []dockItem) string {
	running, waiting := 0, 0
	for _, it := range items {
		switch it.group {
		case groupWorking:
			running++
		case groupNeedsYou:
			waiting++
		}
	}
	return fmt.Sprintf("%s  %d open · %d running · %d waiting · $%.2f", name, p.Lanes, running, waiting, p.SpendToday)
}

// renderDock is the expanded dock: the counts, budget pressure, then every item, flat
// by urgency or under a header per project. It also notes the headers' order for the
// number keys that collapse them.
func (m *Model) renderDock() []string {
	items := m.dockItems()
	m.dockHeaders = nil
	rows := []string{m.countsRow(items)}
	if len(items) == 0 && m.approvals == 0 && m.waiting.Count == 0 {
		rows = []string{styleInfo.Render("Nothing needs you.")}
	}
	rows = append(rows, m.budgetRows()...)
	qualify := qualifyProjects(items)
	if !m.dockGroup || len(m.projects) == 0 {
		if m.dockGroup {
			rows = append(rows, styleInfo.Render("  Projects are not available from this daemon."))
		}
		for _, it := range items {
			rows = append(rows, m.itemRow(it, qualify))
		}
		return rows
	}
	for _, p := range m.projectGroups(items) {
		name := p.Name
		if p.Unassigned {
			name = ""
		}
		var mine []dockItem
		for _, it := range items {
			if it.project == name {
				mine = append(mine, it)
			}
		}
		// Unassigned with only idle lanes has nothing to show; neither has an empty project.
		if len(mine) == 0 && (p.Unassigned || p.Lanes == 0) {
			continue
		}
		label := name
		if p.Unassigned {
			label = "unassigned"
		}
		m.dockHeaders = append(m.dockHeaders, name)
		mark := "▾"
		if m.collapsed[name] {
			mark = "▸"
		}
		key := " "
		if n := len(m.dockHeaders); n <= 9 {
			key = fmt.Sprint(n)
		}
		rows = append(rows, styleHead.Render(fmt.Sprintf("%s %s %s", key, mark, groupSummary(label, p, mine))))
		if m.collapsed[name] {
			continue
		}
		for _, it := range mine {
			rows = append(rows, m.itemRow(it, false))
		}
	}
	return rows
}

// projectGroups are the daemon's projects, plus an Unassigned group when it did not list
// one but some item belongs to no listed project (the lanes and projects reads are
// separate calls), so the grouped view never shows fewer items than the flat one.
func (m *Model) projectGroups(items []dockItem) []api.ProjectSummary {
	groups := m.projects
	has := false
	for _, p := range groups {
		has = has || p.Unassigned
	}
	if has {
		return groups
	}
	for _, it := range items {
		if it.project != "" {
			continue
		}
		open := 0
		for _, l := range m.lanes {
			if l.State == store.LaneOpen && m.projectOf(l.Repo) == "" {
				open++
			}
		}
		return append(append([]api.ProjectSummary(nil), groups...), api.ProjectSummary{Unassigned: true, Lanes: open})
	}
	return groups
}

const dockHint = "p group by project · 1-9 collapse a project · ↑↓ PgUp/PgDn scroll · q back"

// openDock shows the expanded dock in the pager.
func (m *Model) openDock() tea.Cmd {
	m.dockView = true
	return m.openPager("dock")
}

// dockKey handles the dock view's own keys; it says whether the key was one of them.
func (m *Model) dockKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	s := msg.String()
	switch {
	case s == "p":
		m.dockGroup = !m.dockGroup
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9' && m.dockGroup:
		i := int(s[0] - '1')
		if i >= len(m.dockHeaders) {
			return nil, true
		}
		m.toggleCollapsed(m.dockHeaders[i])
	default:
		return nil, false
	}
	m.refreshPager()
	return m.saveDockPrefs(), true
}

func (m *Model) toggleCollapsed(name string) {
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	m.collapsed[name] = !m.collapsed[name]
}

// groupCommand handles /group: show the expanded dock grouped, or set how it is shown.
func (m *Model) groupCommand(f []string) tea.Cmd {
	switch {
	case len(f) == 1:
		m.dockGroup = !m.dockGroup
	case len(f) == 2 && f[1] == "on", len(f) == 2 && f[1] == "off":
		m.dockGroup = f[1] == "on"
	case len(f) == 3 && (f[1] == "collapse" || f[1] == "expand"):
		name := f[2]
		if name == "unassigned" {
			name = ""
		}
		if m.collapsed == nil {
			m.collapsed = map[string]bool{}
		}
		m.collapsed[name] = f[1] == "collapse"
	default:
		m.add(Line{Kind: KindError, Text: "usage: /group [on|off | collapse|expand <project>]"})
		return nil
	}
	cmds := []tea.Cmd{m.saveDockPrefs()}
	if m.pager == nil {
		cmds = append(cmds, m.openDock())
	} else {
		m.refreshPager()
	}
	return tea.Batch(cmds...)
}
