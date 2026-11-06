package desk

import (
	"context"
	"strconv"

	"github.com/ubixsys/ubixshepherd/internal/desklog"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// logTurn keeps what a turn started with for the desk log (docs/desk-log.md): the topic
// it was filed under, the events it carried with the repo, lane and run each was about,
// and the Waiting-on-you count the desk was shown. It reads only, adds nothing to what
// the desk sees, and a failure is logged and goes no further.
func (d *Desk) logTurn(ctx context.Context, t turn) {
	o := d.m.o
	l := store.DeskTurnLog{WorkspaceID: d.ws.ID, Turn: t.id, StartedBy: desklog.StartedBy(t.origin, t.text), Waiting: desklog.WaitingCount(t.text)}
	if link, err := o.Store.TopicOf(ctx, store.TopicTurn, strconv.FormatInt(t.id, 10)); err == nil {
		if topic, err := o.Store.Topic(ctx, link.TopicID); err == nil {
			l.Topic = redact.String(topic.Title)
		}
	}
	for _, p := range t.events {
		e := store.DeskLogEvent{Kind: p.kind}
		e.Repo, e.Lane, e.Run = d.eventSubject(ctx, p.kind, p.ref)
		l.Events = append(l.Events, e)
	}
	if err := o.Store.SaveDeskTurnLog(ctx, l); err != nil {
		o.Log.Warn("desk: keep the turn's log", "workspace", d.ws.Name, "turn", t.id, "err", err)
	}
}

// eventSubject names the repo, lane and run a feed item is about. Its ref is a run, a
// decision or a lane depending on the kind; what cannot be found is left empty.
func (d *Desk) eventSubject(ctx context.Context, kind string, ref int64) (repo, lane string, run int64) {
	st := d.m.o.Store
	var laneID int64
	switch kind {
	case store.FeedDecision, store.FeedDecisionAnswer:
		dec, err := st.Decision(ctx, ref)
		if err != nil {
			return "", "", 0
		}
		ref = dec.RunID
		fallthrough
	case store.FeedRunStarted, store.FeedRunEnded, store.FeedRunPassed, store.FeedRunFailed,
		store.FeedRunInterrupted, store.FeedRunQuota, store.FeedCommit, store.FeedReport:
		r, err := st.Run(ctx, ref)
		if err != nil {
			return "", "", 0
		}
		run, laneID = r.ID, r.LaneID
	case store.FeedGate, store.FeedMR, store.FeedPipeline, store.FeedLaneOpened, store.FeedLaneClosed:
		laneID = ref
	default:
		return "", "", 0
	}
	l, err := st.Lane(ctx, laneID)
	if err != nil {
		return "", "", run
	}
	if r, err := st.Repo(ctx, l.RepoID); err == nil {
		repo = r.Name
	}
	return repo, l.Name, run
}
