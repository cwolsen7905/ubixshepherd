package api

import (
	"fmt"
	"time"
)

// PathTopics is the list of conversation topics (docs/topics.md): GET lists the open
// ones (all=1 adds cleared), POST makes one.
const PathTopics = "/v1/topics"

// PathTopic reads (GET) or changes (PATCH) one topic.
func PathTopic(id int64) string { return fmt.Sprintf("%s/%d", PathTopics, id) }

// TopicRef names a thing a topic can hold: a kind (turn, feed, next_item, lane, run,
// decision, request) and its id.
type TopicRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// TopicAdd is the body of POST /v1/topics. Link, when set, ties a thing to the new
// topic. The daemon refuses a title that repeats an open topic's, with that id.
type TopicAdd struct {
	Title   string    `json:"title"`
	Summary string    `json:"summary,omitempty"`
	Link    *TopicRef `json:"link,omitempty"`
}

// TopicUpdate is the body of PATCH /v1/topics/{id}: nil fields are left alone. Title,
// Status, MergeInto and moving a thing that already has a topic are the person's: the
// daemon refuses them from the front desk outside a turn the person started. Summary
// and linking an unlinked thing are the desk's too.
type TopicUpdate struct {
	Title   *string `json:"title,omitempty"`
	Summary *string `json:"summary,omitempty"`
	// Status is open (reopen) or cleared.
	Status *string `json:"status,omitempty"`
	// MergeInto merges this topic into another; this id keeps resolving to it.
	MergeInto int64 `json:"merge_into,omitempty"`
	// Link ties a thing to this topic. A thing that already has another topic moves
	// only on the person's word.
	Link *TopicRef `json:"link,omitempty"`
}

// TopicLinkView is one thing in a topic, with its own state borrowed, never copied.
type TopicLinkView struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	By   string `json:"by"`
	// Label names the thing (a lane's name, an item's title, a decision's question) and
	// State is its own state; Waits says it needs the person.
	Label string    `json:"label,omitempty"`
	State string    `json:"state,omitempty"`
	Waits bool      `json:"waits,omitempty"`
	Time  time.Time `json:"time"`
}

// TopicView is a topic as the person and the desk see it. Waits, Activity, Settled and
// Reopen are derived at read time, so they cannot go stale.
type TopicView struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Summary   string     `json:"summary,omitempty"`
	Source    string     `json:"source"`
	Created   time.Time  `json:"created"`
	Updated   time.Time  `json:"updated"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
	// Activity is the newest of the topic's own and its links' times.
	Activity time.Time `json:"activity"`
	// Waits counts what needs the person: open decisions, requests needing routing and
	// proposed next-up items.
	Waits int `json:"waits"`
	// Settled marks an open topic whose lanes are closed, items done or dropped and
	// nothing waits: one click clears it.
	Settled bool `json:"settled,omitempty"`
	// Reopen marks a cleared topic where something now needs the person.
	Reopen bool            `json:"reopen,omitempty"`
	Links  []TopicLinkView `json:"links"`
}

// TopicList answers GET /v1/topics: most attention first, then most recent activity.
type TopicList struct {
	Topics []TopicView `json:"topics"`
}
