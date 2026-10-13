package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/update"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// UpgradeHooks are the parts of an upgrade a test replaces.
type UpgradeHooks struct {
	// Method says how the binary was installed; nil detects it from the path.
	Method func(exe string, known bool) update.Method
	// Installer makes the installer for exe; nil is the real one, which downloads from the
	// project's releases and proves the new binary runs.
	Installer func(exe string) *update.Installer
	// Active says how many agents are running; nil asks the runner.
	Active func() int
	// IdlePoll is how often a pending restart looks for an idle moment (default 5s);
	// AutoEvery how often update.auto looks for a release (default 5 minutes);
	// VerifyAfter how long a new binary must serve before it is trusted (default 30s).
	IdlePoll, AutoEvery, VerifyAfter time.Duration
}

// upgradeState is where the one upgrade in flight stands.
type upgradeState struct {
	mu     sync.Mutex
	state  string // api.Upgrade*; "" is idle
	target string
	err    string
}

func (s *Server) exe() string {
	if s.Exe != "" {
		return s.Exe
	}
	exe, _ := os.Executable()
	return exe
}

func (s *Server) method(known bool) update.Method {
	if s.Upgrade.Method != nil {
		return s.Upgrade.Method(s.exe(), known)
	}
	return update.Detect(s.exe(), known)
}

// markerPath is where an unproven upgrade is recorded, next to the config.
func (s *Server) markerPath() string {
	if s.ConfigPath == "" {
		return ""
	}
	return UpgradeMarkerPath(s.ConfigPath)
}

// UpgradeMarkerPath is the marker's place for a config file at configPath.
func UpgradeMarkerPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "upgrade.json")
}

// RestartRequested says Run returned so the process can start again on the binary now in
// place: an upgrade installed and no agent was left running (or the person said now).
func (s *Server) RestartRequested() bool { return s.restartReq.Load() }

// upgradeView is the notice's upgrade step, from the checker's status and the state in flight.
func (s *Server) upgradeView(st update.Status) api.UpgradeStatus {
	m := s.method(st.CurrentKnown)
	latest := ""
	if st.Latest != nil {
		latest = st.Latest.Version
	}
	s.upg.mu.Lock()
	state, target, errText := s.upg.state, s.upg.target, s.upg.err
	s.upg.mu.Unlock()
	out := api.UpgradeStatus{
		State: api.UpgradeIdle, Method: string(m), Target: target, Error: errText,
		Auto: s.LiveConfig().Update.AutoUpgrade(),
	}
	if state != "" {
		out.State = state
	}
	out.ActiveRuns = s.activeRuns()
	if out.State == api.UpgradeIdle && st.FailedVersion != "" && st.FailedVersion == latest {
		out.State = api.UpgradeFailed
		if st.RolledBack {
			out.State = api.UpgradeRolledBack
		}
		out.Error = st.FailedWhy
	}
	if st.FailedVersion != "" {
		out.FailedVersion = st.FailedVersion
	}
	out.CanUpgrade = m == update.MethodBinary && latest != "" && st.Available
	if m != update.MethodBinary && latest != "" {
		out.Hint = m.Hint(latest)
	}
	return out
}

// upgradeError is a refusal with the HTTP code it deserves.
type upgradeError struct {
	code int
	msg  string
}

func (e *upgradeError) Error() string { return e.msg }

// StartUpgrade installs the offered release and arranges the restart onto it. It returns
// once the install is under way; the state is in the update status. now restarts at once
// even with agents running, which interrupts them; otherwise the restart waits for none.
// auto marks an upgrade the daemon began itself (update.auto).
func (s *Server) StartUpgrade(version string, now, auto bool) error {
	s.upg.mu.Lock()
	switch s.upg.state {
	case api.UpgradeInstalling, api.UpgradeRestarting:
		s.upg.mu.Unlock()
		return &upgradeError{http.StatusConflict, "an upgrade is already in progress"}
	case api.UpgradeInstalled:
		target := s.upg.target
		s.upg.mu.Unlock()
		if version != target {
			return &upgradeError{http.StatusConflict, target + " is already installed and waiting for a restart"}
		}
		if now {
			s.requestRestart()
		}
		return nil
	}
	s.upg.mu.Unlock()

	st := s.updates().Status()
	if st.Latest == nil || st.Latest.Version != version {
		return &upgradeError{http.StatusConflict, "that is not the version now offered: reload the notice"}
	}
	if !st.Available {
		return &upgradeError{http.StatusConflict, version + " is not newer than this build"}
	}
	if m := s.method(st.CurrentKnown); m != update.MethodBinary {
		return &upgradeError{http.StatusConflict, "Shepherd will not replace this install itself: " + m.Hint(version)}
	}
	rel := *st.Latest
	s.upg.mu.Lock()
	if s.upg.state == api.UpgradeInstalling || s.upg.state == api.UpgradeInstalled {
		s.upg.mu.Unlock()
		return &upgradeError{http.StatusConflict, "an upgrade is already in progress"}
	}
	s.upg.state, s.upg.target, s.upg.err = api.UpgradeInstalling, version, ""
	s.upg.mu.Unlock()
	if !auto {
		s.updates().ClearFailed()
	}
	go s.install(rel, now)
	return nil
}

