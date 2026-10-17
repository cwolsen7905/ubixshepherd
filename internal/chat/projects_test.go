package chat

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// projectModel has a shop lane at work and a waiting decision on a docs lane, and an
// idle lane in no project.
func projectModel() (*Model, *fakeAPI) {
	m, _, a := newTestModel()
	m.seenUntil = dockStart
	m.clock = func() time.Time { return dockNow }
	shop := lane(1, "feat-login", nil)
	shop.Repo = "app"
	docs := lane(2, "docs-pass", nil)
	docs.Repo = "docs"
	idle := lane(3, "scratch", nil)
	idle.Repo = "misc"
	m.lanes = []api.LaneView{shop, docs, idle}
	m.runs = []api.RunView{run(1, 1, "claude", store.RunRunning, nil)}
	m.decisions = []api.DecisionView{{Decision: store.Decision{ID: 9, Question: "Which one?", Created: dockStart}, Repo: "docs", Lane: "docs-pass", Agent: "claude"}}
	m.projects = []api.ProjectSummary{
		{Name: "shop", Repos: []string{"app"}, Lanes: 3, SpendToday: 1.2},
		{Name: "site", Repos: []string{"docs"}, Lanes: 1},
		{Unassigned: true, Repos: []string{"misc"}, Lanes: 1},
	}
	return m, a
}

func plainRows(rows []string) string {
	var out []string
	for _, r := range rows {
		out = append(out, ansi.Strip(r))
	}
	return strings.Join(out, "\n")
}

func TestDockPrefixesProjectsOnlyWhenSeveralHaveItems(t *testing.T) {
	m, _ := projectModel()
	got := plainRows(m.dockRows(m.dockItems()))
	if !strings.Contains(got, "shop/feat-login") || !strings.Contains(got, "site/docs-pass") {
		t.Errorf("want project prefixes:\n%s", got)
	}
	if strings.Contains(got, "scratch") {
		t.Errorf("an idle lane has no row:\n%s", got)
	}
	// One project with items: nothing new.
	m.decisions = nil
	got = plainRows(m.dockRows(m.dockItems()))
	if strings.Contains(got, "shop/") || !strings.Contains(got, "feat-login") {
		t.Errorf("one project should not be prefixed:\n%s", got)
	}
	// No projects known (an older daemon): nothing new either.
	m.projects = nil
	if got = plainRows(m.dockRows(m.dockItems())); strings.Contains(got, "shop") {
		t.Errorf("no projects, no prefix:\n%s", got)
	}
}

func TestDockCountsRowStaysGlobal(t *testing.T) {
	m, _ := projectModel()
	got := ansi.Strip(m.countsRow(m.dockItems()))
	if got != "1 needs you · 1 working" {
		t.Errorf("counts = %q", got)
	}
}

func TestExpandedDockGroupsOnDemand(t *testing.T) {
	m, _ := projectModel()
	flat := plainRows(m.renderDock())
	if strings.Contains(flat, "open ·") {
		t.Errorf("flat dock has headers:\n%s", flat)
	}
	m.dockGroup = true
	got := plainRows(m.renderDock())
	for _, want := range []string{"1 ▾ shop  3 open · 1 running · 0 waiting · $1.20", "2 ▾ site  1 open · 0 running · 1 waiting · $0.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "unassigned") {
		t.Errorf("Unassigned with only idle lanes should be hidden:\n%s", got)
	}
	// The compact dock does not change.
	if c := plainRows(m.dockRows(m.dockItems())); strings.Contains(c, "open ·") {
		t.Errorf("compact dock grouped:\n%s", c)
	}
	// Unassigned shows once it has something to say.
	m.lanes[2].MR, m.lanes[2].MRState, m.lanes[2].PipelineStatus = 4, api.MRStateOpen, api.PipelineFailed
	if got = plainRows(m.renderDock()); !strings.Contains(got, "unassigned  1 open") {
		t.Errorf("Unassigned with a broken lane:\n%s", got)
	}
}

