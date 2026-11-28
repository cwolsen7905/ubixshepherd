package api

import "time"

// The cluster's routes (docs/cluster.md): a controller, and workers that link to it.
const (
	PathCluster = "/v1/cluster"
	// PathClusterLink is the worker's long-lived connection to the controller, a
	// WebSocket, opened with a link credential.
	PathClusterLink = "/v1/cluster/link"
	// PathClusterEnrol trades an enrolment code for a link credential. It takes no token:
	// the code is the proof.
	PathClusterEnrol = "/v1/cluster/enrol"
	// PathClusterWorkers adds a worker (POST); a worker's own routes sit beneath it:
	// /{name}/revoke, /{name}/remove, /{name}/confirm.
	PathClusterWorkers = "/v1/cluster/workers"
	// PathClusterMembersOld is the old name of PathClusterWorkers, still served, with the
	// same routes beneath it, for one release.
	PathClusterMembersOld = "/v1/cluster/members"
	// PathClusterJoin asks this daemon to enrol with a controller (POST).
	PathClusterJoin = "/v1/cluster/join"
)

// ClusterAPILevel is what this build tells a controller or worker about the link's own
// wire: it moves when a frame changes in a way an older peer cannot read.
const ClusterAPILevel = 1

// DaemonIDHeader carries a worker's daemon id on the request that opens its link, so the
// controller can refuse a changed id before it upgrades.
const DaemonIDHeader = "X-Shepherd-Daemon-Id"

// Roles in a Cluster answer.
const (
	RoleController = "controller"
	RoleWorker     = "worker"
	RoleAlone      = "alone"
)

// Link states on a worker.
const (
	LinkNotJoined  = "not joined"
	LinkConnecting = "connecting"
	LinkConnected  = "connected"
	LinkRefused    = "refused"
	LinkWaiting    = "waiting to retry"
)

// Cluster is GET /v1/cluster: what this daemon is in a cluster. A controller lists its
// workers; a worker says how its link to the controller is.
type Cluster struct {
	Role     string `json:"role"`
	DaemonID string `json:"daemon_id"`
	// Workers are a controller's workers, by name.
	Workers []ClusterWorker `json:"workers,omitempty"`
	// Link is a worker's link to its controller; nil on a controller or a daemon on its
	// own.
	Link *ClusterLink `json:"link,omitempty"`
}

