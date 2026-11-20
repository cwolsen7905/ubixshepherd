package chat

import (
	tea "github.com/charmbracelet/bubbletea"
)

// loadKeepGoingSummary is the command that shows what keep-going did since the person
// last turned it on, as an info line in the thread when the chat attaches. It says
// nothing when the mode never ran, and a failed read is not worth a line of its own:
// the next /keep-going status shows the error.
func (m *Model) loadKeepGoingSummary() tea.Cmd {
	return func() tea.Msg {
		st, err := m.api.KeepGoing(m.ctx, m.workspace.ID)
		if err != nil || st.Summary == "" {
			return nil
		}
		return lineMsg{Kind: KindInfo, Text: st.Summary}
	}
}
