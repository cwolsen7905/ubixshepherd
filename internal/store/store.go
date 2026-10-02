// Package store is the only way to Shepherd's state. The daemon holds the one Store;
// clients go through the HTTP API. SQLite backs it on a single machine; a server database
// can implement the same interface when Shepherd is hosted.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write collides with something already there.
var ErrConflict = errors.New("conflict")

// Lane states.
const (
	// LaneOpening: recorded, branch and worktree being created.
	LaneOpening = "opening"
	LaneOpen    = "open"
	LaneClosed  = "closed"
)

// Workspace is a directory of repos that one Shepherd works over.
type Workspace struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Created time.Time `json:"created"`
}

// Repo is a git repository that has opted in to a workspace.
type Repo struct {
	ID          int64 `json:"id"`
	WorkspaceID int64 `json:"workspace_id"`
	// Name is the repo's path relative to the workspace, with forward slashes.
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
	// Stacks are the generic markers found in the repo (go, node, php, ...), from which
	// a pack is chosen.
	Stacks  []string  `json:"stacks,omitempty"`
	Created time.Time `json:"created"`
}

// Lane is one stream of work in a repo, on its own branch and worktree.
type Lane struct {
	ID     int64  `json:"id"`
	RepoID int64  `json:"repo_id"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	// Base is the branch the lane was cut from and lands on.
	Base     string `json:"base"`
	Worktree string `json:"worktree"`
	// Scope is the globs, relative to the repo, the lane's work stays inside.
	Scope   []string   `json:"scope"`
	State   string     `json:"state"`
	Created time.Time  `json:"created"`
	Closed  *time.Time `json:"closed,omitempty"`
}

// Store is Shepherd's state.
type Store interface {
	// SaveWorkspace creates the workspace at ws.Path, or renames the one already there,
	// and adds or updates each repo by path. Repos not listed are left as they are.
	SaveWorkspace(ctx context.Context, ws Workspace, repos []Repo) (Workspace, error)
	Workspaces(ctx context.Context) ([]Workspace, error)
	Repos(ctx context.Context, workspaceID int64) ([]Repo, error)
	Repo(ctx context.Context, id int64) (Repo, error)
	// Lanes returns a repo's lanes that are not closed.
	Lanes(ctx context.Context, repoID int64) ([]Lane, error)
	Lane(ctx context.Context, id int64) (Lane, error)
	// CreateLane records a lane; ErrConflict if its name or worktree is taken by a lane
	// that is not closed.
	CreateLane(ctx context.Context, l Lane) (Lane, error)
	SetLaneState(ctx context.Context, id int64, state string) error
	// DeleteLane forgets a lane that never opened.
	DeleteLane(ctx context.Context, id int64) error
	// Driver names the backing database, for status.
	Driver() string
	Close() error
}
