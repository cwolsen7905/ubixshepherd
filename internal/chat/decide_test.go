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

// decideModel has three open decisions, oldest first: one with a recommendation and a
// destructive option, one without a recommendation, and one with no options.
func decideModel() (*Model, *fakeDesk, *fakeAPI) {
	m, d, a := newTestModel()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	a.ds = []api.DecisionView{
		{Decision: store.Decision{ID: 9, Question: "OK to change the public API?", Options: []string{"delete the old function", "keep both", "deprecate it"}, Recommendation: "keep both", Created: now.Add(-3 * time.Minute)}, Agent: "copilot", Lane: "test/parser"},
		{Decision: store.Decision{ID: 10, Question: "Which port?", Options: []string{"8080", "9090"}, Created: now.Add(-2 * time.Minute)}, Agent: "claude", Lane: "feat/web"},
		{Decision: store.Decision{ID: 11, Question: "Name the release?", Created: now.Add(-time.Minute)}, Agent: "cursor", Lane: "feat/rel"},
	}
	m.decisions = append([]api.DecisionView(nil), a.ds...)
	return m, d, a
}

func press(t *testing.T, m *Model, k tea.KeyMsg) {
	t.Helper()
	_, cmd := m.Update(k)
	drive(t, m, cmd)
}

var (
	keyTab      = tea.KeyMsg{Type: tea.KeyTab}
	keyShiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}
	keyEsc      = tea.KeyMsg{Type: tea.KeyEsc}
	keyEnter    = tea.KeyMsg{Type: tea.KeyEnter}
)

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestTabMovesBetweenTheInputAndTheDecisions(t *testing.T) {
	m, _, _ := decideModel()
	var got []int64
	for range 4 {
		press(t, m, keyTab)
		got = append(got, m.focusDecision)
	}
	if want := []int64{9, 10, 11, 0}; !equalIDs(got, want) {
		t.Errorf("Tab: %v, want %v", got, want)
	}
	got = nil
	for range 2 {
		press(t, m, keyShiftTab)
		got = append(got, m.focusDecision)
	}
	if want := []int64{11, 10}; !equalIDs(got, want) {
		t.Errorf("Shift-Tab: %v, want %v", got, want)
	}
	if !strings.Contains(ansi.Strip(m.View()), "› ? feat/web") || strings.Contains(m.View(), "Talk to Shepherd") {
		t.Errorf("focused view:\n%s", ansi.Strip(m.View()))
	}
	press(t, m, keyEsc)
	if m.focusDecision != 0 || !m.input.Focused() || !strings.Contains(m.View(), "Talk to Shepherd") {
		t.Errorf("Esc: focus %d", m.focusDecision)
	}
	// Typing while a decision has the focus does not reach the input.
	press(t, m, keyTab)
	press(t, m, key("x"))
	if m.input.Value() != "" {
		t.Errorf("input got %q", m.input.Value())
	}
}

func TestTabWithoutDecisionsStaysInTheInput(t *testing.T) {
	m, _, _ := newTestModel()
	press(t, m, keyTab)
	if m.focusDecision != 0 || !m.input.Focused() {
		t.Error("Tab left the input with nothing to answer")
	}
}

func TestANumberAnswersARecommendedDecision(t *testing.T) {
	m, d, a := decideModel()
	press(t, m, keyTab) // decision 9
	view := ansi.Strip(m.View())
	for _, want := range []string{"? decision 9 · copilot · test/parser", "1. delete the old function", "3. deprecate it", "recommends: keep both", "1-3 answer"} {
		if !strings.Contains(view, want) {
			t.Errorf("focused decision lacks %q:\n%s", want, view)
		}
	}
	press(t, m, key("7")) // no such option
	if len(a.answered) != 0 || m.confirm != 0 {
		t.Fatal("an out-of-range number did something")
	}
	press(t, m, key("2"))
	if a.answered[9] != "option 2: keep both" {
		t.Errorf("answered %v", a.answered)
	}
	if len(d.got) != 0 {
		t.Error("the answer went through the desk")
	}
	if !has(m.Lines(), KindYou, "answer to decision 9: 2. keep both") || !has(m.Lines(), KindInfo, "Answered decision 9") {
		t.Errorf("thread = %+v", m.Lines())
	}
	if m.focusDecision != 0 {
		t.Error("focus stayed on an answered decision")
	}
	for _, it := range m.dockItems() {
		if it.decision == 9 {
			t.Error("the answered decision is still in the dock")
		}
	}
}

