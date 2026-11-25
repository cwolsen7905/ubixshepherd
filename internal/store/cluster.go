package store

import (
	"context"
	"time"
)

// Worker is a daemon enrolled on a controller: a laptop or other machine that links in
// (docs/cluster.md). The controller knows it by Name, and by the daemon id the worker
// presented when it enrolled.
type Worker struct {
	Name string
	// DaemonID is the id the worker enrolled with; "" until it has.
	DaemonID string
	// PendingID is a different id the worker presented at a link since: it is refused
	// until the person confirms it.
	PendingID string
	// Enrolled says the worker holds a credential.
	Enrolled bool
	// CodeUntil is when an unspent enrolment code ends; zero when there is none.
	CodeUntil    time.Time
	Version      string
	APILevel     int
	Capabilities []string
	Created      time.Time
	// LastSeen is when the worker's link last ended or was last heard from.
	LastSeen time.Time
}

// WorkerStore keeps the controller's workers and their link credentials. It is a
// separate interface from Store, which other stores need not implement.
type WorkerStore interface {
	// AddWorker creates a worker with a one-time enrolment code (kept as codeHash). A
	// name that exists and holds a credential is an ErrConflict; one that does not
	// has its code replaced.
	AddWorker(ctx context.Context, name, codeHash string, until time.Time) error
	Workers(ctx context.Context) ([]Worker, error)
	// Enrol spends the code: it records the worker's daemon id and a credential
	// (credHash) in one step, and returns the worker's name. An unknown, spent or
	// expired code is an ErrNotFound.
	Enrol(ctx context.Context, codeHash, daemonID, credID, credHash string, now time.Time) (string, error)
	// CredentialWorker finds the worker a credential belongs to; ErrNotFound if it is
	// unknown or revoked.
	CredentialWorker(ctx context.Context, credHash string) (name string, err error)
	// Handshake records what a worker reported at its link.
	Handshake(ctx context.Context, name, version string, apiLevel int, caps []string, seen time.Time) error
	// Seen records when a worker's link was last up.
	Seen(ctx context.Context, name string, seen time.Time) error
	// SetPendingID holds a changed daemon id until it is confirmed.
	SetPendingID(ctx context.Context, name, id string) error
	// ConfirmID accepts the pending id as the worker's own; ErrNotFound when none waits.
	ConfirmID(ctx context.Context, name string) error
	// RevokeWorker deletes the worker's credentials; the worker stays listed.
	RevokeWorker(ctx context.Context, name string) error
	// RemoveWorker deletes the worker and its credentials.
	RemoveWorker(ctx context.Context, name string) error
}
