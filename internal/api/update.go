package api

import "time"

// The release check (docs/update-check.md). The daemon asks GitHub about once a day and
// serves what it found; no client calls GitHub.
//
//	GET  /v1/update          UpdateStatus
//	POST /v1/update/dismiss  UpdateDismiss, answers UpdateStatus
//	POST /v1/update/upgrade  UpdateUpgrade, answers UpdateStatus (202 once started)
//
// Both are the person's: the operator token and a browser session may call them, an
// agent's token and the front desk may not.
const (
	PathUpdate        = "/v1/update"
	PathUpdateDismiss = "/v1/update/dismiss"
	PathUpdateUpgrade = "/v1/update/upgrade"
)

// UpdateStatus is the cached check against the build's own version.
type UpdateStatus struct {
	// Enabled is update.check. A disabled check still reports what it last found.
	Enabled bool   `json:"enabled"`
	Channel string `json:"channel"`
	// Current is the daemon's build version. CurrentKnown is false for a dev build or
	// source install, whose version is not a release number: Latest is still reported,
	// but nothing is compared and Available stays false.
	Current      string `json:"current"`
	CurrentKnown bool   `json:"current_known"`
	// Latest is the newest release on the channel; empty before a check has succeeded.
	Latest string `json:"latest,omitempty"`
	URL    string `json:"url,omitempty"`
	// Notes is the release's changelog section, as published (markdown).
	Notes string `json:"notes,omitempty"`
	// Available says Latest is newer than a known Current.
	Available bool `json:"available"`
	// Dismissed says the person dismissed Latest's version; a newer one shows again.
	Dismissed bool `json:"dismissed"`
	// Show is what a client should do with its notice: Available or, for an unknown
	// build, Latest exists; and not Dismissed. Clients use it as is.
	Show bool `json:"show"`
	// CheckedAt is the last good check; Error is the last failure, cleared by a good one.
	CheckedAt time.Time `json:"checked_at,omitzero"`
	Error     string    `json:"error,omitempty"`
	// Upgrade is where an upgrade stands and whether one can be started.
	Upgrade UpgradeStatus `json:"upgrade"`
}

// The upgrade's states.
const (
	UpgradeIdle       = "idle"
	UpgradeInstalling = "installing"
	// UpgradeInstalled: the new binary is in place and the daemon restarts onto it once no
	// agent is running (or when the person says now).
	UpgradeInstalled  = "installed"
	UpgradeRestarting = "restarting"
	UpgradeFailed     = "failed"
	UpgradeRolledBack = "rolled_back"
)

// UpgradeStatus is the upgrade step of the notice.
type UpgradeStatus struct {
	State string `json:"state"`
	// Method is how this binary was installed: binary, brew, go, source or readonly.
	Method string `json:"method"`
	// CanUpgrade says the daemon can install Latest itself; when it cannot, Hint says
	// what the person does instead.
	CanUpgrade bool   `json:"can_upgrade"`
	Hint       string `json:"hint,omitempty"`
	// Target is the version being installed or waiting for a restart.
	Target string `json:"target,omitempty"`
	// ActiveRuns is how many agents a restart now would interrupt.
	ActiveRuns int `json:"active_runs"`
	// Error is why the last attempt failed; FailedVersion is the release it was for. An
	// automatic upgrade does not retry that release.
	Error         string `json:"error,omitempty"`
	FailedVersion string `json:"failed_version,omitempty"`
	// Auto is update.auto.
	Auto bool `json:"auto"`
}

// UpdateUpgrade is the body of POST /v1/update/upgrade: the version to install, which
// must be the one now offered. Now restarts at once even with agents running, which
// interrupts their runs; without it the restart waits until none is. A version already
// installed and waiting is restarted onto by Now alone.
type UpdateUpgrade struct {
	Version string `json:"version"`
	Now     bool   `json:"now,omitempty"`
}

// UpdateDismiss is the body of POST /v1/update/dismiss: the version to hide. A version
// other than the one now offered is refused, so a stale client cannot hide a newer one.
type UpdateDismiss struct {
	Version string `json:"version"`
}
