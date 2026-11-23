package api

import "time"

// The cluster's routes (docs/cluster.md): a hub, and members that link to it.
const (
	PathCluster = "/v1/cluster"
	// PathClusterLink is the member's long-lived connection to the hub, a WebSocket, opened
	// with a link credential.
	PathClusterLink = "/v1/cluster/link"
	// PathClusterEnrol trades an enrolment code for a link credential. It takes no token:
	// the code is the proof.
	PathClusterEnrol = "/v1/cluster/enrol"
	// PathClusterMembers adds a member (POST); a member's own routes sit beneath it:
	// /{name}/revoke, /{name}/remove, /{name}/confirm.
	PathClusterMembers = "/v1/cluster/members"
	// PathClusterJoin asks this daemon to enrol with a hub (POST).
	PathClusterJoin = "/v1/cluster/join"
)

// ClusterAPILevel is what this build tells a hub or member about the link's own wire:
// it moves when a frame changes in a way an older peer cannot read.
const ClusterAPILevel = 1

// DaemonIDHeader carries a member's daemon id on the request that opens its link, so the
// hub can refuse a changed id before it upgrades.
const DaemonIDHeader = "X-Shepherd-Daemon-Id"

// Roles in a Cluster answer.
const (
	RoleHub    = "hub"
	RoleMember = "member"
	RoleAlone  = "alone"
)

// Link states on a member.
const (
	LinkNotJoined  = "not joined"
	LinkConnecting = "connecting"
	LinkConnected  = "connected"
	LinkRefused    = "refused"
	LinkWaiting    = "waiting to retry"
)

// Cluster is GET /v1/cluster: what this daemon is in a cluster. A hub lists its members;
// a member says how its link to the hub is.
type Cluster struct {
	Role     string `json:"role"`
	DaemonID string `json:"daemon_id"`
	// Members are a hub's members, by name.
	Members []ClusterMember `json:"members,omitempty"`
	// Link is a member's link to its hub; nil on a hub or a daemon on its own.
	Link *ClusterLink `json:"link,omitempty"`
}

// ClusterMember is one member as a hub sees it.
type ClusterMember struct {
	Name     string `json:"name"`
	DaemonID string `json:"daemon_id,omitempty"`
	// Enrolled says the member holds a credential; one that does not is waiting for its
	// code to be used (CodeExpires) or has been revoked.
	Enrolled bool `json:"enrolled"`
	Online   bool `json:"online"`
	// Since is when the member came online, or, offline, when it was last seen; zero if
	// it never linked.
	Since        time.Time `json:"since,omitempty"`
	Version      string    `json:"version,omitempty"`
	APILevel     int       `json:"api_level,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	// RunsGoing and Waiting are the member's last report; zero while it is offline.
	RunsGoing int `json:"runs_going"`
	Waiting   int `json:"waiting"`
	// PendingID is a daemon id the member presented that differs from the one it enrolled
	// with: the link is refused until the person confirms it.
	PendingID   string    `json:"pending_id,omitempty"`
	CodeExpires time.Time `json:"code_expires,omitempty"`
}

// ClusterLink is a member's link to its hub.
type ClusterLink struct {
	Hub   string `json:"hub,omitempty"`
	State string `json:"state"`
	// Since is when the state began.
	Since time.Time `json:"since,omitempty"`
	// Error is why the last try failed, or the hub refused.
	Error string `json:"error,omitempty"`
	// NextTry is when the member will dial again, while it waits to.
	NextTry    time.Time `json:"next_try,omitempty"`
	HubID      string    `json:"hub_daemon_id,omitempty"`
	HubAPI     int       `json:"hub_api_level,omitempty"`
	HubVersion string    `json:"hub_version,omitempty"`
}

// MemberAdd is POST /v1/cluster/members.
type MemberAdd struct {
	Name string `json:"name"`
}

// MemberCode is the one-time enrolment code a hub shows once.
type MemberCode struct {
	Name    string    `json:"name"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

// ClusterEnrol is POST /v1/cluster/enrol, from the member's daemon to the hub.
type ClusterEnrol struct {
	Code     string `json:"code"`
	DaemonID string `json:"daemon_id"`
}

// ClusterEnrolled is the hub's answer: the member's name and its link credential, shown
// once.
type ClusterEnrolled struct {
	Name       string `json:"name"`
	Credential string `json:"credential"`
	HubID      string `json:"hub_daemon_id"`
}

// ClusterJoin is POST /v1/cluster/join, to the member's own daemon.
type ClusterJoin struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

// ClusterJoined says which member this daemon now is.
type ClusterJoined struct {
	Name string `json:"name"`
	Hub  string `json:"hub"`
}

// LinkFrame is one message on the link, a JSON text message. The member opens with a
// hello and the hub answers with its own; after that the member sends a status at each
// ping. Phase 1 carries nothing else.
type LinkFrame struct {
	Type         string   `json:"type"`
	DaemonID     string   `json:"daemon_id,omitempty"`
	Version      string   `json:"version,omitempty"`
	APILevel     int      `json:"api_level,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	RunsGoing    int      `json:"runs_going,omitempty"`
	Waiting      int      `json:"waiting,omitempty"`
}

// Link frame types.
const (
	FrameHello  = "hello"
	FrameStatus = "status"
)
