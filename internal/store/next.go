package store

import (
	"strconv"
	"strings"
	"time"
)

// Next-up item states. Only the first four are chosen; NextDone is stored when the
// person marks an item done by hand or when its lane's merge was seen, and NextStarted is
// never stored: it is derived from the link to an open lane (Settle).
const (
	NextPlanned  = "planned"  // proposed, not yet approved
	NextApproved = "approved" // the person has said do it
	NextDeferred = "deferred" // acknowledged, on purpose not now
	NextDropped  = "dropped"  // will not be done; kept so it is not re-proposed
	NextDone     = "done"     // finished: marked by hand, or its lane's merge request merged
	NextStarted  = "started"  // derived: linked to an open lane
)

// Next item sources: who put an item on the list.
const (
	NextByPerson = "person"
	// NextByDesk is an item the desk put up for the person's approval: it waits on them.
	NextByDesk = "desk"
	// NextByChat is work the desk registered from chat on the person's agreement:
	// planned, and not waiting on them.
	NextByChat  = "chat"
	NextByAgent = "agent"
)

// NextStates are the states an item can be stored in, in list order.
var NextStates = []string{NextApproved, NextPlanned, NextDeferred, NextDropped, NextDone}

// NextItem is work that is planned but may have no lane yet. The daemon owns the list
// like lanes and decisions; State is what was chosen, and Settle derives what is shown.
type NextItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Repo is the repo's name in the workspace, when the item is for one.
	Repo string `json:"repo,omitempty"`
	// Plan is a path under the repo to the doc that plans the item; Lane is the name a
	// lane opened for it is expected to have, a hint when LaneID is not set.
	Plan string `json:"plan,omitempty"`
	Lane string `json:"lane,omitempty"`
	// LaneID is the link: the lane opened for the item. 0 when not linked.
	LaneID int64 `json:"lane_id,omitempty"`
	// MR is the merge request, a number or URL, when known before a lane's own is.
	MR      string    `json:"mr,omitempty"`
	State   string    `json:"state"`
	Rank    int       `json:"rank"`
	Source  string    `json:"source"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// DoneAt is when the item was finished; nil until then.
	DoneAt *time.Time `json:"done_at,omitempty"`
	// DoneWhen is one line, the acceptance test for the item.
	DoneWhen string `json:"done_when,omitempty"`
	// DependsOn are the ids of the items this one needs finished first. Whether it is
	// blocked is derived (Blocked), never stored.
	DependsOn []int64 `json:"depends_on,omitempty"`
	// Suggestion is a one-line "done?" or "done or drop?" that something noticed and
	// only the person acts on; Suggested is when it was made. Checked is when the weekly
	// re-check last ran for the item.
	Suggestion string    `json:"suggestion,omitempty"`
	Suggested  time.Time `json:"suggested,omitzero"`
	Checked    time.Time `json:"checked,omitzero"`
}

// NextPatch is a change to an item: nil fields are left alone.
type NextPatch struct {
	Title  *string
	Repo   *string
	Plan   *string
	Lane   *string
	LaneID *int64
	MR     *string
	State  *string
	Note   *string
	DoneAt *time.Time
	// DoneWhen and DependsOn are set as given (empty clears). Suggestion is set as given
	// and stamps Suggested when it is not empty; an empty one clears both. Checked
	// records a weekly re-check.
	DoneWhen   *string
	DependsOn  *[]int64
	Suggestion *string
	Checked    *time.Time
}

// FormatDeps is the stored form of a dependency list: ids, comma separated.
func FormatDeps(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// ParseDeps reads the stored form back; anything that is not a positive id is skipped.
func ParseDeps(s string) []int64 {
	var out []int64
	for _, f := range strings.Split(s, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// Blocked returns the dependencies of it whose state is not done, in the order they were
// named, given the effective state of every item by id (an id missing from the map is
// unknown and counts as unfinished). A dropped dependency is not done: it keeps the item
// blocked, so the person decides whether to drop or re-plan it too.
func Blocked(it NextItem, state map[int64]string) []int64 {
	var out []int64
	for _, d := range it.DependsOn {
		if state[d] != NextDone {
			out = append(out, d)
		}
	}
	return out
}

// DependencyCycle reports whether giving item id the dependencies deps closes a cycle
// among the stored lists in all (id to its dependencies): that is, whether id is
// reachable from any of deps, or is one of them.
func DependencyCycle(id int64, deps []int64, all map[int64][]int64) bool {
	seen := map[int64]bool{}
	var reach func(n int64) bool
	reach = func(n int64) bool {
		if n == id {
			return true
		}
		if seen[n] {
			return false
		}
		seen[n] = true
		for _, d := range all[n] {
			if reach(d) {
				return true
			}
		}
		return false
	}
	for _, d := range deps {
		if reach(d) {
			return true
		}
	}
	return false
}

// Settled is an item's effective state: what the person chose, or what the link to its
// lane says. No second status is kept, so it cannot go stale.
type Settled struct {
	// State is one of the Next* states.
	State string
	// Why explains a lane that no longer shows the item as started: "" when there is
	// nothing to explain.
	Why string
	// Merged is set when the item's lane closed on a merged request: the item is done.
	Merged bool
}

// Settle derives an item's state from the lane it is linked to (nil when it is not
// linked) and the forge's last word on that lane. A merged, closed lane makes the item
// done; an open lane makes it started, unless it was dropped or already done; a lane
// closed without a merge returns the item to its stored state and says so.
func Settle(it NextItem, lane *Lane, lf LaneForge) Settled {
	s := Settled{State: it.State}
	if it.State == NextDone || it.State == NextDropped || lane == nil {
		return s
	}
	switch lane.State {
	case LaneOpen, LaneOpening:
		s.State = NextStarted
	case LaneClosed:
		if strings.EqualFold(lf.MRState, "merged") {
			s.State, s.Merged = NextDone, true
		} else {
			s.Why = "lane " + lane.Name + " closed without a merge"
		}
	}
	return s
}
