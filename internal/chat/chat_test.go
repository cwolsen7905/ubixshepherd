package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestParse(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__shepherd__lane_open","input":{"repo":"app","name":"feat/x","scope":["src/**"]}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"opened"}]}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"Opened feat/x."}]}}
not json
{"type":"result","subtype":"success","result":"Opened feat/x.","session_id":"s","total_cost_usd":0.25}
`
	var got []Line
	ok := Parse(strings.NewReader(stream), func(l Line) { got = append(got, l) })
	if !ok || len(got) != 3 || got[2].Kind != KindCost || got[2].Text != "0.25" {
		t.Fatalf("lines = %+v", got)
	}
	if got[0].Kind != KindTool || got[0].Text != "lane_open app feat/x scope src/**" {
		t.Errorf("tool line = %+v", got[0])
	}
	if got[1].Kind != KindDesk || got[1].Text != "Opened feat/x." {
		t.Errorf("text line = %+v", got[1])
	}
	got = nil
	Parse(strings.NewReader(`{"type":"result","subtype":"error_max_turns","is_error":true}`+"\n"), func(l Line) { got = append(got, l) })
	if len(got) != 1 || got[0].Kind != KindError {
		t.Errorf("error result = %+v", got)
	}
}

func TestDeskArgs(t *testing.T) {
	d := ClaudeDesk{Bin: "claude", Shepherd: "/bin/shepherd", Dir: "/w"}
	first := strings.Join(d.Args("S", "hello", true), " ")
	for _, want := range []string{"-p hello", "--session-id S", "--append-system-prompt", "--strict-mcp-config", `"args":["mcp"]`, "--allowedTools mcp__shepherd Read Grep Glob", "--disallowedTools Edit Write Bash"} {
		if !strings.Contains(first, want) {
			t.Errorf("first turn lacks %q", want)
		}
	}
	next := strings.Join(d.Args("S", "again", false), " ")
	if !strings.Contains(next, "--resume S") || strings.Contains(next, "--append-system-prompt") {
		t.Errorf("next turn: %s", next)
	}
}

// fakeDesk records what it is told and replies with one line.
type fakeDesk struct {
	mu   sync.Mutex
	got  []string
	hold chan struct{} // when set, a turn waits for it
}

func (f *fakeDesk) Turn(_ context.Context, session, message string, emit func(Line)) (string, error) {
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	f.got = append(f.got, message)
	f.mu.Unlock()
	emit(Line{KindDesk, "ok: " + strings.SplitN(message, "\n", 2)[0]})
	if session == "" {
		session = "new-session"
	}
	return session, nil
}

type fakeAPI struct {
	feed     []store.FeedItem
	answered map[int64]string
	settings map[string]string
	ds       []api.DecisionView
	spent    float64
}

func (f *fakeAPI) Feed(_ context.Context, after int64) (api.Feed, error) {
	if after < 0 {
		return api.Feed{Last: 0}, nil
	}
	var out api.Feed
	out.Last = after
	for _, it := range f.feed {
		if it.ID > after {
			out.Items = append(out.Items, it)
			out.Last = it.ID
		}
	}
	return out, nil
}
func (f *fakeAPI) Lanes(context.Context, int64, int64) ([]api.LaneView, error) { return nil, nil }
func (f *fakeAPI) Runs(context.Context, int64, string, int) ([]api.RunView, error) {
	return nil, nil
}
func (f *fakeAPI) RunLog(context.Context, int64, int64) (api.RunLog, error) {
	return api.RunLog{Data: "line\n", Offset: 5, Done: true}, nil
}
func (f *fakeAPI) Decisions(context.Context, string) ([]api.DecisionView, error) { return f.ds, nil }
func (f *fakeAPI) Answer(_ context.Context, id int64, a string) (store.Decision, error) {
	f.answered[id] = a
	return store.Decision{ID: id, AnswerRun: 9}, nil
}
func (f *fakeAPI) SpendToday(context.Context) (api.SpendToday, error) {
	return api.SpendToday{Day: "today", USD: f.spent, Budget: 20}, nil
}
func (f *fakeAPI) AddSpend(_ context.Context, sp store.Spend) error {
	f.spent += sp.USD
	return nil
}
func (f *fakeAPI) Setting(_ context.Context, k string) (string, error) { return f.settings[k], nil }
func (f *fakeAPI) SetSetting(_ context.Context, k, v string) error {
	f.settings[k] = v
	return nil
}

// drive runs a command and every command its messages lead to, the way the Bubble Tea
// runtime would, until nothing is left (ticks excluded).
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch msg := msg.(type) {
		case nil, tickMsg:
			continue
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		}
		_, next := m.Update(msg)
		queue = append(queue, next)
	}
}

func newTestModel() (*Model, *fakeDesk, *fakeAPI) {
	d := &fakeDesk{}
	a := &fakeAPI{answered: map[int64]string{}, settings: map[string]string{}}
	m := New(context.Background(), a, d, store.Workspace{ID: 1, Name: "git", Path: "/w"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m, d, a
}

func typeLine(t *testing.T, m *Model, s string) {
	t.Helper()
	m.input.SetValue(s)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drive(t, m, cmd)
}

func has(lines []Line, kind, sub string) bool {
	for _, l := range lines {
		if l.Kind == kind && strings.Contains(l.Text, sub) {
			return true
		}
	}
	return false
}

func TestMessageGoesToTheDeskAndSessionIsKept(t *testing.T) {
	m, d, a := newTestModel()
	typeLine(t, m, "open a lane for the login fix")
	if !has(m.Lines(), KindYou, "login fix") || !has(m.Lines(), KindDesk, "ok: open a lane") {
		t.Fatalf("thread = %+v", m.Lines())
	}
	if a.settings[settingSession] != "new-session" {
		t.Errorf("session not saved: %v", a.settings)
	}
	typeLine(t, m, "and another")
	if len(d.got) != 2 || m.Busy() {
		t.Errorf("desk got %v, busy %v", d.got, m.Busy())
	}
}

func TestEventsShowAndActionableOnesBriefTheDesk(t *testing.T) {
	m, d, a := newTestModel()
	drive(t, m, m.pollFeed()) // the first poll learns where the feed is
	a.feed = []store.FeedItem{
		{ID: 1, Kind: store.FeedRunStarted, Text: "run 3: copilot started in lane api"},
		{ID: 2, Kind: store.FeedDecision, Text: "decision 4 from claude: raise the price?", Ref: 4},
		{ID: 3, Kind: store.FeedRunEnded, Text: "run 3: copilot in lane api succeeded, 1 commit(s)"},
	}
	drive(t, m, m.pollFeed())
	if !has(m.Lines(), KindEvent, "copilot started") || !has(m.Lines(), KindDecision, "/answer 4") {
		t.Fatalf("thread = %+v", m.Lines())
	}
	// Only the ended run needs someone, so only it reaches the desk.
	if len(d.got) != 1 || !strings.HasPrefix(d.got[0], "[Shepherd]") || !strings.Contains(d.got[0], "succeeded") || strings.Contains(d.got[0], "started") {
		t.Errorf("desk briefed with %q", d.got)
	}

	typeLine(t, m, "/auto off")
	a.feed = append(a.feed, store.FeedItem{ID: 4, Kind: store.FeedRunEnded, Text: "run 5 ended"})
	drive(t, m, m.pollFeed())
	if len(d.got) != 1 {
		t.Errorf("desk briefed with auto off: %q", d.got)
	}
}

func TestAnswerIsThePersonsAndPicksOptions(t *testing.T) {
	m, d, a := newTestModel()
	a.ds = []api.DecisionView{{Decision: store.Decision{ID: 4, Options: []string{"8080", "9090"}}}}
	typeLine(t, m, "/answer 4 2")
	if a.answered[4] != "option 2: 9090" {
		t.Errorf("answered %q", a.answered[4])
	}
	if len(d.got) != 0 {
		t.Error("an answer went through the desk")
	}
	if !has(m.Lines(), KindInfo, "carries on as run 9") {
		t.Errorf("thread = %+v", m.Lines())
	}
}

func TestQueueWhileTheDeskWorks(t *testing.T) {
	m, d, _ := newTestModel()
	d.hold = make(chan struct{})
	m.input.SetValue("first")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Busy() {
		t.Fatal("not busy")
	}
	m.input.SetValue("second")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	close(d.hold)
	drive(t, m, cmd)
	if len(d.got) != 2 || d.got[1] != "second" {
		t.Errorf("desk got %q", d.got)
	}
}

func TestCommands(t *testing.T) {
	m, _, a := newTestModel()
	a.settings[settingSession] = "old"
	typeLine(t, m, "/new")
	if a.settings[settingSession] != "" {
		t.Error("/new kept the session")
	}
	typeLine(t, m, "/log 7")
	if m.logRun != 7 || !strings.Contains(m.logText.String(), "line") {
		t.Errorf("/log: run %d, text %q", m.logRun, m.logText.String())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.logRun != 0 {
		t.Error("Esc did not leave the log")
	}
	typeLine(t, m, "/bogus")
	if !has(m.Lines(), KindError, "unknown command") {
		t.Error("unknown command not reported")
	}
	if v := m.View(); !strings.Contains(v, "LANES") || !strings.Contains(v, "desk ready") {
		t.Errorf("view:\n%s", v)
	}
}

func TestDeskCostIsRecordedNotShown(t *testing.T) {
	m, _, a := newTestModel()
	_, cmd := m.Update(deskLineMsg{KindCost, "0.4"})
	drive(t, m, cmd)
	if a.spent != 0.4 || has(m.Lines(), KindCost, "") {
		t.Errorf("spent %v, lines %+v", a.spent, m.Lines())
	}
}
