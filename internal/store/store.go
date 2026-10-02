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

// Run states.
const (
	RunRunning     = "running"
	RunSucceeded   = "succeeded"
	RunFailed      = "failed"
	RunStopped     = "stopped"
	RunInterrupted = "interrupted" // the daemon stopped while it ran
)

// Run is one agent started in one lane, and what came of it.
type Run struct {
	ID     int64  `json:"id"`
	LaneID int64  `json:"lane_id"`
	Agent  string `json:"agent"`
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt"`
	State  string `json:"state"`
	PID    int    `json:"pid,omitempty"`
	// Log is the path of the run's output, redacted.
	Log      string `json:"log"`
	StartSHA string `json:"start_sha"`
	EndSHA   string `json:"end_sha,omitempty"`
	// Commits made during the run, and any files they changed outside the lane's scope.
	Commits  int        `json:"commits"`
	Outside  []string   `json:"outside,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Ended    *time.Time `json:"ended,omitempty"`
	// Session is the agent's own conversation id; a run that continues another shares
	// it, and Parent is the run it follows.
	Session string `json:"session,omitempty"`
	Parent  int64  `json:"parent,omitempty"`
}

// Event is something an agent told Shepherd during a run.
type Event struct {
	ID    int64  `json:"id"`
	RunID int64  `json:"run_id"`
	Kind  string `json:"kind"` // report
	// Status is a report's: progress, done or blocked.
	Status  string    `json:"status,omitempty"`
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

// Decision states.
const (
	DecisionOpen     = "open"
	DecisionAnswered = "answered"
)

// Decision is a question an agent holds for a person.
type Decision struct {
	ID             int64    `json:"id"`
	RunID          int64    `json:"run_id"`
	Question       string   `json:"question"`
	Options        []string `json:"options,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	// Why says why the agent judged it the person's call.
	Why    string `json:"why,omitempty"`
	State  string `json:"state"`
	Answer string `json:"answer,omitempty"`
	// AnswerRun is the run that carried the answer back into the agent's session.
	AnswerRun int64      `json:"answer_run,omitempty"`
	Created   time.Time  `json:"created"`
	Answered  *time.Time `json:"answered,omitempty"`
}

// Request states.
const (
	// RequestPending: waiting for the asker to end its turn and the target lane to be free.
	RequestPending = "pending"
	// RequestNeedsRouting: Shepherd cannot route it by rule; the front desk or the
	// person must say which lane and agent.
	RequestNeedsRouting = "needs_routing"
	// RequestRouted: the target agent is working on it.
	RequestRouted = "routed"
	// RequestReplyReady: answered, waiting for the asker's lane to be free.
	RequestReplyReady = "reply_ready"
	// RequestReplied: the reply went back into the asker's conversation.
	RequestReplied = "replied"
	RequestFailed  = "failed"
)

// Request is one agent asking, through Shepherd, for something from another lane.
type Request struct {
	ID      int64  `json:"id"`
	FromRun int64  `json:"from_run"`
	Kind    string `json:"kind"` // question, handoff, review
	// Lane is the target lane's name, once known.
	Lane    string `json:"lane,omitempty"`
	Message string `json:"message"`
	State   string `json:"state"`
	// Agent is the target agent, once chosen.
	Agent     string `json:"agent,omitempty"`
	TargetRun int64  `json:"target_run,omitempty"`
	Reply     string `json:"reply,omitempty"`
	ReplyRun  int64  `json:"reply_run,omitempty"`
	// Depth counts the requests in a chain: an agent answering one may ask in turn.
	Depth   int       `json:"depth"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
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

	CreateRun(ctx context.Context, r Run) (Run, error)
	UpdateRun(ctx context.Context, r Run) error
	Run(ctx context.Context, id int64) (Run, error)
	// Runs returns the newest first; laneID 0 means every lane, state "" every state.
	Runs(ctx context.Context, laneID int64, state string, limit int) ([]Run, error)

	AddEvent(ctx context.Context, e Event) (Event, error)
	Events(ctx context.Context, runID int64) ([]Event, error)
	CreateDecision(ctx context.Context, d Decision) (Decision, error)
	Decision(ctx context.Context, id int64) (Decision, error)
	// Decisions returns decisions in a state ("" for all), oldest first.
	Decisions(ctx context.Context, state string) ([]Decision, error)
	// AnswerDecision records the answer; ErrConflict if the decision is not open.
	AnswerDecision(ctx context.Context, id int64, answer string) (Decision, error)
	SetDecisionRun(ctx context.Context, id, runID int64) error

	CreateRequest(ctx context.Context, q Request) (Request, error)
	UpdateRequest(ctx context.Context, q Request) error
	Request(ctx context.Context, id int64) (Request, error)
	// Requests returns requests in any of the states (all when none), oldest first.
	Requests(ctx context.Context, states ...string) ([]Request, error)
	// Driver names the backing database, for status.
	Driver() string
	Close() error
}
