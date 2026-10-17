package api

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Artifact routes. The lane-level discovery read is
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

// PathArtifactContent is GET /v1/artifacts/{id}/content: the bytes of a kept or live
// artifact. version is a sha-256 of a recorded version; "" is the newest.
func PathArtifactContent(id int64, version string) string {
	p := PathArtifact(id) + "/content"
	if version != "" {
		p += "?version=" + url.QueryEscape(version)
	}
	return p
}

// PathArtifactDiscard is POST /v1/artifacts/{id}/discard: the person's own call.
func PathArtifactDiscard(id int64) string { return PathArtifact(id) + "/discard" }

// PathArtifactGC is POST /v1/artifacts/gc: apply retention now.
const PathArtifactGC = PathArtifacts + "/gc"

// ArtifactPatch is the body of PATCH /v1/artifacts/{id}: the person's changes. Move
// "keep" moves an untracked file into the Shepherd directory.
type ArtifactPatch struct {
	Intent *string `json:"intent,omitempty"`
	Pinned *bool   `json:"pinned,omitempty"`
	Title  *string `json:"title,omitempty"`
	Move   string  `json:"move,omitempty"`
}

// ArtifactGC is the body of POST /v1/artifacts/gc; zero fields take the configured
// retention (artifacts.keep_days and artifacts.cap_mb).
type ArtifactGC struct {
	KeepDays int   `json:"keep_days,omitempty"`
	CapBytes int64 `json:"cap_bytes,omitempty"`
	DryRun   bool  `json:"dry_run,omitempty"`
}

// AddItems puts items that are derived outside the collector into the list, keeps it
// oldest first and seals it. Artifact items are counted.
func (w *Waiting) AddItems(items []WaitingItem) {
	for _, it := range items {
		if it.Kind == WaitingArtifact {
			w.Counts.Artifacts++
		}
		w.Items = append(w.Items, it)
	}
	sort.SliceStable(w.Items, func(i, j int) bool { return w.Items[i].Since.Before(w.Items[j].Since) })
	w.Seal()
}
