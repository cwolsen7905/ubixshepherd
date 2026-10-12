package store

import "time"

// Topic states: a topic is open, or the person cleared it out of the thread.
const (
	TopicOpen    = "open"
	TopicCleared = "cleared"
)

// Topic sources: who made the topic.
const (
	TopicByDesk     = "desk"
	TopicByPerson   = "person"
	TopicByBackfill = "backfill"
)

// Topic link kinds: what can belong to a topic. A turn is a desk event's turn id.
const (
	TopicTurn     = "turn"
	TopicFeed     = "feed"
	TopicNextItem = "next_item"
	TopicLane     = "lane"
	TopicRun      = "run"
	TopicDecision = "decision"
	TopicRequest  = "request"
)

// TopicKinds are the kinds a link can have.
var TopicKinds = []string{TopicTurn, TopicFeed, TopicNextItem, TopicLane, TopicRun, TopicDecision, TopicRequest}

// Topic link authors, strongest last: a link the person set is never replaced by the
// desk or Shepherd, so a move sticks.
const (
	TopicLinkShepherd = "shepherd"
	TopicLinkDesk     = "desk"
	TopicLinkPerson   = "person"
)

// Topic names a matter being discussed. What belongs to it is in TopicLink; whether it
// waits on the person and when it was last active are derived at read time.
type Topic struct {
	ID      int64     `json:"id"`
	Title   string    `json:"title"`
	Status  string    `json:"status"`
	Summary string    `json:"summary,omitempty"`
	Source  string    `json:"source"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// ClearedAt is when the person cleared it; nil while open.
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
	// MergedInto is the topic this one was merged into; 0 unless merged. A merged
	// topic's id still resolves, to the one it became.
	MergedInto int64 `json:"merged_into,omitempty"`
}

// TopicLink ties one thing to a topic. (Kind, Ref) is unique: a thing belongs to one
// topic at a time, and moving it is an update.
type TopicLink struct {
	TopicID int64     `json:"topic_id"`
	Kind    string    `json:"kind"`
	Ref     string    `json:"ref"`
	By      string    `json:"by"`
	Created time.Time `json:"created"`
}

// TopicPatch is a change to a topic: nil fields are left alone.
type TopicPatch struct {
	Title   *string
	Summary *string
	// Status is TopicOpen or TopicCleared; ClearedAt is set to now on clearing.
	Status *string
}

// ValidTopicKind reports whether k is a link kind.
func ValidTopicKind(k string) bool {
	for _, v := range TopicKinds {
		if v == k {
			return true
		}
	}
	return false
}

// topicRank orders link authors: a stronger author replaces a weaker one's link, and a
// weaker one never replaces a stronger.
func topicRank(by string) int {
	switch by {
	case TopicLinkPerson:
		return 3
	case TopicLinkDesk:
		return 2
	}
	return 1
}

// TopicLinkWins reports whether a new link by `by` may replace an existing one by
// `existing`. Shepherd's link-based tags never reassign; the desk replaces its own and
// Shepherd's; the person replaces anything.
func TopicLinkWins(by, existing string) bool {
	if by == TopicLinkPerson {
		return true
	}
	if by == TopicLinkShepherd {
		return false
	}
	return topicRank(by) >= topicRank(existing) && existing != TopicLinkPerson
}
