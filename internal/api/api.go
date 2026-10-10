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

// Fold import, view, review and retire routes (POST).
const (
	PathFoldImport = "/v1/fold/import"
	PathFoldView   = "/v1/fold/view"
	PathFoldReview = "/v1/fold/review"
	PathFoldRetire = "/v1/fold/retire"
)

// FoldRetire is the body of POST /v1/fold/retire.
type FoldRetire struct {
	RepoID   int64  `json:"repo_id"`
	Worktree string `json:"worktree"`
}

// FoldImport is the body of POST /v1/fold/import.
type FoldImport struct {
	RepoID int64  `json:"repo_id"`
	File   string `json:"file,omitempty"`
	Apply  bool   `json:"apply"`
	// Adopt names branches whose worktrees become lanes though no row claims them.
	Adopt []string `json:"adopt,omitempty"`
}

// FoldView is the body of POST /v1/fold/view, and its answer.
type FoldView struct {
	RepoID int64  `json:"repo_id"`
	Write  bool   `json:"write"`
	View   string `json:"view,omitempty"`
	File   string `json:"file,omitempty"`
}

// Conversation routes.
const (
	PathSessions       = "/v1/sessions"
	PathSessionsImport = "/v1/sessions/import"
)

// PathSessionAsk is POST /v1/sessions/{id}/ask.
func PathSessionAsk(id string) string { return PathSessions + "/" + id + "/ask" }

// SessionImport is the body of POST /v1/sessions/import; RepoID 0 means every repo.
type SessionImport struct {
	RepoID int64 `json:"repo_id,omitempty"`
}

// SessionView is an adopted conversation with its repo's name and whether it looks open.
type SessionView struct {
	store.Conversation
	Repo  string `json:"repo,omitempty"`
	InUse bool   `json:"in_use"`
}

// Ask is the body of POST /v1/sessions/{id}/ask.
type Ask struct {
	Question string `json:"question"`
}

// Tag reservation routes.
const (
	PathTags        = "/v1/tags"
	PathTagsReserve = "/v1/tags/reserve"
	PathTagsRelease = "/v1/tags/release"
)

// Reserve is the body of POST /v1/tags/reserve.
type Reserve struct {
	RepoID int64  `json:"repo_id"`
	LaneID int64  `json:"lane_id,omitempty"`
	Bump   string `json:"bump"`
}

// ReleaseTag is the body of POST /v1/tags/release.
type ReleaseTag struct {
	RepoID int64  `json:"repo_id"`
	Tag    string `json:"tag"`
}

// ReservationView is a reservation with its lane's name.
type ReservationView struct {
	store.Reservation
	Lane string `json:"lane,omitempty"`
}

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

// PathUsage is GET /v1/usage?from=DAY&to=DAY (local days, "2006-01-02", inclusive; both
// default to today): the tokens and context sizes of the range's runs and desk turns, as
// store.UsageStats.
const PathUsage = "/v1/usage"

// PathFeed is GET /v1/feed?after=N (or after=latest for the newest id only).
const PathFeed = "/v1/feed"

// PathSettings holds client settings: GET and PUT /v1/settings/{key}.
const PathSettings = "/v1/settings"

// Feed answers GET /v1/feed: items after the given id, and the id to ask after next.
//
// Events is parallel to Items (same length, same order): Events[i] is the event of
// Items[i], one of the Event* values, mapped by EventKind from the item's kind. Draw
// glyphs and colours from the event; Items[i].Kind is the store's raw kind and may grow.
// Use Event(i) rather than indexing Events, so an older daemon's answer still works.
type Feed struct {
	Items  []store.FeedItem `json:"items"`
	Events []string         `json:"events"`
	Last   int64            `json:"last"`
}

// Event returns the event of item i, mapping its kind when the daemon sent no events.
func (f Feed) Event(i int) string {
	if i < len(f.Events) && f.Events[i] != "" {
		return f.Events[i]
	}
	return EventKind(f.Items[i].Kind)
}

