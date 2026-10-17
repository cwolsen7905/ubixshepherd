package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// STUB (next-up #123, PLAN-008). This file holds only what the keep-going checks in
// keepgoing_checks_test.go need to compile. It changes no behaviour: nothing here is
// called by the daemon, and nothing starts work. The drive lane replaces it.

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

var (
	driveMu    sync.Mutex
	driveSeams = map[*KeepGoing]*DriveSeams{}
)

// Drive returns the controller's seams, created empty on first use.
func (k *KeepGoing) Drive() *DriveSeams {
	driveMu.Lock()
	defer driveMu.Unlock()
	if driveSeams[k] == nil {
		driveSeams[k] = &DriveSeams{}
	}
	return driveSeams[k]
}

// errDriveNotImplemented is what the stubs return.
var errDriveNotImplemented = errors.New("keep-going drive: not implemented")

// SinceYouLeft is the one summary for the person coming back: items started, runs
// finished, MRs opened or merged, what failed, and why and when the mode stopped.
func (k *KeepGoing) SinceYouLeft(ctx context.Context, ws int64) (string, error) {
	return "", errDriveNotImplemented
}
