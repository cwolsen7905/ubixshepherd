package chat

import (
	"context"
	"fmt"
	"strings"

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
type updateMsg struct {
	st *api.UpdateStatus
	// note is what to tell the person; "" says the notice was dismissed.
	note string
}

// upgrader is the daemon's upgrade, which a daemon too old for it does not have.
type upgrader interface {
	UpgradeUpdate(ctx context.Context, version string, now bool) (api.UpdateStatus, error)
}

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
	switch u := st.Upgrade; {
	case u.State == api.UpgradeInstalling:
		return row + styleInfo.Render(" · installing "+u.Target)
	case u.State == api.UpgradeInstalled:
		return row + styleInfo.Render(fmt.Sprintf(" · installed, restarts when no agent is running (%d now) · /update upgrade now", u.ActiveRuns))
	case u.State == api.UpgradeRestarting:
		return row + styleInfo.Render(" · restarting")
	case u.CanUpgrade:
		row += styleInfo.Render(" · /update upgrade")
	case u.State == api.UpgradeFailed || u.State == api.UpgradeRolledBack:
		row += styleInfo.Render(" · upgrade failed: " + clip(u.Error, 80))
	case u.Hint != "":
		row += styleInfo.Render(" · " + clip(u.Hint, 100))
	}
	return row + styleInfo.Render(" · /update dismiss")
}

// upgradeUpdate has the daemon install the release offered; now restarts at once, which
// interrupts running agents.
func (m *Model) upgradeUpdate(now bool) tea.Cmd {
	u, ok := m.api.(upgrader)
	if !ok || m.release == nil || m.release.Latest == "" {
		m.add(Line{Kind: KindInfo, Text: "No update to install."})
		return nil
	}
	version := m.release.Latest
	if t := m.release.Upgrade.Target; m.release.Upgrade.State == api.UpgradeInstalled && t != "" {
		version = t
	}
	return func() tea.Msg {
		st, err := u.UpgradeUpdate(m.ctx, version, now)
		if err != nil {
			return errorMsg{err}
		}
		note := "Installing " + version + ": the daemon restarts onto it when no agent is running. /update upgrade now restarts at once and interrupts them."
		if now {
			note = "Installing " + version + " and restarting at once."
		}
		return updateMsg{st: &st, note: note}
	}
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

// clip shortens s to at most n runes for the one-line notice.
func clip(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