// Setting is a setting's value. Configured, on a read, is what config.yaml says for a
// setting that overrides it (desk.model), used when Value is empty.
type Setting struct {
	Value      string `json:"value"`
	Configured string `json:"configured,omitempty"`
}

// PathRequests lists requests between lanes (GET).
const PathRequests = "/v1/requests"

func PathRunRequests(id int64) string  { return fmt.Sprintf("%s/%d/requests", PathRuns, id) }
func PathRequestRoute(id int64) string { return fmt.Sprintf("%s/%d/route", PathRequests, id) }
func PathRequestClose(id int64) string { return fmt.Sprintf("%s/%d/close", PathRequests, id) }

// CloseRequest is the body of POST /v1/requests/{id}/close.
type CloseRequest struct {
	Why string `json:"why,omitempty"`
}

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

// PathRunDiagnoses is GET /v1/runs/{id}/diagnoses: why the run, or the push of its work,
// went wrong, newest first, with each one's evidence and suggested fixes.
func PathRunDiagnoses(id int64) string { return fmt.Sprintf("%s/%d/diagnoses", PathRuns, id) }

// PathRunFix is POST /v1/runs/{id}/fix: take one of the run's suggested fixes.
func PathRunFix(id int64) string { return fmt.Sprintf("%s/%d/fix", PathRuns, id) }

// RunFix is the body of POST /v1/runs/{id}/fix. Action is a fix id from the run's
// diagnoses. Overrides replace the fix's arguments (the agent and model of a retry on
// another agent, an edited prompt) and are the person's alone: the front desk takes a
// fix as suggested.
type RunFix struct {
	Action    string            `json:"action"`
	Overrides map[string]string `json:"overrides,omitempty"`
}

// Diagnosis is a stored diagnosis as run and lane views carry it. Lists leave the
// evidence out to stay small (EvidenceOmitted says there is some); a single run's view
// and GET /v1/runs/{id}/diagnoses carry it.
type Diagnosis struct {
	store.Diagnosis
	EvidenceOmitted bool `json:"evidence_omitted,omitempty"`
}

// RunView is a run with its lane's and repo's names.
type RunView struct {
	store.Run
	Lane     string `json:"lane"`
	Repo     string `json:"repo"`
	Worktree string `json:"worktree"`
	// Usage is what the run's tokens and context came to, once it has ended and its
	// agent reported any; nil on a run still going and on runs from before it was kept.
	// Only a single run's view (GET /v1/runs/{id}) carries it.
	Usage *store.Usage `json:"usage,omitempty"`
	// Diagnosis is the run's newest diagnosis, when it has one; Progress what a run that
	// is still going is doing now.
	Diagnosis *Diagnosis `json:"diagnosis,omitempty"`
	Progress  *Progress  `json:"progress,omitempty"`
}

// Progress is a one-line account of what a running run is doing now, worked out from
// the tail of its log, its commits so far and how long it has run. Line is the whole
// phrase ("running make check (3 commits, 12m)"); Step is the part from the log, absent
// when the log shows nothing yet.
type Progress struct {
	Line    string `json:"line"`
	Step    string `json:"step,omitempty"`
	Commits int    `json:"commits"`
	Elapsed int64  `json:"elapsed_seconds"`
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
	// Detail is set only when asked for with StatusDetailQuery, and not for an agent's
	// token: every command probes status first, and most need none of it.
	Detail *StatusDetail `json:"detail,omitempty"`
}

// StatusDetailQuery is the query that asks GET /v1/status for Status.Detail.
const StatusDetailQuery = "detail=1"

