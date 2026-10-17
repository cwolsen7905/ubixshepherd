package daemon

import (
	"context"
	"errors"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PLACEHOLDER (next-up #123, PLAN-008). This file holds only what the keep-going checks in
// keepgoing_checks_test.go need to compile. It changes no behaviour: nothing here is
// called by the daemon, and nothing starts work. It is untagged because KeepGoing holds a
// DriveSeams field. The drive lane replaces it, SinceYouLeft included.

// DriveStart is one run the drive asks for: the approved item, the lane to continue
// (the zero Lane means open a new one for the item) and the prompt built from the item.
type DriveStart struct {
	WorkspaceID int64
	Item        store.NextItem
	Lane        store.Lane
	Prompt      string
}

// QuestionLink is what a question or decision is linked to when it is created. All
// zero means unlinked, and an unlinked question blocks nothing (SHP-FR-KEEP-010).
type QuestionLink struct {
	ItemID, LaneID, TopicID int64
}

// DriveSeams are the hooks the drive uses to reach the rest of the daemon.
type DriveSeams struct {
	// StartRun opens the lane if needed, links it to the item and starts the run.
	StartRun func(ctx context.Context, req DriveStart) (store.Run, error)
	// LinkOf says what an open question is linked to.
	LinkOf func(q store.Question) QuestionLink
}

// Drive returns the controller's seams.
func (k *KeepGoing) Drive() *DriveSeams { return &k.drive }

// errDriveNotImplemented is what the stubs return.
var errDriveNotImplemented = errors.New("keep-going drive: not implemented")

// SinceYouLeft is a placeholder that always errors until the drive lands. It is the one summary for the person coming back: items started, runs
// finished, MRs opened or merged, what failed, and why and when the mode stopped.
func (k *KeepGoing) SinceYouLeft(ctx context.Context, ws int64) (string, error) {
	return "", errDriveNotImplemented
}
