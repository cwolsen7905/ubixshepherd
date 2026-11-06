package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/desklog"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// seedDeskTurns writes two event turns and a person turn into the desk's conversation:
// the first turn asks for something and is answered by the person's next message.
func seedDeskTurns(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	st := h.srv.Store
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "app", Path: "/w/app"}})
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind, origin, text string, turn int64) int64 {
		e, err := st.AddDeskEvent(ctx, store.DeskEvent{WorkspaceID: ws.ID, Kind: kind, Origin: origin, Text: text, Turn: turn})
		if err != nil {
			t.Fatal(err)
		}
		return e.Turn
	}
	if err := st.AddFeed(ctx, store.FeedMR, "!7 is ready for review", 0); err != nil {
		t.Fatal(err)
	}
	t1 := add(store.DeskSystem, store.DeskBySystem, "[Shepherd] Since your last turn:\n- !7 is ready for review\n\nreact\n[Waiting on you (1)] request 3", 0)
	add(store.DeskTool, store.DeskBySystem, "next_list", t1)
	add(store.DeskAssistant, store.DeskBySystem, "Needs you:\n1. Review [!7](https://forge.example/mr/7).\n\nStatus:\n- ok", t1)
	add(store.DeskTurnEnd, store.DeskBySystem, "", t1)
	time.Sleep(5 * time.Millisecond)
	if err := st.SaveDeskTurnLog(ctx, store.DeskTurnLog{WorkspaceID: ws.ID, Turn: -1, StartedBy: desklog.ByEvent,
		Events: []store.DeskLogEvent{{Kind: store.FeedRunFailed, Repo: "app", Lane: "old", Run: 2}}}); err != nil {
		t.Fatal(err)
	}
	t2 := add(store.DeskUser, store.DeskByHuman, "is !7 passing?", 0)
	add(store.DeskAssistant, store.DeskByHuman, "Yes.", t2)
}

func TestDeskLogLinesAndSummary(t *testing.T) {
	h := newHarness(t, "", false)
	seedDeskTurns(t, h)
	run := func(args ...string) error {
		if code := h.run(append([]string{"desk"}, args...)...); code != 0 {
			return fmt.Errorf("exit %d: %s", code, h.err)
		}
		return nil
	}
	if err := run("log"); err != nil {
		t.Fatal(err)
	}
	var recs []desklog.Record
	sc := bufio.NewScanner(h.out)
	for sc.Scan() {
		var r desklog.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d lines", len(recs))
	}
	// A turn the conversation no longer holds comes first, from what the desk kept.
	if old := recs[0]; !old.Trimmed || old.Turn != -1 || len(old.Events) != 1 || old.Events[0].Lane != "old" {
		t.Fatalf("trimmed turn: %+v", old)
	}
	recs = recs[1:]
	r := recs[0]
	if r.StartedBy != desklog.ByEvent || len(r.Events) != 1 || r.Events[0].Kind != store.FeedMR ||
		r.Waiting == nil || *r.Waiting != 1 || r.Opening != desklog.OpenNeedsYou || r.NeedsYou != 1 ||
		len(r.Tools) != 1 || r.Tools[0] != "next_list" || r.NextTurn != recs[1].Turn {
		t.Fatalf("turn 1: %+v", r)
	}
	if got := strings.Join(r.NextDid, ","); got != desklog.DidFollowUp {
		t.Fatalf("next did %s", got)
	}

	// A window after everything shows nothing; a bad one is refused.
	if err := run("log", "--since", time.Now().Add(time.Hour).Format(time.RFC3339)); err != nil || strings.TrimSpace(h.out.String()) != "" {
		t.Fatalf("future window: %v %q", err, h.out.String())
	}
	if err := run("log", "--since", "soon"); err == nil {
		t.Fatal("a bad --since was taken")
	}
	if err := run("log", "--summary"); err != nil || !strings.Contains(h.out.String(), "Event turns: 2") {
		t.Fatalf("summary: %v\n%s", err, h.out.String())
	}
	if err := run("log", "--summary", "--json"); err != nil {
		t.Fatal(err)
	}
	var s desklog.Summary
	if err := json.Unmarshal(h.out.Bytes(), &s); err != nil || s.EventTurns != 2 || s.Judged != 1 {
		t.Fatalf("summary json: %v %+v", err, s)
	}
	if err := run("other"); err == nil {
		t.Fatal("an unknown subcommand was taken")
	}
}

func TestParseLogTime(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	if got, _ := parseLogTime("7d", now, false); !got.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("7d: %v", got)
	}
	start, _ := parseLogTime("2026-11-01", now, false)
	end, _ := parseLogTime("2026-11-01", now, true)
	if !end.Add(time.Nanosecond).Equal(start.AddDate(0, 0, 1)) {
		t.Fatalf("a day runs %v to %v", start, end)
	}
	if _, err := parseLogTime("x", now, false); err == nil {
		t.Fatal("garbage taken")
	}
}

func TestDeskLogMCPTool(t *testing.T) {
	h := newHarness(t, "", false)
	seedDeskTurns(t, h)
	resps := mcpExchange(t, h.env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"desk_log","arguments":{"summary":true,"since":"2000-01-01"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"desk_log","arguments":{"since":"nonsense"}}}`,
	)
	if text, isErr := toolText(t, resps[0]); isErr || !strings.Contains(text, "Event turns: 2") {
		t.Errorf("desk_log summary: %v %s", isErr, text)
	}
	if text, isErr := toolText(t, resps[1]); !isErr || !strings.Contains(text, "--since") {
		t.Errorf("desk_log bad since: %v %s", isErr, text)
	}
}
