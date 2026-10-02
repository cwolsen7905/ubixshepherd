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
	ID       int64     `json:"id"`
	RepoID   int64     `json:"repo_id"`
	Name     string    `json:"name"`
	Branch   string    `json:"branch"`
	Worktree string    `json:"worktree"`
	State    string    `json:"state"`
	Created  time.Time `json:"created"`
}

// Store is Shepherd's state.
type Store interface {
	// SaveWorkspace creates the workspace at ws.Path, or renames the one already there,
	// and adds or updates each repo by path. Repos not listed are left as they are.
	SaveWorkspace(ctx context.Context, ws Workspace, repos []Repo) (Workspace, error)
	Workspaces(ctx context.Context) ([]Workspace, error)
	Repos(ctx context.Context, workspaceID int64) ([]Repo, error)
	Lanes(ctx context.Context, repoID int64) ([]Lane, error)
	// Driver names the backing database, for status.
	Driver() string
	Close() error
}
