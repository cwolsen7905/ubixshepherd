package api

import (
	"fmt"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// PathNext is the next-up list: GET lists it (all=1 adds dropped and done items), POST
// adds an item.
const PathNext = "/v1/next"

// PathNextItem reads (GET) or changes (PATCH) one item.
func PathNextItem(id int64) string { return fmt.Sprintf("%s/%d", PathNext, id) }

// NextAdd is the body of POST /v1/next. State is planned unless the person says
// approved; the daemon refuses an approval from the front desk outside a turn the
// person started, as it does an answer to a decision.
type NextAdd struct {
	Title string `json:"title"`
	Repo  string `json:"repo,omitempty"`
	// Plan is a path under the repo to the doc that plans the item.
	Plan string `json:"plan,omitempty"`
	// Lane is the name a lane opened for the item is expected to have.
	Lane  string `json:"lane,omitempty"`
	Note  string `json:"note,omitempty"`
	State string `json:"state,omitempty"`
	// Propose, from the front desk, puts the item up for the person's approval: it
	// waits in "Waiting on you". Without it a desk item is work registered from chat.
	Propose bool `json:"propose,omitempty"`
}

// NextUpdate is the body of PATCH /v1/next/{id}: nil fields and an empty Move are left
// alone. State and Move are the person's: the daemon refuses them from the front desk
// outside a turn the person started.
type NextUpdate struct {
	Title *string `json:"title,omitempty"`
	Repo  *string `json:"repo,omitempty"`
	Plan  *string `json:"plan,omitempty"`
	Lane  *string `json:"lane,omitempty"`
	MR    *string `json:"mr,omitempty"`
	Note  *string `json:"note,omitempty"`
	// State is planned, approved, deferred, dropped or done. started is derived and
	// cannot be set.
	State *string `json:"state,omitempty"`
	// LaneID links the item to a lane; 0 unlinks it.
	LaneID *int64 `json:"lane_id,omitempty"`
	// Move is top, up or down within the item's state.
	Move string `json:"move,omitempty"`
}

// NextSourceMatrix is the Source of a row read through from a repo's work matrix.
const NextSourceMatrix = "matrix"

// NextItemView is an item as the person and the desk see it: what they chose, and what
// its lane makes of it. The lane's own state is borrowed, never copied into a status.
type NextItemView struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Repo  string `json:"repo,omitempty"`
	Plan  string `json:"plan,omitempty"`
	Lane  string `json:"lane,omitempty"`
	// State is the effective state: started and done are derived from the lane. Stored
	// is what was chosen.
	State  string `json:"state"`
	Stored string `json:"stored_state"`
	Rank   int    `json:"rank"`
	Source string `json:"source"`
	// Proposed marks an item an agent or the desk added that the person has not approved.
	Proposed bool   `json:"proposed,omitempty"`
	Note     string `json:"note,omitempty"`
	// LaneID, LaneName and LaneState describe the linked lane; MR, MRURL and MRState its
	// merge request as the forge last said, or the MR recorded on the item.
	LaneID    int64  `json:"lane_id,omitempty"`
	LaneName  string `json:"lane_name,omitempty"`
	LaneState string `json:"lane_state,omitempty"`
	LaneRepo  string `json:"lane_repo,omitempty"`
	MR        string `json:"mr,omitempty"`
	MRURL     string `json:"mr_url,omitempty"`
	MRState   string `json:"mr_state,omitempty"`
	// Running is set when the linked lane has an agent running.
	Running bool `json:"running,omitempty"`
	// Why says why an item with a lane is not started: its lane closed without a merge.
	Why     string     `json:"why,omitempty"`
	Created time.Time  `json:"created"`
	Updated time.Time  `json:"updated"`
	DoneAt  *time.Time `json:"done_at,omitempty"`
	// Key, ReadOnly, Area, RowID and PRURL are set on a work-matrix row (Source is
	// NextSourceMatrix): read through from the repo's docs/WORK.md, id 0, never changed here.
	Key      string `json:"key,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Area     string `json:"area,omitempty"`
	RowID    string `json:"row_id,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`
}

// NextList answers GET /v1/next: the items in list order (approved, planned, deferred,
// then dropped and done when asked for).
type NextList struct {
	Items []NextItemView `json:"items"`
	// NextUp is the id of the first approved item that has no lane, 0 when none.
	NextUp int64 `json:"next_up,omitempty"`
	// Matrix is the repos' work-matrix rows with status todo or blocked, read through and
	// not stored, after the items. A row a stored item already names by lane is left out.
	// ?matrix=0 omits them.
	Matrix []NextItemView `json:"matrix,omitempty"`
}

// OpenLaneBody is the body of POST /v1/lanes: the lane to open, and the next-up item it
// is for, if any. Item links the two; without it the daemon links an item only when
// exactly one open item in the repo names the lane.
type OpenLaneBody struct {
	fold.OpenRequest
	Item int64 `json:"item,omitempty"`
}

// AwaitingApproval is the items that wait on the person: proposed by the desk or an
// agent, still planned, and not yet started from a lane. The dock, the web header and
// the desk's summary count the same set.
func AwaitingApproval(items []NextItemView) []NextItemView {
	var out []NextItemView
	for _, it := range items {
		if it.Proposed && it.State == store.NextPlanned {
			out = append(out, it)
		}
	}
	return out
}
