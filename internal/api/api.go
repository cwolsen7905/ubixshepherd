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

// ClientHeader names the kind of client making a request (cli, mcp, hook), for logs.
const ClientHeader = "X-Shepherd-Client"

// Routes.
const (
	PathStatus     = "/v1/status"
	PathWorkspaces = "/v1/workspaces"
	PathResolve    = "/v1/resolve"
	PathShutdown   = "/v1/shutdown"
	PathLanes      = "/v1/lanes"
	PathFoldGC     = "/v1/fold/gc"
	PathPrePush    = "/v1/hook/pre-push"
	PathRuns       = "/v1/runs"
)

// PathSpend is GET (today's spend) and POST (record a front desk turn's cost).
const PathSpend = "/v1/spend"

// SpendToday answers GET /v1/spend.
type SpendToday struct {
	Day string `json:"day"`
	// USD is today's total in dollars, Copilot's credits priced at CreditUSD.
	USD       float64                `json:"usd"`
	Budget    float64                `json:"budget"`
	CreditUSD float64                `json:"credit_usd"`
	BySource  map[string]store.Spend `json:"by_source"`
}

// PathFeed is GET /v1/feed?after=N (or after=latest for the newest id only).
const PathFeed = "/v1/feed"

// PathSettings holds client settings: GET and PUT /v1/settings/{key}.
const PathSettings = "/v1/settings"

// Feed answers GET /v1/feed: items after the given id, and the id to ask after next.
type Feed struct {
	Items []store.FeedItem `json:"items"`
	Last  int64            `json:"last"`
}

// Setting is a setting's value.
type Setting struct {
	Value string `json:"value"`
}

// PathRequests lists requests between lanes (GET).
const PathRequests = "/v1/requests"

func PathRunRequests(id int64) string  { return fmt.Sprintf("%s/%d/requests", PathRuns, id) }
func PathRequestRoute(id int64) string { return fmt.Sprintf("%s/%d/route", PathRequests, id) }

// Route is the body of POST /v1/requests/{id}/route.
type Route struct {
	Lane  string `json:"lane"`
	Agent string `json:"agent,omitempty"`
}

// RequestView is a request with where it came from.
type RequestView struct {
	store.Request
	FromAgent string `json:"from_agent"`
	FromLane  string `json:"from_lane"`
	Repo      string `json:"repo"`
}

// PathDecisions lists decisions (GET).
const PathDecisions = "/v1/decisions"

func PathDecisionAnswer(id int64) string { return fmt.Sprintf("%s/%d/answer", PathDecisions, id) }
func PathRunEvents(id int64) string      { return fmt.Sprintf("%s/%d/events", PathRuns, id) }
func PathRunDecisions(id int64) string   { return fmt.Sprintf("%s/%d/decisions", PathRuns, id) }

// Answer is the body of POST /v1/decisions/{id}/answer.
type Answer struct {
	Answer string `json:"answer"`
}

// DecisionView is a decision with where it came from.
type DecisionView struct {
	store.Decision
	Agent string `json:"agent"`
	Lane  string `json:"lane"`
	Repo  string `json:"repo"`
}

// RunEvents answers GET /v1/runs/{id}/events.
type RunEvents struct {
	Events    []store.Event    `json:"events"`
	Decisions []store.Decision `json:"decisions"`
}

func PathRun(id int64) string     { return fmt.Sprintf("%s/%d", PathRuns, id) }
func PathRunLog(id int64) string  { return fmt.Sprintf("%s/%d/log", PathRuns, id) }
func PathRunStop(id int64) string { return fmt.Sprintf("%s/%d/stop", PathRuns, id) }

// RunView is a run with its lane's and repo's names.
type RunView struct {
	store.Run
	Lane     string `json:"lane"`
	Repo     string `json:"repo"`
	Worktree string `json:"worktree"`
}

// RunLog answers GET /v1/runs/{id}/log?offset=N: the next piece of the log.
type RunLog struct {
	Data   string `json:"data"`
	Offset int64  `json:"offset"`
	// Done: the run has ended and Data reaches the end of its log.
	Done bool `json:"done"`
}

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