func (s *Server) upgradeFailed(version string, err error) {
	s.Log.Warn("upgrade failed", "version", version, "err", err)
	s.upg.mu.Lock()
	s.upg.state, s.upg.err = api.UpgradeFailed, err.Error()
	s.upg.mu.Unlock()
	s.updates().MarkFailed(version, err.Error(), false)
}

// install replaces the binary, writes the marker that lets the next start prove it, and
// hands over to the restart.
func (s *Server) install(rel update.Release, now bool) {
	s.Log.Info("upgrading", "from", version.Version, "to", rel.Version, "exe", s.exe())
	inst := &update.Installer{Exe: s.exe(), Check: update.RunVersion}
	if s.Upgrade.Installer != nil {
		inst = s.Upgrade.Installer(s.exe())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := inst.Install(ctx, rel); err != nil {
		s.upgradeFailed(rel.Version, err)
		return
	}
	if p := s.markerPath(); p != "" {
		m := update.Marker{From: version.Version, To: rel.Version, At: time.Now().UTC()}
		if err := update.WriteMarker(p, m); err != nil {
			// Without the marker a bad start could not be rolled back: put the old one back.
			if rerr := update.Restore(s.exe()); rerr != nil {
				err = errors.Join(err, rerr)
			}
			s.upgradeFailed(rel.Version, err)
			return
		}
	}
	s.upg.mu.Lock()
	s.upg.state = api.UpgradeInstalled
	s.upg.mu.Unlock()
	s.Log.Info("upgrade installed; restarting when no agent is running", "version", rel.Version)
	if now {
		s.requestRestart()
		return
	}
	s.restartWhenIdle()
}

// restartWhenIdle asks for the restart at the first moment no agent is running.
func (s *Server) restartWhenIdle() {
	poll := s.Upgrade.IdlePoll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		s.upg.mu.Lock()
		state := s.upg.state
		s.upg.mu.Unlock()
		if state != api.UpgradeInstalled {
			return // the person restarted it already
		}
		if s.activeRuns() == 0 {
			s.requestRestart()
			return
		}
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
	}
}

// requestRestart stops Run and marks that the process should start again.
func (s *Server) requestRestart() {
	s.upg.mu.Lock()
	s.upg.state = api.UpgradeRestarting
	s.upg.mu.Unlock()
	s.Log.Info("restarting onto the upgraded binary", "active_runs", s.activeRuns())
	s.restartReq.Store(true)
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Server) activeRuns() int {
	if s.Upgrade.Active != nil {
		return s.Upgrade.Active()
	}
	if s.Runner == nil {
		return 0
	}
	return s.Runner.Active()
}

// upgradeUpdate answers POST /v1/update/upgrade.
func (s *Server) upgradeUpdate(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateUpgrade
	if !decode(w, r, &req) {
		return
	}
	if err := s.StartUpgrade(req.Version, req.Now, false); err != nil {
		var ue *upgradeError
		if errors.As(err, &ue) {
			writeError(w, ue.code, err)
			return
		}
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.updateStatus())
}

// autoUpgradeLoop is update.auto: a newer release on the channel is installed without a
// click. It skips a dismissed release and one an earlier attempt failed for.
func (s *Server) autoUpgradeLoop(ctx context.Context) {
	every := s.Upgrade.AutoEvery
	if every <= 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.autoUpgradeOnce()
		}
	}
}

func (s *Server) autoUpgradeOnce() {
	if !s.LiveConfig().Update.AutoUpgrade() {
		return
	}
	st := s.updates().Status()
	if !st.Enabled || st.Latest == nil || !st.Available || st.Dismissed || st.FailedVersion == st.Latest.Version {
		return
	}
	if s.method(st.CurrentKnown) != update.MethodBinary {
		return
	}
	if err := s.StartUpgrade(st.Latest.Version, false, true); err != nil {
		s.Log.Debug("automatic upgrade not started", "err", err)
	}
}

// startUpgradeVerify trusts a freshly upgraded binary after it has served for a while:
// the marker that would roll it back on a bad start is cleared.
func (s *Server) startUpgradeVerify(ctx context.Context) {
	p := s.markerPath()
	if p == "" {
		return
	}
	m, ok := update.ReadMarker(p)
	if !ok || m.To != version.Version {
		return
	}
	after := s.Upgrade.VerifyAfter
	if after <= 0 {
		after = 30 * time.Second
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(after):
			update.ClearMarker(p)
			s.Log.Info("upgrade verified", "from", m.From, "to", m.To)
		}
	}()
}

// clearOwnMarker is a clean stop of a freshly upgraded daemon: not a bad start, so the
// next launch must not roll back. (A crash never reaches it.)
func (s *Server) clearOwnMarker() {
	if p := s.markerPath(); p != "" {
		if m, ok := update.ReadMarker(p); ok && m.To == version.Version {
			update.ClearMarker(p)
		}
	}
}
