package store

import "time"

// CheckFile is a test file the person approved as "this is what done means": a path on a
// repo's base branch, pinned to the blob and commit that were approved. While it is live
// no lane may change it. Released rows stay as history.
type CheckFile struct {
	ID     int64 `json:"id"`
	RepoID int64 `json:"repo_id"`
	// Path is repo-relative with "/" separators.
	Path string `json:"path"`
	// Blob is the git blob id approved; BaseCommit the commit on the base branch it was
	// read at.
	Blob       string `json:"blob"`
	BaseCommit string `json:"base_commit"`
	// ItemID is the next-up item the check belongs to (0: none); Set labels a group.
	ItemID int64  `json:"item_id,omitempty"`
	Set    string `json:"set,omitempty"`
	// Approved is when, ApprovedBy who: "person" and the surface (cli, web, desk).
	Approved   time.Time `json:"approved"`
	ApprovedBy string    `json:"approved_by"`
	// Released is when the mark was lifted; nil while it stands.
	Released   *time.Time `json:"released,omitempty"`
	ReleaseWhy string     `json:"release_why,omitempty"`
}

// Live reports whether the file is still marked.
func (c CheckFile) Live() bool { return c.Released == nil }
