package desk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestDigestEndsWithTheWaitingLine(t *testing.T) {
	line := "[Waiting on you (2)] decision 4 (app:a): ask; MR !9 (app:b): approve it"
	got := digest([]string{"run 3 ended"}, 0, line, false)
	if !strings.Contains(got, "\n"+line+"\n") || !strings.HasSuffix(got, "repeating this line, so the person sees what waits on them.") {
		t.Errorf("digest: %q", got)
	}
	if none := digest([]string{"run 3 ended"}, 0, "", false); strings.Contains(none, "Waiting on you") {
		t.Errorf("digest with nothing waiting: %q", none)
	}
}

func TestBriefTracksQuestions(t *testing.T) {
	for _, want := range []string{"question_open", "question_answer", "plainly covers it", "never mark one answered on a guess"} {
		if !strings.Contains(Brief, want) {
			t.Errorf("Brief lacks %q", want)
		}
	}
}

// The "do not start new work" text is one function of the flag: the digest, the terminal
// chat and the briefs all take it from here.
func TestEventInstructionFollowsKeepGoing(t *testing.T) {
	off, on := EventInstruction(false), EventInstruction(true)
	if !strings.Contains(off, NoNewWork) || strings.Contains(off, "Keep-going is on") {
		t.Errorf("off text = %q", off)
	}
	if !strings.Contains(on, KeepGoingOn) || strings.Contains(on, NoNewWork) {
		t.Errorf("on text = %q", on)
	}
	for _, want := range []string{"unless a decision needs the person", "do not work around them"} {
		if !strings.Contains(on, want) {
			t.Errorf("on text lacks %q", want)
		}
	}
	if d := digest([]string{"run 3 ended"}, 0, "", true); !strings.Contains(d, KeepGoingOn) || strings.Contains(d, NoNewWork) {
		t.Errorf("digest with keep-going on = %q", d)
	}
	if d := digest([]string{"run 3 ended"}, 0, "", false); !strings.Contains(d, NoNewWork) {
		t.Errorf("digest with keep-going off = %q", d)
	}
	for _, want := range []string{"keep_going", "never covers", "answering an agent's decision", "Shepherd refuses a start past its limits"} {
		if !strings.Contains(Brief, want) {
			t.Errorf("Brief lacks %q", want)
		}
	}
}

// The desk's wake-up digest reads the flag from the store: on says keep going, and the
// event turn still comes from the system, never the person.
func TestWakeDigestReadsKeepGoing(t *testing.T) {
	for _, on := range []bool{false, true} {
		r := newRig(t, func(o *Options) { o.Grace = time.Millisecond })
		if on {
			if err := r.st.SetKeepGoing(context.Background(), store.KeepGoing{WorkspaceID: r.ws.ID, On: true, Since: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
		}
		d := r.desk(t)
		r.m.Wake([]store.FeedItem{feedItem(store.FeedDecision, "decision 2 waits")})
		sub := d.Subscribe()
		es := r.waitEnded(t, 1)
		d.Unsubscribe(sub)
		text := es[0].Text
		if on != strings.Contains(text, KeepGoingOn) || on == strings.Contains(text, NoNewWork) {
			t.Errorf("keep-going %v: digest = %q", on, text)
		}
		if es[0].Origin != store.DeskBySystem {
			t.Errorf("origin = %q", es[0].Origin)
		}
	}
}
