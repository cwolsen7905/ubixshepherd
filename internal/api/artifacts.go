package api

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Artifact routes (docs/design/artifacts.md §6). The lane-level discovery read is
// PathLaneArtifacts; these are the stored list and the declaration.
const PathArtifacts = "/v1/artifacts"

// PathArtifact is GET /v1/artifacts/{id}: one artifact with its versions and links.
func PathArtifact(id int64) string { return PathArtifacts + "/" + strconv.FormatInt(id, 10) }

// PathRunArtifacts is POST /v1/runs/{id}/artifacts: an agent declares a file of its
// lane. The body is an ArtifactDeclare and the answer an ArtifactDeclared. Only the run
// itself may call it.
func PathRunArtifacts(id int64) string { return fmt.Sprintf("%s/%d/artifacts", PathRuns, id) }

// ArtifactDeclare is the body of POST /v1/runs/{id}/artifacts. Path is relative to
// the worktree (an absolute path inside it is accepted). Intent is commit, keep or
// scratch; empty leaves it to the default for the path.
type ArtifactDeclare struct {
	Path   string `json:"path"`
	Title  string `json:"title,omitempty"`
	Intent string `json:"intent,omitempty"`
}

// ArtifactDeclared answers a declaration. New says Shepherd had not known the file;
// Version says the content was new to it.
type ArtifactDeclared struct {
	Artifact store.Artifact `json:"artifact"`
	New      bool           `json:"new"`
	Version  bool           `json:"new_version"`
}

// ArtifactListQuery is the query string of GET /v1/artifacts for f.
func ArtifactListQuery(f store.ArtifactFilter) string {
	q := url.Values{}
	set := func(k string, v int64) {
		if v != 0 {
			q.Set(k, strconv.FormatInt(v, 10))
		}
	}
	set("workspace_id", f.WorkspaceID)
	set("lane_id", f.LaneID)
	set("run_id", f.RunID)
	set("topic_id", f.TopicID)
	set("decision_id", f.DecisionID)
	set("limit", int64(f.Limit))
	if f.State != "" {
		q.Set("state", f.State)
	}
	if f.Kind != "" {
		q.Set("kind", f.Kind)
	}
	if !f.Since.IsZero() {
		q.Set("since", f.Since.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if len(q) == 0 {
		return PathArtifacts
	}
	return PathArtifacts + "?" + q.Encode()
}
