package api

import (
	"fmt"
	"net/url"

	"github.com/ubixsys/ubixshepherd/internal/work"
)

// PathUnassignedWork is GET /v1/unassigned/work: the same view as a project's work, for
// the repos that are in no project.
const PathUnassignedWork = PathProjectsUnassigned + "/work"

// PathProjectWork reads a project's work matrices (GET): the rows of each repo's
// docs/WORK.md as of its base branch's last fetch, with warnings and drift.
func PathProjectWork(project string) string {
	return fmt.Sprintf("%s/%s/work", PathProjects, url.PathEscape(project))
}

// Drift kinds.
const (
	// DriftLaneUntracked: an open lane that no live row names.
	DriftLaneUntracked = "lane_untracked"
	// DriftDoingLaneClosed: a doing row whose lane is closed.
	DriftDoingLaneClosed = "doing_lane_closed"
	// DriftDoingLaneUnknown: a doing row with no lane, or a lane the repo does not have.
	DriftDoingLaneUnknown = "doing_lane_unknown"
	// DriftNoWorkFile: the repo has no docs/WORK.md on its base branch. The page shows it
	// as a quiet hint, not a fault.
	DriftNoWorkFile = "no_work_file"
)

// WorkRow is a matrix row with its links.
type WorkRow struct {
	work.Row
	// Repo is the repo the row came from.
	Repo string `json:"repo"`
	// LaneID is the id of the row's lane when the repo has one by that name (open or
	// closed), so the page can link to it.
	LaneID int64 `json:"lane_id,omitempty"`
	// PRURL is the forge's page for the PR or MR; empty when the remote is unknown.
	PRURL string `json:"pr_url,omitempty"`
}

// RepoWork is one repo's matrix.
type RepoWork struct {
	Repo string `json:"repo"`
	// Found is false when the base ref or docs/WORK.md is not there.
	Found bool `json:"found"`
	// Ref is the ref it was read from ("origin/dev"); Blob is the file's hash there.
	Ref      string         `json:"ref,omitempty"`
	Blob     string         `json:"blob,omitempty"`
	Rows     []WorkRow      `json:"rows"`
	Warnings []work.Warning `json:"warnings"`
}

// WorkDrift is a place where the matrix and the lanes disagree.
type WorkDrift struct {
	Kind    string `json:"kind"`
	Repo    string `json:"repo"`
	Lane    string `json:"lane,omitempty"`
	LaneID  int64  `json:"lane_id,omitempty"`
	RowID   string `json:"row_id,omitempty"`
	Message string `json:"message"`
}

// ProjectWork is GET /v1/projects/{name}/work. Project is empty for the Unassigned group.
type ProjectWork struct {
	Project string      `json:"project"`
	Repos   []RepoWork  `json:"repos"`
	Drift   []WorkDrift `json:"drift"`
}
