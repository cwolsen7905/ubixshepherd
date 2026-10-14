package chat

import (
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/desk"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestDeskBriefDescribesKeepGoing(t *testing.T) {
	for _, want := range []string{"keep_going", "never covers", "Shepherd refuses a start past its limits", desk.BriefNoNewWork} {
		if !strings.Contains(DeskBrief, want) {
			t.Errorf("DeskBrief lacks %q", want)
		}
	}
}

// /keep-going goes to the daemon's endpoint as the person's own act, and the event text
// the desk gets follows the flag the panel last read.
func TestKeepGoingCommandAndEventText(t *testing.T) {
	m, d, a := newTestModel()
	typeLine(t, m, "/keep-going on")
	if len(a.keepSets) != 1 || !a.keepSets[0].On || a.keepSets[0].WorkspaceID != 1 {
		t.Fatalf("sets = %+v", a.keepSets)
	}
	if !has(m.Lines(), KindInfo, "Keep-going is on") {
		t.Errorf("thread = %+v", m.Lines())
	}
	drive(t, m, m.pollFeed())
	a.feed = []store.FeedItem{{ID: 1, Kind: store.FeedRunEnded, Text: "run 3 ended"}}
	drive(t, m, m.pollFeed())
	if len(d.got) != 1 || !strings.Contains(d.got[0], desk.KeepGoingOn) || strings.Contains(d.got[0], desk.NoNewWork) {
		t.Fatalf("event text with keep-going on = %q", d.got)
	}

	typeLine(t, m, "/keep-going off")
	if len(a.keepSets) != 2 || a.keepSets[1].On || !has(m.Lines(), KindInfo, "Keep-going is off") {
		t.Fatalf("off: sets %+v thread %+v", a.keepSets, m.Lines())
	}
	a.feed = append(a.feed, store.FeedItem{ID: 2, Kind: store.FeedRunEnded, Text: "run 4 ended"})
	drive(t, m, m.pollFeed())
	if len(d.got) != 2 || !strings.Contains(d.got[1], desk.NoNewWork) || strings.Contains(d.got[1], desk.KeepGoingOn) {
		t.Errorf("event text with keep-going off = %q", d.got)
	}

	typeLine(t, m, "/keep-going maybe")
	if !has(m.Lines(), KindError, "usage: /keep-going") || len(a.keepSets) != 2 {
		t.Errorf("a bad argument went through: %+v", a.keepSets)
	}
}