// StatusDetail is what the web UI's daemon popover shows. It is read-only and holds no
// token, environment or path beyond the config's.
type StatusDetail struct {
	// Commit and BuiltAt are from the build's VCS stamp; empty when it has none (go
	// install ...@version, or a build outside a checkout).
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Host    string `json:"host,omitempty"`
	// Now is the daemon's clock, so a browser can show uptime without trusting its own.
	Now time.Time `json:"now"`
	// ConfigLoaded is when the config in force was read: at start or by the last
	// reload that took.
	ConfigLoaded time.Time `json:"config_loaded"`
	// ReloadError is why the last reload was refused, on one line; empty when the
	// last reload took (or there was none). ReloadFailedAt says when.
	ReloadError    string    `json:"reload_error,omitempty"`
	ReloadFailedAt time.Time `json:"reload_failed_at,omitzero"`
	// Running is agents running now, OpenLanes lanes in the open state, Decisions the
	// ones waiting on the person.
	Running   int `json:"running"`
	OpenLanes int `json:"open_lanes"`
	Decisions int `json:"decisions"`
	// QuotaHolds is the agents held for quota. Absent, not empty, when this daemon
	// does not report them.
	QuotaHolds *[]QuotaHold `json:"quota_holds,omitempty"`
	// SpendUSD is today's spend; BudgetUSD the daily budget, 0 for none.
	SpendUSD  float64 `json:"spend_usd"`
	BudgetUSD float64 `json:"budget_usd,omitempty"`
	// Forge is the forge watcher; absent when this daemon does not report it.
	Forge *ForgeWatch `json:"forge,omitempty"`
}

// QuotaHold is an agent held until its limit resets.
type QuotaHold struct {
	Agent string    `json:"agent"`
	Until time.Time `json:"until"`
}

// ForgeWatch is how the forge watcher last went: when it last polled without error, and
// the last error, on one line, if its latest poll failed.
type ForgeWatch struct {
	LastPoll time.Time `json:"last_poll,omitzero"`
	Error    string    `json:"error,omitempty"`
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

// LaneView is a lane with its repo's name, as lists show it, and what the forge last
// said about its branch.
//
// The forge fields are Shepherd's last known state, refreshed by the watcher; a forge
// that cannot be reached leaves them as they were. They are absent (not guessed) for a
// lane with no merge request, and pipeline fields are absent without a pipeline.
// MRState is one of the MRState* values, PipelineStatus one of the Pipeline* values.
// A badge such as "!34 · pipeline failed" reads mr and pipeline_status.
type LaneView struct {
	store.Lane
	Repo           string `json:"repo"`
	MR             int    `json:"mr,omitempty"`
	MRState        string `json:"mr_state,omitempty"`
	MRURL          string `json:"mr_url,omitempty"`
	Pipeline       int64  `json:"pipeline,omitempty"`
	PipelineStatus string `json:"pipeline_status,omitempty"`
	// Diagnosis is the newest diagnosis of the lane's latest run; it is not set for a
	// lane that has run nothing. Progress is what that run is doing now while it goes.
	Diagnosis *Diagnosis `json:"diagnosis,omitempty"`
	Progress  *Progress  `json:"progress,omitempty"`
}

// PathLaneShip is POST /v1/lanes/{id}/ship: push the lane's committed work and open or
// update its merge request, after Shepherd's own checks.
func PathLaneShip(id int64) string { return fmt.Sprintf("%s/%d/ship", PathLanes, id) }

// PathLaneScope is POST /v1/lanes/{id}/scope.
func PathLaneScope(id int64) string { return fmt.Sprintf("%s/%d/scope", PathLanes, id) }

// Rescope is the body of POST /v1/lanes/{id}/scope.
type Rescope struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// CloseLane is the body of POST /v1/lanes/{id}/close.
type CloseLane struct {
	Force bool `json:"force"`
}

// PrePush is the body of POST /v1/hook/pre-push.
type PrePush struct {
	// Path is the worktree git ran the hook in.
	Path string `json:"path"`
	// Remote is the hook's first argument: the remote's name, or its URL when the push
	// names none. An older hook leaves it out, and a push from a running agent's lane is
	// then refused.
	Remote string         `json:"remote,omitempty"`
	Refs   []fold.PushRef `json:"refs"`
}

// RepoHook is the body of POST /v1/repos/{id}/hook: install, uninstall or status.
type RepoHook struct {
	Action string `json:"action"`
}

// Error is the body of every non-2xx response.
type Error struct {
	Error string `json:"error"`
}
