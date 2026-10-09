package chat

import (
	"fmt"
	"regexp"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Decisions are answered in place: Tab moves focus from the input to the decisions in
// the dock (Shift-Tab back), a number key picks an option, and Esc returns to the input.
// An answer is always the person's own key press: nothing here answers on a timer, by
// default, or for the desk.

// reDestructive is options that sound like they lose something; picking one asks for
// Enter first.
var reDestructive = regexp.MustCompile(`(?i)\b(delete|remove|drop|destroy|discard|wipe|purge|erase|overwrite|force|reset|revert|roll ?back|truncate|kill|abandon|rm)\b`)

// needsConfirm says whether option n of d waits for Enter: when the agent recommended
// nothing, or the option looks destructive.
func needsConfirm(d api.DecisionView, n int) bool {
	return d.Recommendation == "" || reDestructive.MatchString(d.Options[n-1])
}

// dockDecisions is the open decisions in the order the dock shows them.
func (m *Model) dockDecisions(items []dockItem) []api.DecisionView {
	byID := map[int64]api.DecisionView{}
	for _, d := range m.decisions {
		byID[d.ID] = d
	}
	var out []api.DecisionView
	for _, it := range items {
		if d, ok := byID[it.decision]; ok && it.decision != 0 {
			out = append(out, d)
		}
	}
	return out
}

// focusedDecision is the decision in focus, if it is still open.
func (m *Model) focusedDecision() (api.DecisionView, bool) {
	for _, d := range m.decisions {
		if m.focusDecision != 0 && d.ID == m.focusDecision {
			return d, true
		}
	}
	return api.DecisionView{}, false
}

// moveFocus steps through input, then each decision, then back to the input; step is
// 1 for Tab and -1 for Shift-Tab. It reports whether there was anywhere to go.
func (m *Model) moveFocus(step int) bool {
	ds := m.dockDecisions(m.dockItems())
	if len(ds) == 0 {
		m.focusInput()
		return false
	}
	// Positions: 0 is the input, i+1 is decision i.
	pos := 0
	for i, d := range ds {
		if d.ID == m.focusDecision {
			pos = i + 1
		}
	}
	pos = (pos + step + len(ds) + 1) % (len(ds) + 1)
	if pos == 0 {
		m.focusInput()
		return true
	}
	m.focusDecision, m.confirm = ds[pos-1].ID, 0
	m.input.Blur()
	return true
}

func (m *Model) focusInput() {
	m.focusDecision, m.confirm = 0, 0
	m.input.Focus()
}

// decisionKey handles a key while a decision has the focus.
func (m *Model) decisionKey(msg tea.KeyMsg) tea.Cmd {
	d, ok := m.focusedDecision()
	if !ok {
		m.focusInput()
		return nil
	}
	switch msg.Type {
	case tea.KeyTab:
		m.moveFocus(1)
	case tea.KeyShiftTab:
		m.moveFocus(-1)
	case tea.KeyEsc:
		m.focusInput()
	case tea.KeyEnter:
		if m.confirm != 0 {
			return m.answerOption(d, m.confirm)
		}
		// No option chosen: answer in the person's own words, from the input.
		m.focusInput()
		m.input.SetValue(fmt.Sprintf("/answer %d ", d.ID))
		m.input.CursorEnd()
	case tea.KeyRunes:
		if len(msg.Runes) != 1 || msg.Alt {
			return nil
		}
		n, err := strconv.Atoi(string(msg.Runes))
		if err != nil || n < 1 || n > len(d.Options) {
			return nil
		}
		if needsConfirm(d, n) && m.confirm != n {
			m.confirm = n
			return nil
		}
		return m.answerOption(d, n)
	}
	return nil
}

// answerOption sends the person's pick through the same call /answer uses, and takes the
// decision out of the dock at once.
func (m *Model) answerOption(d api.DecisionView, n int) tea.Cmd {
	m.focusInput()
	m.add(Line{Kind: KindYou, Text: fmt.Sprintf("answer to decision %d: %d. %s", d.ID, n, d.Options[n-1])})
	var rest []api.DecisionView
	for _, o := range m.decisions {
		if o.ID != d.ID {
			rest = append(rest, o)
		}
	}
	m.decisions = rest
	return m.answer(d.ID, strconv.Itoa(n))
}

// decisionHeight is the rows decisionRows needs to show every option.
func decisionHeight(d api.DecisionView) int {
	n := 3 + len(d.Options)
	if d.Recommendation != "" {
		n++
	}
	return n
}

// decisionRows is the focused decision, shown in place of the input: the question, its
// options numbered, the recommendation, and the keys; or the confirmation it waits on.
func (m *Model) decisionRows(d api.DecisionView, room int) []string {
	rows := []string{
		styleDecision.Render(fmt.Sprintf("? decision %d", d.ID)) + styleInfo.Render(fmt.Sprintf(" · %s · %s", d.Agent, d.Lane)),
		"  " + oneLine(d.Question),
	}
	shown := len(d.Options)
	if over := len(rows) + shown + 2 - room; over > 0 {
		shown = max(0, shown-over-1)
	}
	for i, o := range d.Options[:shown] {
		rows = append(rows, fmt.Sprintf("  %s %s", styleHead.Render(fmt.Sprintf("%d.", i+1)), oneLine(o)))
	}
	if shown < len(d.Options) {
		rows = append(rows, styleInfo.Render(fmt.Sprintf("  +%d more options: /answer %d <number>", len(d.Options)-shown, d.ID)))
	}
	if d.Recommendation != "" {
		rows = append(rows, styleInfo.Render("  recommends: "+oneLine(d.Recommendation)))
	}
	var keys string
	switch {
	case m.confirm != 0:
		keys = pal.warn.Render(fmt.Sprintf("Answer %d. %s? Enter to confirm · Esc back", m.confirm, oneLine(d.Options[m.confirm-1])))
	case len(d.Options) == 0:
		keys = styleInfo.Render("Enter answer in your words · Esc back · Tab next")
	default:
		keys = styleInfo.Render(fmt.Sprintf("1-%d answer · Esc back · Enter own words · Tab next", min(len(d.Options), 9)))
	}
	return append(rows, keys)
}

// keysWithDecisions adds Tab to the keys line when decisions are waiting.
func keysWithDecisions(keys []string, items []dockItem) []string {
	for _, it := range items {
		if it.decision != 0 {
			return append(keys, "Tab decisions")
		}
	}
	return keys
}
