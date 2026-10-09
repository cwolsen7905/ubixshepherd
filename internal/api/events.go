package api

import "github.com/ubixsys/ubixshepherd/internal/store"

// Event kinds: the closed set a client maps to one glyph and colour each. A feed
// item's raw kind (the store's) may grow; its event never leaves this set.
const (
	EventLaneOpened       = "lane_opened"
	EventLaneClosed       = "lane_closed"
	EventRunStarted       = "run_started"
	EventRunEnded         = "run_ended"
	EventReport           = "report"         // an agent's typed progress report
	EventDecisionAsked    = "decision_asked" // waits for the person
	EventDecisionAnswer   = "decision_answered"
	EventRequest          = "request"           // between lanes, moving normally
	EventRequestAttention = "request_attention" // stuck or failed: the person or front desk must act
	EventMR               = "mr"                // a merge request opened, updated, merged or closed
	EventPipeline         = "pipeline"          // the gate or the forge's pipeline: pass, fail or hand-back
	EventBudget           = "budget"
	EventTag              = "tag"
	EventRelease          = "release"
	EventConfig           = "config"
	EventInfo             = "info" // anything else, such as a session note
)

// eventKinds is the one place a store feed kind becomes an event.
var eventKinds = map[string]string{
	store.FeedLaneOpened:     EventLaneOpened,
	store.FeedLaneClosed:     EventLaneClosed,
	store.FeedRunStarted:     EventRunStarted,
	store.FeedRunEnded:       EventRunEnded,
	store.FeedReport:         EventReport,
	store.FeedDecision:       EventDecisionAsked,
	store.FeedDecisionAnswer: EventDecisionAnswer,
	store.FeedRequest:        EventRequest,
	store.FeedRequestRouted:  EventRequest,
	store.FeedRequestReplied: EventRequest,
	store.FeedRequestStuck:   EventRequestAttention,
	store.FeedRequestFailed:  EventRequestAttention,
	store.FeedMR:             EventMR,
	store.FeedPipeline:       EventPipeline,
	store.FeedBudget:         EventBudget,
	store.FeedTag:            EventTag,
	store.FeedRelease:        EventRelease,
	"config":                 EventConfig, // the daemon's own kind (daemon.FeedConfig)
}

// EventKind maps a store feed kind to its event; any kind not listed is EventInfo.
func EventKind(feedKind string) string {
	if e, ok := eventKinds[feedKind]; ok {
		return e
	}
	return EventInfo
}
