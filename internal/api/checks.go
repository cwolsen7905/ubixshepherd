package api

import "github.com/ubixsys/ubixshepherd/internal/store"

// Approved check files: test files the person marked as the definition of done. No lane
// may change one while it is marked. The routes are the person's: GET lists for every
// client (an agent should know what it may not touch), approve and release are refused
// to agents and, from the front desk, outside a turn the person started.

// PathChecks is GET /v1/checks?repo_id=N[&all=1][&status=1]: the repo's marks. all adds
// released ones; status reads each live mark against the base branch.
const PathChecks = "/v1/checks"

// PathChecksApprove is POST /v1/checks/approve.
const PathChecksApprove = PathChecks + "/approve"

// PathChecksRelease is POST /v1/checks/release.
const PathChecksRelease = PathChecks + "/release"

// Checks is the answer of GET /v1/checks.
type Checks struct {
	RepoID int64  `json:"repo_id"`
	Repo   string `json:"repo"`
	// Files are newest first. With status=1 only live marks are read, and Status holds
	// them with their state against the base branch.
	Files  []store.CheckFile `json:"files"`
	Status []CheckStatus     `json:"status,omitempty"`
}

// Check states in CheckStatus.State.
const (
	CheckCurrent = "current" // the base branch has the approved blob
	CheckChanged = "changed" // the base branch has a different blob
	CheckMissing = "missing" // the base branch no longer has the file
)

// CheckStatus is a live mark and whether the base branch still holds what was approved.
type CheckStatus struct {
	store.CheckFile
	State string `json:"state"`
	// BaseBlob is the blob now on the base branch, when it differs.
	BaseBlob string `json:"base_blob,omitempty"`
}

// ApproveChecks is the body of POST /v1/checks/approve: files already on the repo's base
// branch. Decision is required from the front desk: the answered decision in which the
// person approved them.
type ApproveChecks struct {
	RepoID   int64    `json:"repo_id"`
	Paths    []string `json:"paths"`
	ItemID   int64    `json:"item_id,omitempty"`
	Set      string   `json:"set,omitempty"`
	Decision int64    `json:"decision,omitempty"`
}

// ReleaseChecks is the body of POST /v1/checks/release. Force releases although a lane
// for the checks' next-up item is open.
type ReleaseChecks struct {
	RepoID   int64    `json:"repo_id"`
	Paths    []string `json:"paths"`
	Why      string   `json:"why"`
	Force    bool     `json:"force,omitempty"`
	Decision int64    `json:"decision,omitempty"`
}

// CheckChange is the answer of approve and release: the rows changed.
type CheckChange struct {
	Files []store.CheckFile `json:"files"`
}

// Rescoped is the answer of POST /v1/lanes/{id}/scope: the lane, and the approved check
// files its scope reaches but may not change. It reads as a lane to a client that
// ignores Excluded.
type Rescoped struct {
	store.Lane
	Excluded []string `json:"excluded,omitempty"`
}
