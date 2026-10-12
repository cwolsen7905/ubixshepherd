package chat

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// updater is what the chat needs of the daemon's release check. It is separate from API
// so a client without it (and every fake that predates it) shows no notice.
type updater interface {
	Update(ctx context.Context) (api.UpdateStatus, error)
	DismissUpdate(ctx context.Context, version string) (api.UpdateStatus, error)
}

// updateMsg is the release check as the daemon last answered; nil clears the notice.
type updateMsg struct{ st *api.UpdateStatus }

// loadUpdate reads the daemon's cached release check. A daemon too old to have it, or
// any failure, leaves things as they are: the notice is never worth an error line.
func (m *Model) loadUpdate() (api.UpdateStatus, bool) {
	u, ok := m.api.(updater)
	if !ok {
		return api.UpdateStatus{}, false
	}
	st, err := u.Update(m.ctx)
	return st, err == nil
}

// updateRow is the one-line notice, or "" when there is nothing to say. An unknown build
// (a dev build or source install) is told of the latest release without a comparison.
func (m *Model) updateRow() string {
	st := m.release
	if st == nil || !st.Show {
		return ""
	}
	what := fmt.Sprintf("%s is available", st.Latest)
	if !st.CurrentKnown {
		what = fmt.Sprintf("latest release is %s (this build is %s)", st.Latest, st.Current)
	}
	row := "  " + pal.accent.Render("↑") + " " + styleInfo.Render("update: "+what)
	if st.URL != "" {
		row += styleInfo.Render(" · " + st.URL)
	}
	return row + styleInfo.Render(" · /update dismiss")
}

// dismissUpdate hides the notice for the version shown.
func (m *Model) dismissUpdate() tea.Cmd {
	u, ok := m.api.(updater)
	if !ok || m.release == nil || m.release.Latest == "" {
		m.add(Line{Kind: KindInfo, Text: "No update notice to dismiss."})
		return nil
	}
	version := m.release.Latest
	return func() tea.Msg {
		st, err := u.DismissUpdate(m.ctx, version)
		if err != nil {
			return errorMsg{err}
		}
		return updateMsg{st: &st}
	}
}
