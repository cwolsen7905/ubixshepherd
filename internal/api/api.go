// Package api holds the HTTP API's request and response types, shared by the daemon and
// its clients. Every client goes through this API, the CLI on the same machine included.
package api

import (
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Routes.
const (
	PathStatus     = "/v1/status"
	PathWorkspaces = "/v1/workspaces"
	PathResolve    = "/v1/resolve"
	PathShutdown   = "/v1/shutdown"
)

// Runtime is what a running daemon writes to its runtime file so clients can find it.
type Runtime struct {
	Addr    string    `json:"addr"`
	PID     int       `json:"pid"`
	Token   string    `json:"token"`
	Version string    `json:"version"`
	Started time.Time `json:"started"`
}

// Status answers GET /v1/status.
type Status struct {
	Version    string             `json:"version"`
	PID        int                `json:"pid"`
	Started    time.Time          `json:"started"`
	Store      string             `json:"store"`
	Config     string             `json:"config"`
	Workspaces []WorkspaceSummary `json:"workspaces"`
}

// WorkspaceSummary is one workspace in Status.
type WorkspaceSummary struct {
	store.Workspace
	Repos int `json:"repos"`
	Lanes int `json:"lanes"`
}

// SaveWorkspace is the body of POST /v1/workspaces.
type SaveWorkspace struct {
	Name  string       `json:"name"`
	Path  string       `json:"path"`
	Repos []store.Repo `json:"repos"`
}

// WorkspaceDetail answers POST and GET on workspaces.
type WorkspaceDetail struct {
	store.Workspace
	Repos []store.Repo `json:"repos"`
}

// Resolution answers GET /v1/resolve?path=..., saying what a directory belongs to.
// Fields are empty from the bottom up: a path in a workspace but no repo has only
// Workspace set.
type Resolution struct {
	Path      string           `json:"path"`
	Workspace *store.Workspace `json:"workspace,omitempty"`
	Repo      *store.Repo      `json:"repo,omitempty"`
	Lane      *store.Lane      `json:"lane,omitempty"`
	// Profile is the repo's effective profile when Repo is set.
	Profile *config.Profile `json:"profile,omitempty"`
}

// Error is the body of every non-2xx response.
type Error struct {
	Error string `json:"error"`
}
