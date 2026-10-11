package desk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// An action is stored and followed like any event, and starts no turn: nothing runs,
// the queue stays empty and nobody is asked, with a client attached or not.
func TestActedStartsNoTurn(t *testing.T) {
	r := newRig(t, func(o *Options) { o.Wake = func() string { return "always" } })
	d := r.desk(t)
	sub := d.Subscribe()
	defer d.Unsubscribe(sub)
	changed := d.Changed()
	if err := d.Acted(context.Background(), "answered decision 5 (lane x) with \"yes\""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was not told of the action")
	}
	time.Sleep(300 * time.Millisecond)
	es := r.events(t)
	if len(es) != 1 || es[0].Kind != store.DeskAction || es[0].Origin != store.DeskByHuman || es[0].Turn != 0 {
		t.Fatalf("events = %s (%+v), want one human action with no turn", kinds(es), es)
	}
	if busy, queued := d.Busy(); busy || queued != 0 {
		t.Errorf("busy %v, queued %d after an action", busy, queued)
	}
	if got := r.claudeLog(t); got != "" {
		t.Errorf("the agent ran: %s", got)
	}
	if len(r.tok.humans()) != 0 {
		t.Error("a token was minted for an action")
	}
}

// The next turn, the person's or a wake-up digest, carries what was done at its head,
// once. Redaction applies, and a new conversation forgets what it held.
func TestActedPrefixesTheNextTurn(t *testing.T) {
	r := newRig(t, nil)
	d := r.desk(t)
	ctx := context.Background()
	d.Acted(ctx, "stopped run 8 (lane a)")
	d.Acted(ctx, "closed request 3: ghp_0123456789abcdefghijABCDEFGHIJ012345")
	if _, _, err := d.Send(ctx, "what now?"); err != nil {
		t.Fatal(err)
	}
	r.waitEnded(t, 1)
	log := r.claudeLog(t)
	want := "[Shepherd] Since your last turn the person did this from the web page: - stopped run 8 (lane a) - closed request 3:"
	if !strings.Contains(log, want) || !strings.Contains(log, "what now?") {
		t.Fatalf("first turn's message lacks the actions: %s", log)
	}
	if strings.Contains(log, "ghp_0123") {
		t.Errorf("a secret reached the desk: %s", log)
	}
	if strings.Index(log, "stopped run 8") > strings.Index(log, "what now?") {
		t.Errorf("the actions do not come first: %s", log)
	}
	for _, e := range r.events(t) {
		if e.Kind == store.DeskUser && e.Text != "what now?" {
			t.Errorf("the stored message was changed: %q", e.Text)
		}
	}

	if _, _, err := d.Send(ctx, "and again"); err != nil {
		t.Fatal(err)
	}
	r.waitEnded(t, 2)
	if n := strings.Count(r.claudeLog(t), "did this from the web page"); n != 1 {
		t.Errorf("actions told %d times, want once", n)
	}

	// A new conversation drops what was held; the history keeps the rows.
	d.Acted(ctx, "dismissed run 9 (lane b)")
	if err := d.New(ctx); err != nil {
		t.Fatal(err)
	}
	d.Send(ctx, "fresh")
	r.waitEnded(t, 3)
	if strings.Contains(r.claudeLog(t), "dismissed run 9") {
		t.Error("a new conversation was told of an earlier one's actions")
	}
}

// Held actions are bounded like the swarm's events: the newest win, and the prefix says
// how many were left out.
func TestActedIsBounded(t *testing.T) {
	r := newRig(t, func(o *Options) { o.DigestMax = 2 })
	d := r.desk(t)
	ctx := context.Background()
	for _, s := range []string{"did one", "did two", "did three"} {
		d.Acted(ctx, s)
	}
	got := d.takeActed()
	if strings.Contains(got, "did one") || !strings.Contains(got, "did two") || !strings.Contains(got, "did three") || !strings.Contains(got, "1 earlier action(s)") {
		t.Errorf("prefix = %q", got)
	}
	if d.takeActed() != "" {
		t.Error("the actions were kept after being taken")
	}
	if n := len(r.events(t)); n != 3 {
		t.Errorf("%d events stored, want all 3: the bound is for the prompt, not the history", n)
	}
}

// A conversation that holds only actions has nothing to summarise a first session from.
func TestActedIsNotEarlierHistory(t *testing.T) {
	r := newRig(t, nil)
	d := r.desk(t)
	ctx := context.Background()
	d.Acted(ctx, "answered decision 1 (lane a) with \"yes\"")
	d.Send(ctx, "hello")
	r.waitEnded(t, 1)
	if strings.Contains(r.claudeLog(t), "Summary for the new session") {
		t.Errorf("a first session was seeded with a summary: %s", r.claudeLog(t))
	}
}
