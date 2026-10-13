package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/update"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// NoUpdateCheckEnv, set to 1, stops the daemon calling GitHub for the release check
// whatever the config says: for tests, CI and machines with no route out.
const NoUpdateCheckEnv = "SHEPHERD_NO_UPDATE_CHECK"

// updates is the release checker, made on first use from the live config so a reload's
// update.check and update.channel apply to it. Its dismissal lives next to the config.
func (s *Server) updates() *update.Checker {
	s.updateOnce.Do(func() {
		if s.Updates != nil {
			return
		}
		state := ""
		if s.ConfigPath != "" {
			state = filepath.Join(filepath.Dir(s.ConfigPath), "update.json")
		}
		s.Updates = &update.Checker{
			Current: version.Version, StatePath: state, Log: s.Log,
			Settings: func() (bool, string) {
				u := s.LiveConfig().Update
				return u.Enabled() && os.Getenv(NoUpdateCheckEnv) != "1", u.ChannelName()
			},
		}
	})
	return s.Updates
}

// updateStatus is the checker's answer as the API says it.
func (s *Server) updateStatus() api.UpdateStatus {
	st := s.updates().Status()
	out := api.UpdateStatus{
		Enabled: st.Enabled, Channel: st.Channel, Current: st.Current, CurrentKnown: st.CurrentKnown,
		Available: st.Available, Dismissed: st.Dismissed, CheckedAt: st.CheckedAt, Error: st.LastError,
	}
	if st.Latest != nil {
		out.Latest, out.URL, out.Notes = st.Latest.Version, st.Latest.URL, st.Latest.Notes
	}
	// An unknown build cannot be compared, so it is told of the latest release anyway.
	out.Upgrade = s.upgradeView(st)
	out.Show = out.Enabled && out.Latest != "" && !out.Dismissed && (out.Available || !out.CurrentKnown)
	return out
}

// getUpdate answers GET /v1/update from the cache; it never calls GitHub.
func (s *Server) getUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateStatus())
}

// dismissUpdate answers POST /v1/update/dismiss. Only the version now offered can be
// dismissed, so a client that fell behind cannot hide a newer release it never saw.
func (s *Server) dismissUpdate(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateDismiss
	if !decode(w, r, &req) {
		return
	}
	if latest := s.updateStatus().Latest; latest == "" || req.Version != latest {
		writeError(w, http.StatusConflict, errors.New("that is not the version now offered: reload the notice"))
		return
	}
	if err := s.updates().Dismiss(req.Version); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.updateStatus())
}

// startUpdateCheck runs the daily check until ctx ends. Run calls it, so a Server built
// for a test never calls out.
func (s *Server) startUpdateCheck(ctx context.Context) {
	if os.Getenv(NoUpdateCheckEnv) == "1" {
		return
	}
	go s.updates().Run(ctx)
	go s.autoUpgradeLoop(ctx)
}
