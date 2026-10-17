package store

import (
	"path"
	"strings"
	"time"
)

// Artifact kinds: what a file is, by its extension.
const (
	ArtifactMarkdown = "markdown"
	ArtifactText     = "text"
	ArtifactCode     = "code"
	ArtifactImage    = "image"
	ArtifactOther    = "other"
)

// Artifact sources: how Shepherd came to know the file.
const (
	ArtifactDeclared   = "declared"   // the agent said so, with the artifact tool
	ArtifactDiscovered = "discovered" // a run's end saw it in the worktree
	ArtifactCommitted  = "committed"  // it is in the lane's committed range
)

// Artifact intents: what is to become of the file. The person's choice outranks the
// agent's, and the agent's outranks a default.
const (
	ArtifactCommit    = "commit"
	ArtifactKeep      = "keep"
	ArtifactScratch   = "scratch"
	ArtifactUndecided = "undecided"
)

// Artifact states.
const (
	ArtifactLive      = "live" // in the worktree
	ArtifactKept      = "kept"
	ArtifactInRepo    = "committed"
	ArtifactDiscarded = "discarded"
	ArtifactGone      = "gone"
)

// Artifact link kinds: what an artifact is tied to besides its lane and run.
const (
	ArtifactLinkDecision = "decision"
	ArtifactLinkRequest  = "request"
	ArtifactLinkReport   = "report"
)

// Who chose an artifact's intent (Artifact.IntentBy), weakest first.
const (
	IntentByDefault = "default"
	IntentByAgent   = "agent"
	IntentByPerson  = "person"
)

// ArtifactIntents are the intents an agent may declare.
var ArtifactIntents = []string{ArtifactCommit, ArtifactKeep, ArtifactScratch}

// ArtifactDir is where a file not meant for the repo goes, in the worktree.
const ArtifactDir = ".shepherd/artifacts/"

// Artifact is a file in a lane that the person may want to read, whatever its git
// state. Path is relative to the worktree, with slashes.
type Artifact struct {
	ID          int64 `json:"id"`
	WorkspaceID int64 `json:"workspace_id"`
	RepoID      int64 `json:"repo_id"`
	LaneID      int64 `json:"lane_id"`
	// RunID is the run that first produced it.
	RunID  int64  `json:"run_id"`
	Path   string `json:"path"`
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Intent string `json:"intent"`
	// IntentBy says who chose the intent: a default, the agent, or the person.
	IntentBy string     `json:"intent_by"`
	State    string     `json:"state"`
	SHA256   string     `json:"sha256,omitempty"`
	Size     int64      `json:"size"`
	Pinned   bool       `json:"pinned,omitempty"`
	Created  time.Time  `json:"created"`
	Updated  time.Time  `json:"updated"`
	Closed   *time.Time `json:"closed,omitempty"`
	// Versions and Links are filled by Artifact, not by ListArtifacts.
	Versions []ArtifactVersion `json:"versions,omitempty"`
	Links    []ArtifactLink    `json:"links,omitempty"`
}

// ArtifactVersion is one distinct content of an artifact, with the run that produced it.
// RunID is 0 for a change seen with no run (the person edited it by hand).
type ArtifactVersion struct {
	ArtifactID int64     `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	RunID      int64     `json:"run_id"`
	Seen       time.Time `json:"seen"`
	// Snapshotted is when a copy was stored; nil until snapshots exist.
	Snapshotted *time.Time `json:"snapshotted,omitempty"`
}

// ArtifactLink ties an artifact to a decision, request or report. Ref is its id.
type ArtifactLink struct {
	ArtifactID int64     `json:"artifact_id"`
	Kind       string    `json:"kind"`
	Ref        string    `json:"ref"`
	Created    time.Time `json:"created"`
}

// ArtifactFilter narrows ListArtifacts; a zero field matches everything. TopicID
// matches an artifact linked to the topic, or with no link of its own whose lane is.
type ArtifactFilter struct {
	WorkspaceID int64
	LaneID      int64
	RunID       int64
	TopicID     int64
	DecisionID  int64
	State       string
	Kind        string
	Since       time.Time
	// Limit caps the answer; 0 means 200.
	Limit int
}

// ArtifactKindOf is the kind of a file by its extension.
func ArtifactKindOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return ArtifactMarkdown
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return ArtifactImage
	case ".txt", ".log", ".csv", ".json", ".yaml", ".yml", ".toml", ".html", ".htm":
		return ArtifactText
	case ".go", ".ts", ".tsx", ".js", ".py", ".sh", ".rs", ".c", ".h", ".java", ".sql":
		return ArtifactCode
	}
	return ArtifactOther
}
