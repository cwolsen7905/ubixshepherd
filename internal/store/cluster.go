package store

import (
	"context"
	"time"
)

// Member is a daemon enrolled on a hub: a laptop or other machine that links in
// (docs/cluster.md). The hub knows it by Name, and by the daemon id the member
// presented when it enrolled.
type Member struct {
	Name string
	// DaemonID is the id the member enrolled with; "" until it has.
	DaemonID string
	// PendingID is a different id the member presented at a link since: it is refused
	// until the person confirms it.
	PendingID string
	// Enrolled says the member holds a credential.
	Enrolled bool
	// CodeUntil is when an unspent enrolment code ends; zero when there is none.
	CodeUntil    time.Time
	Version      string
	APILevel     int
	Capabilities []string
	Created      time.Time
	// LastSeen is when the member's link last ended or was last heard from.
	LastSeen time.Time
}

// MemberStore keeps the hub's members and their link credentials. It is a separate
// interface from Store, which other stores need not implement.
type MemberStore interface {
	// AddMember creates a member with a one-time enrolment code (kept as codeHash). A
	// name that exists and holds a credential is an ErrConflict; one that does not
	// has its code replaced.
	AddMember(ctx context.Context, name, codeHash string, until time.Time) error
	Members(ctx context.Context) ([]Member, error)
	// Enrol spends the code: it records the member's daemon id and a credential
	// (credHash) in one step, and returns the member's name. An unknown, spent or
	// expired code is an ErrNotFound.
	Enrol(ctx context.Context, codeHash, daemonID, credID, credHash string, now time.Time) (string, error)
	// CredentialMember finds the member a credential belongs to; ErrNotFound if it is
	// unknown or revoked.
	CredentialMember(ctx context.Context, credHash string) (name string, err error)
	// Handshake records what a member reported at its link.
	Handshake(ctx context.Context, name, version string, apiLevel int, caps []string, seen time.Time) error
	// Seen records when a member's link was last up.
	Seen(ctx context.Context, name string, seen time.Time) error
	// SetPendingID holds a changed daemon id until it is confirmed.
	SetPendingID(ctx context.Context, name, id string) error
	// ConfirmID accepts the pending id as the member's own; ErrNotFound when none waits.
	ConfirmID(ctx context.Context, name string) error
	// RevokeMember deletes the member's credentials; the member stays listed.
	RevokeMember(ctx context.Context, name string) error
	// RemoveMember deletes the member and its credentials.
	RemoveMember(ctx context.Context, name string) error
}
