package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// updateAPI is the fake API with the daemon's release check.
type updateAPI struct {
	*fakeAPI
	st        api.UpdateStatus
	dismissed string
}

func (u *updateAPI) Update(context.Context) (api.UpdateStatus, error) { return u.st, nil }
func (u *updateAPI) DismissUpdate(_ context.Context, v string) (api.UpdateStatus, error) {
	u.dismissed = v
	u.st.Dismissed, u.st.Show = true, false
	return u.st, nil
}

func TestUpdateNoticeAndDismiss(t *testing.T) {
	m, _, base := newTestModel()
	u := &updateAPI{fakeAPI: base, st: api.UpdateStatus{
		Enabled: true, Current: "v0.4.0", CurrentKnown: true, Latest: "v0.5.0-beta.2",
		URL: "https://example.test/r/2", Available: true, Show: true,
	}}
	m.api = u
	if strings.Contains(ansi.Strip(m.View()), "update:") {
		t.Fatal("a notice before the daemon was asked")
	}
	drive(t, m, m.pollPanel(true))
	v := ansi.Strip(m.View())
	for _, want := range []string{"v0.5.0-beta.2 is available", "https://example.test/r/2", "/update dismiss"} {
		if !strings.Contains(v, want) {
			t.Errorf("notice lacks %q:\n%s", want, v)
		}
	}

	typeLine(t, m, "/update dismiss")
	if u.dismissed != "v0.5.0-beta.2" {
		t.Errorf("dismissed %q", u.dismissed)
	}
	if strings.Contains(ansi.Strip(m.View()), "update:") {
		t.Error("the notice stayed after it was dismissed")
	}
}

func TestUpdateNoticeForAnUnknownBuild(t *testing.T) {
	m, _, base := newTestModel()
	m.api = &updateAPI{fakeAPI: base, st: api.UpdateStatus{
		Enabled: true, Current: "dev", Latest: "v0.5.0-beta.2", Show: true,
	}}
	drive(t, m, m.pollPanel(true))
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "latest release is v0.5.0-beta.2 (this build is dev)") {
		t.Errorf("no notice for a dev build:\n%s", v)
	}
}

func TestNoUpdateNoticeWithoutTheDaemonsCheck(t *testing.T) {
	m, _, _ := newTestModel() // the plain fake has no release check
	drive(t, m, m.pollPanel(true))
	if strings.Contains(ansi.Strip(m.View()), "update:") {
		t.Error("a notice with no release check")
	}
}

type upgradeAPI struct {
	*updateAPI
	version string
	now     bool
}

func (u *upgradeAPI) UpgradeUpdate(_ context.Context, v string, now bool) (api.UpdateStatus, error) {
	u.version, u.now = v, now
	u.st.Upgrade.State, u.st.Upgrade.Target = api.UpgradeInstalled, v
	return u.st, nil
}

func TestUpdateUpgradeFromTheNotice(t *testing.T) {
	m, _, base := newTestModel()
	u := &upgradeAPI{updateAPI: &updateAPI{fakeAPI: base, st: api.UpdateStatus{
		Enabled: true, Current: "v0.4.0", CurrentKnown: true, Latest: "v0.5.0", Available: true, Show: true,
		Upgrade: api.UpgradeStatus{State: api.UpgradeIdle, CanUpgrade: true},
	}}}
	m.api = u
	drive(t, m, m.pollPanel(true))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "/update upgrade") {
		t.Fatalf("notice offers no upgrade:\n%s", v)
	}
	typeLine(t, m, "/update upgrade")
	if u.version != "v0.5.0" || u.now {
		t.Errorf("upgrade %q now=%v", u.version, u.now)
	}
	typeLine(t, m, "/update upgrade now")
	if !u.now {
		t.Error("now was not passed")
	}
	drive(t, m, m.pollPanel(true))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "restarts when no agent is running") {
		t.Errorf("no pending-restart line:\n%s", v)
	}
}