func TestSpendIsOnlyInTheExpandedDock(t *testing.T) {
	m, _ := projectModel()
	m.dockGroup = true
	if c := plainRows(m.dockRows(m.dockItems())); strings.Contains(c, "$1.20") {
		t.Errorf("compact dock shows spend:\n%s", c)
	}
	if e := plainRows(m.renderDock()); !strings.Contains(e, "$1.20") {
		t.Errorf("expanded dock lacks spend:\n%s", e)
	}
}

func TestDockBudgetPressure(t *testing.T) {
	m, _ := projectModel()
	m.decisions, m.runs = nil, nil
	m.projects[0].Budget = &api.ProjectBudgetView{Amount: 10, Cap: "soft", Period: "day", Spent: 7.9}
	if got := plainRows(m.dockRows(m.dockItems())); strings.Contains(got, "budget") {
		t.Errorf("79%% should not warn:\n%s", got)
	}
	m.projects[0].Budget.Spent = 8
	if got := plainRows(m.dockRows(m.dockItems())); !strings.Contains(got, "budget 80%: $8.00 of $10.00 this day") || strings.Contains(got, "held") {
		t.Errorf("80%% warns:\n%s", got)
	}
	// A soft cap past its limit still only warns; a hard one holds runs.
	m.projects[0].Budget.Spent = 12
	if got := plainRows(m.dockRows(m.dockItems())); strings.Contains(got, "held") {
		t.Errorf("soft cap holds nothing:\n%s", got)
	}
	m.projects[0].Budget.Cap = "hard"
	rows := m.dockRows(m.dockItems())
	if got := plainRows(rows); !strings.Contains(got, "✗ shop  budget 120%") || !strings.Contains(got, "runs held") {
		t.Errorf("hard cap holds:\n%s", got)
	}
	if rows[1] == "" || !strings.Contains(rows[1], "budget") {
		t.Errorf("the budget row comes right after the counts: %q", rows[1])
	}
	// Even with quiet lanes the dock shows it.
	m.lanes = nil
	if len(m.dockRows(m.dockItems())) != 2 {
		t.Errorf("budget row alone: %q", m.dockRows(m.dockItems()))
	}
}

func TestDockViewKeysGroupAndCollapseAndRemember(t *testing.T) {
	m, a := projectModel()
	typeLine(t, m, "/dock")
	if m.pager == nil || !m.dockView {
		t.Fatal("/dock should open the expanded dock")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	drive(t, m, cmd)
	if !m.dockGroup || a.settings["dock.group"] != "project" {
		t.Fatalf("p groups and saves: %v %v", m.dockGroup, a.settings)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	drive(t, m, cmd)
	if !m.collapsed["shop"] || a.settings["dock.collapsed"] != `["shop"]` {
		t.Fatalf("1 collapses shop: %v %v", m.collapsed, a.settings)
	}
	if got := plainRows(m.renderDock()); strings.Contains(got, "feat-login") || !strings.Contains(got, "1 ▸ shop") {
		t.Errorf("collapsed shop:\n%s", got)
	}
	// A new chat reads the choices back.
	m2, _ := projectModel()
	m2.api = a
	_, cmd = m2.Update(m2.loadDockPrefs()())
	drive(t, m2, cmd)
	if !m2.dockGroup || !m2.collapsed["shop"] {
		t.Errorf("prefs not restored: %v %v", m2.dockGroup, m2.collapsed)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	drive(t, m, cmd)
	if m.pager != nil || m.dockView {
		t.Error("q closes the dock view")
	}
}

func TestGroupCommand(t *testing.T) {
	m, a := projectModel()
	typeLine(t, m, "/group")
	if !m.dockGroup || m.pager == nil || a.settings["dock.group"] != "project" {
		t.Fatalf("/group groups and shows it: %v", a.settings)
	}
	m.closePager()
	typeLine(t, m, "/group collapse unassigned")
	if !m.collapsed[""] || a.settings["dock.collapsed"] != `[""]` {
		t.Errorf("collapsed = %v %v", m.collapsed, a.settings)
	}
	m.closePager()
	typeLine(t, m, "/group off")
	if m.dockGroup || a.settings["dock.group"] != "" {
		t.Errorf("/group off: %v", a.settings)
	}
	m.closePager()
	typeLine(t, m, "/group nonsense now")
	if !has(m.Lines(), KindError, "usage: /group") {
		t.Error("want a usage line")
	}
}
