package api

import (
	"fmt"
	"net/url"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PathProjects names a project's brief routes.
const PathProjects = "/v1/projects"

// PathProjectBrief reads a project's brief (GET): the approved text with its age, and
// the open draft.
func PathProjectBrief(project string) string {
	return fmt.Sprintf("%s/%s/brief", PathProjects, url.PathEscape(project))
}

// PathProjectBriefDraft records a draft brief (POST).
func PathProjectBriefDraft(project string) string { return PathProjectBrief(project) + "/draft" }

// PathProjectBriefApprove approves the open draft (POST). It is the person's alone.
func PathProjectBriefApprove(project string) string { return PathProjectBrief(project) + "/approve" }

// DraftBrief is the body of POST /v1/projects/{name}/brief/draft. The text is redacted
// before it is stored.
type DraftBrief struct {
	Text string `json:"text"`
}

// ApproveBrief is the body of POST /v1/projects/{name}/brief/approve. Words are the
// person's own: their approval, typed by them. The daemon refuses an approval from an
// agent's token, and from the front desk outside a turn the person started, as it does
// an answer to a decision.
type ApproveBrief struct {
	// ID is the draft being approved; it must still be the project's open draft.
	ID    int64  `json:"id"`
	Words string `json:"words"`
}

// BriefView is a project's brief as the person and the front desk see it. Only
// Approved is ever given to an agent; Pending never is.
type BriefView struct {
	Project string `json:"project"`
	// Approved is the brief agents are given; nil when none has been approved.
	Approved *store.ProjectBrief `json:"approved,omitempty"`
	// Age is the time since approval, in nanoseconds as a Go duration; AgeText is the
	// same as agents read it ("approved 41 days ago").
	Age     time.Duration `json:"age,omitempty"`
	AgeText string        `json:"age_text,omitempty"`
	// Stale is set when the approved brief is older than MaxAge (brief_max_age).
	Stale  bool          `json:"stale,omitempty"`
	MaxAge time.Duration `json:"max_age"`
	// Pending is the open draft waiting for the person; nil when none.
	Pending *store.ProjectBrief `json:"pending,omitempty"`
	// Caches are what agents in the project are told about its build caches.
	Caches []BriefCache `json:"caches,omitempty"`
}

// BriefCache is a declared build cache as agents are told of it.
type BriefCache struct {
	Path   string `json:"path"`
	Repo   string `json:"repo"`
	Recipe string `json:"recipe"`
	Note   string `json:"note,omitempty"`
}

// PathProjectsUnassigned is GET /v1/unassigned: the same view as a project's, for the
// repos that are in no project. It is not under PathProjects so that no project name can
// clash with it.
const PathProjectsUnassigned = "/v1/unassigned"

// PathProject reads one project (GET): its summary, repos, open lanes, recent runs,
// pending decisions and requests, and brief.
func PathProject(project string) string {
	return fmt.Sprintf("%s/%s", PathProjects, url.PathEscape(project))
}

// ProjectBudgetView is a project's spend limit and what its period has spent. A project
// with no budget of its own shows the daemon's, as the dispatcher applies it.
type ProjectBudgetView struct {
	// Amount is dollars per period; 0 is no cap.
	Amount float64 `json:"amount"`
	// Cap is soft or hard.
	Cap string `json:"cap"`
	// Period is day or month; Since is its first day, a local day.
	Period string `json:"period"`
	Since  string `json:"since"`
	// Spent is dollars this period, credits priced at credit_usd.
	Spent float64 `json:"spent"`
}

// ProjectSummary is what the project list and the board's groups show. A repo is in at
// most one project: Unassigned is the group of the repos that are in none.
type ProjectSummary struct {
	// Name is the project's name; empty on the Unassigned group.
	Name       string `json:"name"`
	Unassigned bool   `json:"unassigned,omitempty"`
	// Repos are the members' names, as in the configuration. Unassigned lists the repos
	// of the workspace that no project names.
	Repos []string `json:"repos"`
	// Lanes counts the open lanes; Running those with a run going.
	Lanes   int `json:"lanes"`
	Running int `json:"running"`
	// Decisions counts the open decisions of the project's lanes, and Requests the
	// requests of its lanes, or addressed to it, that are still unanswered.
	Decisions int `json:"decisions"`
	Requests  int `json:"requests"`
	// Waiting is what needs the person: open decisions and requests that need routing.
	Waiting int `json:"waiting"`
	// SpendToday is today's dollars, credits priced at credit_usd.
	SpendToday float64 `json:"spend_today"`
	// Budget is the project's own limit and period; nil on Unassigned, which has none.
	Budget *ProjectBudgetView `json:"budget,omitempty"`
}

// ProjectList answers GET /v1/projects: the configured projects by name, then
// Unassigned when some repo is in none.
type ProjectList struct {
	Projects []ProjectSummary `json:"projects"`
}

// ProjectDetail answers GET /v1/projects/{name} and GET /v1/unassigned.
type ProjectDetail struct {
	ProjectSummary
	// OpenLanes are the project's open lanes with their merge request and pipeline.
	OpenLanes []LaneView `json:"open_lanes"`
	// Runs are the project's most recent runs, newest first.
	Runs []RunView `json:"runs"`
	// PendingDecisions and PendingRequests are those of the lanes above, oldest first.
	PendingDecisions []DecisionView `json:"pending_decisions"`
	PendingRequests  []RequestView  `json:"pending_requests"`
	// Brief is nil on Unassigned, which has none.
	Brief *BriefView `json:"brief,omitempty"`
}
