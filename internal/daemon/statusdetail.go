package daemon

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// reloadState is how the last config reload went: loaded is when one took, and failed
// with err when one was refused. The daemon's own start counts as the first load.
type reloadState struct {
	loaded time.Time
	failed time.Time
	err    string
}

// maxErrLine bounds an error shown in the status detail.
const maxErrLine = 200

// oneLine is s redacted, on one line, and cut to maxErrLine.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(redact.String(s)), " ")
	if r := []rune(s); len(r) > maxErrLine {
		s = string(r[:maxErrLine-1]) + "…"
	}
	return s
}

// buildStamp is the VCS revision and time Go recorded in the binary, if it did.
func buildStamp() (commit, built string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, kv := range bi.Settings {
		switch kv.Key {
		case "vcs.revision":
			commit = kv.Value
		case "vcs.time":
			built = kv.Value
		}
	}
	return commit, built
}

// statusDetail gathers api.StatusDetail from what the daemon already keeps: the
// store's runs, lanes, decisions and spend, the live config, and the reload outcome.
func (s *Server) statusDetail(ctx context.Context) (api.StatusDetail, error) {
	cfg := s.LiveConfig()
	d := api.StatusDetail{OS: runtime.GOOS, Arch: runtime.GOARCH, Now: time.Now().UTC(), ConfigLoaded: s.started}
	d.Commit, d.BuiltAt = buildStamp()
	d.Host, _ = os.Hostname()
	if rs := s.reload.Load(); rs != nil {
		if !rs.loaded.IsZero() {
			d.ConfigLoaded = rs.loaded
		}
		d.ReloadError, d.ReloadFailedAt = oneLine(rs.err), rs.failed
	}
	running, err := s.Store.Runs(ctx, 0, store.RunRunning, 1000)
	if err != nil {
		return d, err
	}
	d.Running = len(running)
	decisions, err := s.Store.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		return d, err
	}
	d.Decisions = len(decisions)
	wss, err := s.Store.Workspaces(ctx)
	if err != nil {
		return d, err
	}
	for _, ws := range wss {
		repos, err := s.Store.Repos(ctx, ws.ID)
		if err != nil {
			return d, err
		}
		for _, rp := range repos {
			lanes, err := s.Store.Lanes(ctx, rp.ID)
			if err != nil {
				return d, err
			}
			for _, l := range lanes {
				if l.State == store.LaneOpen {
					d.OpenLanes++
				}
			}
		}
	}
	if d.SpendUSD, _, err = dispatch.Spent(ctx, s.Store, cfg); err != nil {
		return d, err
	}
	if b := cfg.Daemon.Budget; b != nil {
		d.BudgetUSD = *b
	}
	if s.QuotaHolds != nil {
		holds := s.QuotaHolds()
		if holds == nil {
			holds = []api.QuotaHold{}
		}
		d.QuotaHolds = &holds
	}
	if s.ForgeWatch != nil {
		fw := s.ForgeWatch()
		fw.Error = oneLine(fw.Error)
		d.Forge = &fw
	}
	return d, nil
}
