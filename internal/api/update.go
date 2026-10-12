package api

import "time"

// The release check (docs/update-check.md). The daemon asks GitHub about once a day and
// serves what it found; no client calls GitHub.
//
//	GET  /v1/update          UpdateStatus
//	POST /v1/update/dismiss  UpdateDismiss, answers UpdateStatus
//
// Both are the person's: the operator token and a browser session may call them, an
// agent's token and the front desk may not.
const (
	PathUpdate        = "/v1/update"
	PathUpdateDismiss = "/v1/update/dismiss"
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
}

// UpdateDismiss is the body of POST /v1/update/dismiss: the version to hide. A version
// other than the one now offered is refused, so a stale client cannot hide a newer one.
type UpdateDismiss struct {
	Version string `json:"version"`
}
