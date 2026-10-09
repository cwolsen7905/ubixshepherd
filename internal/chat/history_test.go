package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// historyDesk is a fake desk that remembers its conversation.
type historyDesk struct {
	*fakeDesk
	entries []Entry
	session string
}

func (h *historyDesk) History(session string, n int) ([]Entry, error) {
	h.session = session
	if len(h.entries) > n {
		return h.entries[len(h.entries)-n:], nil
	}
	return h.entries, nil
}

func TestHistoryIsReplayedOnStart(t *testing.T) {
	t0 := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)
	a := &fakeAPI{answered: map[int64]string{}, settings: map[string]string{settingSession: "S"}}
	for i := int64(1); i <= 60; i++ {
		a.feed = append(a.feed, store.FeedItem{ID: i, Kind: store.FeedRunStarted, Text: fmt.Sprintf("event %d.", i), Created: t0.Add(time.Duration(i) * time.Minute)})
	}
	d := &historyDesk{fakeDesk: &fakeDesk{}, entries: []Entry{
		{t0.Add(10 * time.Minute), Line{KindYou, "long ago"}},
		{t0.Add(59*time.Minute + 30*time.Second), Line{KindYou, "hello"}},
		{t0.Add(59*time.Minute + 40*time.Second), Line{KindTool, "lane_open app feat/x"}},
		{t0.Add(59*time.Minute + 50*time.Second), Line{KindDesk, "hi there"}},
	}}
	m := New(context.Background(), a, d, store.Workspace{ID: 1, Name: "git", Path: "/w"})
	m.tick = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	m.HistoryItems = 10
	printed := capturePrints(m)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	drive(t, m, m.Init())

	out := strings.Join(*printed, "\n")
	order := []string{"Earlier: the last 10 entries", "event 54.", "event 59.", "› hello", "→ lane_open app feat/x", "● hi there", "event 60.", "Shepherd, workspace git"}
	at := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 || i < at {
			t.Fatalf("%q missing or out of order in:\n%s", want, out)
		}
		at = i
	}
	for _, gone := range []string{"event 53.", "long ago"} {
		if strings.Contains(out, gone) {
			t.Errorf("printed %q, beyond the last 10:\n%s", gone, out)
		}
	}
	if d.session != "S" || m.session != "S" {
		t.Errorf("session: desk read %q, chat has %q", d.session, m.session)
	}

	// The feed goes on from where history ended, without repeating it.
	a.feed = append(a.feed, store.FeedItem{ID: 61, Kind: store.FeedRunStarted, Text: "event 61."})
	n := len(m.Lines())
	drive(t, m, m.pollFeed())
	if got := m.Lines()[n:]; len(got) != 1 || got[0].Text != "event 61." {
		t.Errorf("after history, the feed added %+v", got)
	}
}

func TestClaudeDeskReadsItsSession(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-w")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	records := []string{
		`{"type":"user","timestamp":"2026-10-08T09:00:00Z","message":{"role":"user","content":"open a lane for the login fix"}}`,
		`{"type":"assistant","timestamp":"2026-10-08T09:00:05Z","message":{"content":[{"type":"tool_use","name":"ToolSearch","input":{}},{"type":"tool_use","name":"mcp__shepherd__lane_open","input":{"repo":"app","name":"fix/login"}}]}}`,
		`{"type":"user","timestamp":"2026-10-08T09:00:06Z","message":{"role":"user","content":[{"type":"tool_result","content":"opened"}]}}`,
		`{"type":"assistant","timestamp":"2026-10-08T09:00:09Z","message":{"content":[{"type":"text","text":"Opened **fix/login**."}]}}`,
		`{"type":"user","timestamp":"2026-10-08T09:05:00Z","message":{"role":"user","content":"[Shepherd] Since your last turn:\n- run 3 ended"}}`,
		`{"type":"user","timestamp":"2026-10-08T09:05:01Z","isMeta":true,"message":{"role":"user","content":"meta"}}`,
		`{"type":"user","timestamp":"2026-10-08T09:05:02Z","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`not json`,
		`{"type":"assistant","timestamp":"2026-10-08T09:05:09Z","message":{"content":[{"type":"text","text":"Run 3 is done."}]}}`,
	}
	if err := os.WriteFile(filepath.Join(proj, "S1.jsonl"), []byte(strings.Join(records, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := ClaudeDesk{Projects: dir}
	es, err := d.History("S1", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{
		{KindYou, "open a lane for the login fix"},
		{KindTool, "lane_open app fix/login"},
		{KindDesk, "Opened **fix/login**."},
		{KindDesk, "Run 3 is done."},
	}
	if len(es) != len(want) {
		t.Fatalf("entries = %+v", es)
	}
	for i, w := range want {
		if es[i].Line != w {
			t.Errorf("entry %d = %+v, want %+v", i, es[i].Line, w)
		}
	}
	if es[0].At.IsZero() {
		t.Error("entries lack their time")
	}
	if es, _ := d.History("S1", 2); len(es) != 2 || es[1].Line.Text != "Run 3 is done." {
		t.Errorf("last 2 = %+v", es)
	}
	for _, s := range []string{"", "missing", "../S1"} {
		if es, err := d.History(s, 10); len(es) != 0 || err != nil {
			t.Errorf("History(%q) = %+v, %v", s, es, err)
		}
	}
}
