// Package api holds the HTTP API's request and response types, shared by the daemon and
// its clients. Every client goes through this API, the CLI on the same machine included.
package api

import (
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Routes.
const (
	PathStatus     = "/v1/status"
	PathWorkspaces = "/v1/workspaces"
	PathResolve    = "/v1/resolve"
	PathShutdown   = "/v1/shutdown"
	PathLanes      = "/v1/lanes"
	PathFoldGC     = "/v1/fold/gc"
	PathPrePush    = "/v1/hook/pre-push"
)

// PathRepoHook is POST /v1/repos/{id}/hook.
func PathRepoHook(id int64) string { return fmt.Sprintf("/v1/repos/%d/hook", id) }

// PathLaneClose is POST /v1/lanes/{id}/close.
func PathLaneClose(id int64) string { return fmt.Sprintf("%s/%d/close", PathLanes, id) }

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

// LaneView is a lane with its repo's name, as lists show it.
type LaneView struct {
	store.Lane
	Repo string `json:"repo"`
}

// CloseLane is the body of POST /v1/lanes/{id}/close.
type CloseLane struct {
	Force bool `json:"force"`
}

// PrePush is the body of POST /v1/hook/pre-push.
type PrePush struct {
	// Path is the worktree git ran the hook in.
	Path string         `json:"path"`
	Refs []fold.PushRef `json:"refs"`
}

// RepoHook is the body of POST /v1/repos/{id}/hook: install, uninstall or status.
type RepoHook struct {
	Action string `json:"action"`
}

// Error is the body of every non-2xx response.
type Error struct {
	Error string `json:"error"`
}