func TestDestructiveOrUnrecommendedOptionsWaitForEnter(t *testing.T) {
	m, _, a := decideModel()
	press(t, m, keyTab)
	press(t, m, key("1")) // "delete the old function"
	if len(a.answered) != 0 || m.confirm != 1 {
		t.Fatalf("a destructive option answered at once: %v", a.answered)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Answer 1. delete the old function? Enter to confirm") {
		t.Errorf("no confirmation:\n%s", ansi.Strip(m.View()))
	}
	press(t, m, keyEsc)
	if len(a.answered) != 0 || m.focusDecision != 0 || m.confirm != 0 {
		t.Fatal("Esc did not cancel")
	}
	press(t, m, keyTab)
	press(t, m, key("1"))
	press(t, m, keyEnter)
	if a.answered[9] != "option 1: delete the old function" {
		t.Errorf("confirmed answer %v", a.answered)
	}

	// No recommendation: every option waits for Enter.
	press(t, m, keyTab) // decision 10, now the oldest
	if m.focusDecision != 10 {
		t.Fatalf("focus %d", m.focusDecision)
	}
	press(t, m, key("2"))
	if _, ok := a.answered[10]; ok {
		t.Fatal("an unrecommended option answered at once")
	}
	press(t, m, key("1")) // changing the pick asks again
	if m.confirm != 1 || len(a.answered) != 1 {
		t.Fatalf("confirm %d, answered %v", m.confirm, a.answered)
	}
	press(t, m, keyEnter)
	if a.answered[10] != "option 1: 8080" {
		t.Errorf("answered %v", a.answered)
	}
}

func TestEnterAnswersInThePersonsOwnWords(t *testing.T) {
	m, _, a := decideModel()
	press(t, m, keyTab)
	press(t, m, keyTab)
	press(t, m, keyTab) // decision 11, no options
	if !strings.Contains(ansi.Strip(m.View()), "Enter answer in your words") {
		t.Errorf("view:\n%s", ansi.Strip(m.View()))
	}
	press(t, m, key("1"))
	press(t, m, keyEnter)
	if len(a.answered) != 0 || m.focusDecision != 0 || m.input.Value() != "/answer 11 " {
		t.Fatalf("answered %v, focus %d, input %q", a.answered, m.focusDecision, m.input.Value())
	}
	m.input.InsertString("v1.0")
	press(t, m, keyEnter)
	if a.answered[11] != "v1.0" {
		t.Errorf("answered %v", a.answered)
	}
}

func TestFocusReturnsWhenTheDecisionIsAnsweredElsewhere(t *testing.T) {
	m, _, a := decideModel()
	press(t, m, keyTab)
	m.Update(panelMsg{panelUpdated: true, decisions: a.ds[1:]})
	if m.focusDecision != 0 || !m.input.Focused() {
		t.Error("focus stayed on a decision that is gone")
	}
}

func TestFocusedDecisionFitsNarrowAndShortTerminals(t *testing.T) {
	m, _, _ := decideModel()
	press(t, m, keyTab)
	for _, size := range [][2]int{{40, 24}, {40, 9}, {40, 8}, {30, 6}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View()
		for _, l := range strings.Split(v, "\n") {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %q is %d wide", size[0], size[1], l, w)
			}
		}
		if !strings.Contains(v, "decision 9") || !strings.Contains(ansi.Strip(v), "Esc back") || !strings.Contains(ansi.Strip(v), "3 needs you") {
			t.Errorf("%dx%d:\n%s", size[0], size[1], ansi.Strip(v))
		}
		// Once the dock gives way, every option shows when the terminal can hold them.
		if size[1] >= 9 && !strings.Contains(ansi.Strip(v), "3. deprecate it") {
			t.Errorf("%dx%d:\n%s", size[0], size[1], ansi.Strip(v))
		}
	}
}

func TestNeedsConfirm(t *testing.T) {
	d := api.DecisionView{Decision: store.Decision{Options: []string{"force-push the branch", "Rebase it", "Reset to main", "keep it"}, Recommendation: "Rebase it"}}
	for n, want := range map[int]bool{1: true, 2: false, 3: true, 4: false} {
		if got := needsConfirm(d, n); got != want {
			t.Errorf("option %d: %v, want %v", n, got, want)
		}
	}
	d.Recommendation = ""
	if !needsConfirm(d, 4) {
		t.Error("no recommendation did not ask")
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