// ClusterWorker is one worker as a controller sees it.
type ClusterWorker struct {
	Name     string `json:"name"`
	DaemonID string `json:"daemon_id,omitempty"`
	// Enrolled says the worker holds a credential; one that does not is waiting for its
	// code to be used (CodeExpires) or has been revoked.
	Enrolled bool `json:"enrolled"`
	Online   bool `json:"online"`
	// Since is when the worker came online, or, offline, when it was last seen; zero if
	// it never linked.
	Since        time.Time `json:"since,omitempty"`
	Version      string    `json:"version,omitempty"`
	APILevel     int       `json:"api_level,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	// RunsGoing and Waiting are the worker's last report; zero while it is offline.
	RunsGoing int `json:"runs_going"`
	Waiting   int `json:"waiting"`
	// Tenant is the slug of the tenant the worker's credential belongs to.
	Tenant string `json:"tenant,omitempty"`
	// Repos and Agents are what the worker reported over its link: its repos by origin
	// and the agent CLIs it has. They are live, kept with the link and empty while it is
	// offline, and empty for a worker without the CapRepos capability, which predates the
	// report.
	Repos  []LinkRepo `json:"repos,omitempty"`
	Agents []string   `json:"agents,omitempty"`
	// ReposTruncated is how many more repos the worker has than its report could carry.
	ReposTruncated int `json:"repos_truncated,omitempty"`
	// PendingID is a daemon id the worker presented that differs from the one it enrolled
	// with: the link is refused until the person confirms it.
	PendingID   string    `json:"pending_id,omitempty"`
	CodeExpires time.Time `json:"code_expires,omitempty"`
}

// ClusterLink is a worker's link to its controller.
type ClusterLink struct {
	Controller string `json:"controller,omitempty"`
	State      string `json:"state"`
	// Since is when the state began.
	Since time.Time `json:"since,omitempty"`
	// Error is why the last try failed, or the controller refused.
	Error string `json:"error,omitempty"`
	// NextTry is when the worker will dial again, while it waits to.
	NextTry           time.Time `json:"next_try,omitempty"`
	ControllerID      string    `json:"controller_daemon_id,omitempty"`
	ControllerAPI     int       `json:"controller_api_level,omitempty"`
	ControllerVersion string    `json:"controller_version,omitempty"`
	// Tenant is the tenant the controller puts this worker in, from its hello; empty
	// for a controller from before tenants were named on the link.
	Tenant string `json:"tenant,omitempty"`
}

// WorkerAdd is POST /v1/cluster/workers.
type WorkerAdd struct {
	Name string `json:"name"`
}

// WorkerCode is the one-time enrolment code a controller shows once.
type WorkerCode struct {
	Name    string    `json:"name"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

// ClusterEnrol is POST /v1/cluster/enrol, from the worker's daemon to the controller.
type ClusterEnrol struct {
	Code     string `json:"code"`
	DaemonID string `json:"daemon_id"`
}

// ClusterEnrolled is the controller's answer: the worker's name and its link credential,
// shown once. It crosses between daemons that may run different versions, so the
// controller's id goes under both its name and its old one, hub_daemon_id, and a worker
// reads either (ID).
type ClusterEnrolled struct {
	Name         string `json:"name"`
	Credential   string `json:"credential"`
	ControllerID string `json:"controller_daemon_id,omitempty"`
	// HubID is ControllerID under its old name, for a worker from before the rename.
	HubID string `json:"hub_daemon_id,omitempty"`
}

// NewClusterEnrolled is the answer a controller sends, with its id under both names.
func NewClusterEnrolled(name, credential, controllerID string) ClusterEnrolled {
	return ClusterEnrolled{Name: name, Credential: credential, ControllerID: controllerID, HubID: controllerID}
}

// ID is the controller's daemon id, from whichever name the controller sent it under.
func (e ClusterEnrolled) ID() string {
	if e.ControllerID != "" {
		return e.ControllerID
	}
	return e.HubID
}

// ClusterJoin is POST /v1/cluster/join, to the worker's own daemon.
type ClusterJoin struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

// ClusterJoined says which worker this daemon now is.
type ClusterJoined struct {
	Name       string `json:"name"`
	Controller string `json:"controller"`
}

// LinkFrame is one message on the link, a JSON text message. The worker opens with a
// hello and the controller answers with its own; once the worker accepts that hello it
// sends its report in a repos frame and a status, then a status at each ping and a repos
// frame whenever its repos or agents change. No frame names a
// role, so a worker and a controller from either side of the hub and member rename read
// each other's frames.
//
// Each side ignores a frame type and a field it does not know, so a newer peer adds both
// without moving ClusterAPILevel: an older controller drops a repos frame and the
// fields below, and an older worker sends neither and says so by not naming CapRepos.
type LinkFrame struct {
	Type         string   `json:"type"`
	DaemonID     string   `json:"daemon_id,omitempty"`
	Version      string   `json:"version,omitempty"`
	APILevel     int      `json:"api_level,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	RunsGoing    int      `json:"runs_going,omitempty"`
	Waiting      int      `json:"waiting,omitempty"`
	// Tenant, on the controller's hello, is the slug of the tenant the worker's
	// credential belongs to.
	Tenant string `json:"tenant,omitempty"`
	// Repos and Agents, on a worker's repos frame, are its whole report: each frame
	// replaces the last one, and one with neither says it has none. ReposTruncated is how
	// many repos were left out to keep the frame under the link's read limit
	// (MaxReportRepos, MaxReportBytes).
	Repos          []LinkRepo `json:"repos,omitempty"`
	Agents         []string   `json:"agents,omitempty"`
	ReposTruncated int        `json:"repos_truncated,omitempty"`
}

// LinkReadLimit is the largest frame either side reads; a bigger one closes the link.
const LinkReadLimit = 64 << 10

// A worker's report is capped so its repos frame always fits LinkReadLimit, whatever the
// peer's version: at most MaxReportRepos repos, and at most MaxReportBytes of them as
// JSON. The rest are counted in ReposTruncated.
const (
	MaxReportRepos = 500
	MaxReportBytes = 48 << 10
)

// LinkRepo is one repo a worker has: its origin, normalised so the SSH and HTTPS forms
// of one repo are equal (host/path, lowercase), and the worker's own name for it.
type LinkRepo struct {
	Origin string `json:"origin"`
	Name   string `json:"name"`
}

// Link frame types.
const (
	FrameHello  = "hello"
	FrameStatus = "status"
	FrameRepos  = "repos"
)

// Link capabilities, named in a hello.
const (
	// CapStatus is the status frame's counts.
	CapStatus = "status"
	// CapRepos says a worker reports its repos and agents, in repos frames.
	CapRepos = "repos"
)
