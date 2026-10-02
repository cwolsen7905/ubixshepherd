package chat

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/convo"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// API is what the chat needs from the daemon; *client.Client provides it.
type API interface {
	Feed(ctx context.Context, after int64) (api.Feed, error)
	Lanes(ctx context.Context, workspaceID, repoID int64) ([]api.LaneView, error)
	Runs(ctx context.Context, laneID int64, state string, limit int) ([]api.RunView, error)
	RunLog(ctx context.Context, id, offset int64) (api.RunLog, error)
	Decisions(ctx context.Context, state string) ([]api.DecisionView, error)
	Answer(ctx context.Context, id int64, answer string) (store.Decision, error)
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	SpendToday(ctx context.Context) (api.SpendToday, error)
	AddSpend(ctx context.Context, sp store.Spend) error
	Sessions(ctx context.Context, repoID int64) ([]api.SessionView, error)
	AskSession(ctx context.Context, id, question string) (convo.Answer, error)
}

// Settings the chat keeps in the daemon.
const settingSession = "desk.session"

// autoKinds are feed items that continue the desk on their own when it is idle: they
// usually need someone to act.
var autoKinds = map[string]bool{
	store.FeedRunEnded:      true,
	store.FeedRequestStuck:  true,
	store.FeedRequestFailed: true,
}

const panelWidth = 34

// Model is the chat's state.
type Model struct {
	ctx       context.Context
	api       API
	desk      Desk
	workspace store.Workspace

	lines   []Line
	session string
	busy    bool
	queue   []string
	auto    bool
	deskCh  chan tea.Msg
	pending []string // events waiting for the desk to be free

	lastFeed int64
	lanes    []api.LaneView
	runs     []api.RunView

	spend    api.SpendToday
	sessions []api.SessionView

	logRun    int64
	logText   strings.Builder
	logOffset int64
	logDone   bool

	width, height int
	thread        viewport.Model
	logView       viewport.Model
	input         textarea.Model
	ready         bool
}

// New returns the chat for a workspace.
func New(ctx context.Context, a API, d Desk, ws store.Workspace) *Model {
	in := textarea.New()
	in.Placeholder = "Talk to Shepherd. /help for commands."
	in.ShowLineNumbers = false
	in.SetHeight(2)
	in.CharLimit = 8000
	in.Focus()
	return &Model{ctx: ctx, api: a, desk: d, workspace: ws, auto: true, input: in, lastFeed: -1}
}

// Lines returns the thread so far (for tests).
func (m *Model) Lines() []Line { return m.lines }

// Busy reports whether the desk is mid-turn (for tests).
func (m *Model) Busy() bool { return m.busy }

type (
	tickMsg  struct{}
	feedMsg  api.Feed
	panelMsg struct {
		lanes    []api.LaneView
		runs     []api.RunView
		spend    api.SpendToday
		sessions []api.SessionView
	}
	sessionMsg  string
	deskLineMsg Line
	deskDoneMsg struct {
		session string
		err     error
	}
	logMsg   api.RunLog
	lineMsg  Line
	errorMsg struct{ err error }
)

func (m *Model) Init() tea.Cmd {
	m.add(Line{KindInfo, fmt.Sprintf("Shepherd, workspace %s (%s). The desk delegates to agents in lanes; their events appear here. /help for commands.", m.workspace.Name, m.workspace.Path)})
	return tea.Batch(textarea.Blink, m.loadSession(), m.openDecisions(), m.pollFeed(), m.pollPanel(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) loadSession() tea.Cmd {
	return func() tea.Msg {
		s, _ := m.api.Setting(m.ctx, settingSession)
		return sessionMsg(s)
	}
}

func (m *Model) openDecisions() tea.Cmd {
	return func() tea.Msg {
		ds, err := m.api.Decisions(m.ctx, store.DecisionOpen)
		if err != nil || len(ds) == 0 {
			return nil
		}
		return lineMsg{KindDecision, fmt.Sprintf("%d decision(s) waiting for you: %s. /decisions to see them.", len(ds), decisionIDs(ds))}
	}
}

func decisionIDs(ds []api.DecisionView) string {
	var ids []string
	for _, d := range ds {
		ids = append(ids, strconv.FormatInt(d.ID, 10))
	}
	return strings.Join(ids, ", ")
}

// pollFeed asks for new feed items. The first poll only learns the newest id, so the
// thread starts with what happens from now on.
func (m *Model) pollFeed() tea.Cmd {
	after := m.lastFeed
	return func() tea.Msg {
		f, err := m.api.Feed(m.ctx, after)
		if err != nil {
			return errorMsg{err}
		}
		return feedMsg(f)
	}
}

func (m *Model) pollPanel() tea.Cmd {
	return func() tea.Msg {
		lanes, err := m.api.Lanes(m.ctx, m.workspace.ID, 0)
		if err != nil {
			return errorMsg{err}
		}
		runs, err := m.api.Runs(m.ctx, 0, "", 8)
		if err != nil {
			return errorMsg{err}
		}
		sp, _ := m.api.SpendToday(m.ctx)
		ss, _ := m.api.Sessions(m.ctx, 0)
		return panelMsg{lanes, runs, sp, ss}
	}
}

func (m *Model) pollLog() tea.Cmd {
	id, off := m.logRun, m.logOffset
	return func() tea.Msg {
		l, err := m.api.RunLog(m.ctx, id, off)
		if err != nil {
			return errorMsg{err}
		}
		return logMsg(l)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			if m.logRun != 0 {
				m.logRun = 0
				return m, nil
			}
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			if m.logRun != 0 {
				m.logView, cmd = m.logView.Update(msg)
			} else {
				m.thread, cmd = m.thread.Update(msg)
			}
			return m, cmd
		case tea.KeyEnter:
			if m.logRun != 0 {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			m.input.Reset()
			if text == "" {
				return m, nil
			}
			return m, m.handle(text)
		}
		if m.logRun == 0 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
	case tickMsg:
		cmds = append(cmds, m.pollFeed(), m.pollPanel(), tick())
		if m.logRun != 0 && !m.logDone {
			cmds = append(cmds, m.pollLog())
		}
	case sessionMsg:
		m.session = string(msg)
	case feedMsg:
		cmds = append(cmds, m.onFeed(api.Feed(msg)))
	case panelMsg:
		m.lanes, m.runs, m.spend, m.sessions = msg.lanes, msg.runs, msg.spend, msg.sessions
	case deskLineMsg:
		if msg.Kind == KindCost {
			usd, _ := strconv.ParseFloat(msg.Text, 64)
			cmds = append(cmds, func() tea.Msg { m.api.AddSpend(m.ctx, store.Spend{Source: "desk", USD: usd}); return nil })
		} else {
			m.add(Line(msg))
		}
		cmds = append(cmds, waitDesk(m.deskCh))
	case deskDoneMsg:
		cmds = append(cmds, m.onDeskDone(msg))
	case logMsg:
		m.logText.WriteString(msg.Data)
		m.logOffset, m.logDone = msg.Offset, msg.Done
		m.logView.SetContent(m.logText.String())
		m.logView.GotoBottom()
	case lineMsg:
		m.add(Line(msg))
	case errorMsg:
		m.add(Line{KindError, msg.err.Error()})
	}
	return m, tea.Batch(cmds...)
}

// handle is what the person typed: a command, or a message for the desk.
func (m *Model) handle(text string) tea.Cmd {
	if !strings.HasPrefix(text, "/") {
		m.add(Line{KindYou, text})
		return m.send(text)
	}
	f := strings.Fields(text)
	switch f[0] {
	case "/help":
		m.add(Line{KindInfo, "/answer <decision> <option number or words>   answer a decision yourself\n" +
			"/sessions   your adopted conversations; /attach <id> reopens one here, /ask <id> <question> asks it\n" +
			"/decisions   decisions waiting for you\n/log <run>   a run's live output (Esc to come back)\n" +
			"/auto on|off   let swarm events reach the desk on their own (on)\n/new   start a new conversation with the desk\n/quit   leave (Ctrl-C too)"})
	case "/quit", "/exit":
		return tea.Quit
	case "/new":
		m.session = ""
		m.add(Line{KindInfo, "The next message starts a new conversation with the desk."})
		return func() tea.Msg { m.api.SetSetting(m.ctx, settingSession, ""); return nil }
	case "/auto":
		if len(f) == 2 && (f[1] == "on" || f[1] == "off") {
			m.auto = f[1] == "on"
		}
		m.add(Line{KindInfo, fmt.Sprintf("Swarm events reach the desk on their own: %v.", m.auto)})
	case "/decisions":
		return func() tea.Msg {
			ds, err := m.api.Decisions(m.ctx, store.DecisionOpen)
			if err != nil {
				return errorMsg{err}
			}
			if len(ds) == 0 {
				return lineMsg{KindInfo, "No decisions waiting for you."}
			}
			var b strings.Builder
			for _, d := range ds {
				b.WriteString(decisionText(d))
				b.WriteString("\n")
			}
			return lineMsg{KindDecision, strings.TrimSpace(b.String())}
		}
	case "/answer":
		if len(f) < 3 {
			m.add(Line{KindError, "usage: /answer <decision> <option number or words>"})
			return nil
		}
		id, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			m.add(Line{KindError, "decision id must be a number"})
			return nil
		}
		answer := strings.Join(f[2:], " ")
		m.add(Line{KindYou, fmt.Sprintf("answer to decision %d: %s", id, answer)})
		return m.answer(id, answer)
	case "/sessions":
		return func() tea.Msg {
			ss, err := m.api.Sessions(m.ctx, 0)
			if err != nil {
				return errorMsg{err}
			}
			if len(ss) == 0 {
				return lineMsg{KindInfo, "No conversations adopted. From a shell: shepherd session import"}
			}
			var b strings.Builder
			for _, s := range ss {
				open := ""
				if s.InUse {
					open = " (may be open)"
				}
				fmt.Fprintf(&b, "%s  %-10s %s  ·  %s%s\n", s.ID[:8], s.Repo, clipTo(s.Title, 70), s.Last.Local().Format("Jan 2"), open)
			}
			b.WriteString("/attach <id> to reopen one here; /ask <id> <question> to ask it")
			return lineMsg{KindInfo, b.String()}
		}
	case "/attach":
		if len(f) != 2 {
			m.add(Line{KindError, "usage: /attach <conversation id>"})
			return nil
		}
		s, err := m.conversation(f[1])
		if err != nil {
			m.add(Line{KindError, err.Error()})
			return nil
		}
		bin, err := exec.LookPath("claude")
		if err != nil {
			m.add(Line{KindError, "claude is not on PATH"})
			return nil
		}
		if s.InUse {
			m.add(Line{KindInfo, "That conversation changed in the last few minutes; if it is open in another terminal, use that one."})
		}
		m.add(Line{KindInfo, fmt.Sprintf("Opening conversation %s (%s) in Claude Code; exit it to come back here.", s.ID[:8], clipTo(s.Title, 60))})
		cmd := exec.Command(bin, "--resume", s.ID)
		cmd.Dir = s.Dir
		id := s.ID[:8]
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return lineMsg{KindInfo, fmt.Sprintf("Back from conversation %s (%v).", id, err)}
			}
			return lineMsg{KindInfo, fmt.Sprintf("Back from conversation %s.", id)}
		})
	case "/ask":
		if len(f) < 3 {
			m.add(Line{KindError, "usage: /ask <conversation id> <question>"})
			return nil
		}
		s, err := m.conversation(f[1])
		if err != nil {
			m.add(Line{KindError, err.Error()})
			return nil
		}
		q := strings.Join(f[2:], " ")
		m.add(Line{KindYou, fmt.Sprintf("to conversation %s: %s", s.ID[:8], q)})
		id, title := s.ID, s.Title
		return func() tea.Msg {
			a, err := m.api.AskSession(m.ctx, id, q)
			if err != nil {
				return errorMsg{err}
			}
			return lineMsg{KindDesk, fmt.Sprintf("conversation %s (%s):\n%s", id[:8], clipTo(title, 50), a.Text)}
		}
	case "/log":
		if len(f) != 2 {
			m.add(Line{KindError, "usage: /log <run>"})
			return nil
		}
		id, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			m.add(Line{KindError, "run id must be a number"})
			return nil
		}
		m.logRun, m.logOffset, m.logDone = id, 0, false
		m.logText.Reset()
		m.logView.SetContent("")
		return m.pollLog()
	default:
		m.add(Line{KindError, "unknown command " + f[0] + "; /help"})
	}
	return nil
}

// answer records the person's own answer, turning a bare option number into the option.
func (m *Model) answer(id int64, answer string) tea.Cmd {
	return func() tea.Msg {
		if n, err := strconv.Atoi(answer); err == nil {
			if ds, err := m.api.Decisions(m.ctx, ""); err == nil {
				for _, d := range ds {
					if d.ID == id && n >= 1 && n <= len(d.Options) {
						answer = fmt.Sprintf("option %d: %s", n, d.Options[n-1])
					}
				}
			}
		}
		d, err := m.api.Answer(m.ctx, id, answer)
		if err != nil {
			return errorMsg{err}
		}
		if d.AnswerRun != 0 {
			return lineMsg{KindInfo, fmt.Sprintf("Answered decision %d; the agent carries on as run %d.", d.ID, d.AnswerRun)}
		}
		return lineMsg{KindInfo, fmt.Sprintf("Answered decision %d; the agent gets it when its turn ends.", d.ID)}
	}
}

// session finds an adopted conversation by id or prefix, among those last polled.
func (m *Model) conversation(prefix string) (api.SessionView, error) {
	var hit []api.SessionView
	for _, s := range m.sessions {
		if strings.HasPrefix(s.ID, prefix) {
			hit = append(hit, s)
		}
	}
	switch len(hit) {
	case 0:
		return api.SessionView{}, fmt.Errorf("no conversation starts with %s (/sessions)", prefix)
	case 1:
		return hit[0], nil
	}
	return api.SessionView{}, fmt.Errorf("%d conversations start with %s; give more of the id", len(hit), prefix)
}

func decisionText(d api.DecisionView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "decision %d from %s in lane %s: %s", d.ID, d.Agent, d.Lane, d.Question)
	for i, o := range d.Options {
		fmt.Fprintf(&b, "\n   %d. %s", i+1, o)
	}
	if d.Recommendation != "" {
		fmt.Fprintf(&b, "\n   recommends: %s", d.Recommendation)
	}
	fmt.Fprintf(&b, "\n   /answer %d <option or words>", d.ID)
	return b.String()
}

// send gives the desk a message, or queues it while the desk is mid-turn.
func (m *Model) send(text string) tea.Cmd {
	if m.busy {
		m.queue = append(m.queue, text)
		return nil
	}
	m.busy = true
	ch := make(chan tea.Msg, 64)
	m.deskCh = ch
	session := m.session
	go func() {
		s, err := m.desk.Turn(m.ctx, session, text, func(l Line) { ch <- deskLineMsg(l) })
		ch <- deskDoneMsg{s, err}
	}()
	return waitDesk(ch)
}

func waitDesk(ch chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg { return <-ch }
}

func (m *Model) onDeskDone(msg deskDoneMsg) tea.Cmd {
	m.busy = false
	var cmds []tea.Cmd
	if msg.err != nil {
		m.add(Line{KindError, "the desk: " + msg.err.Error()})
	}
	if msg.session != "" && msg.session != m.session {
		m.session = msg.session
		s := msg.session
		cmds = append(cmds, func() tea.Msg { m.api.SetSetting(m.ctx, settingSession, s); return nil })
	}
	switch {
	case len(m.queue) > 0:
		next := m.queue[0]
		m.queue = m.queue[1:]
		cmds = append(cmds, m.send(next))
	case len(m.pending) > 0 && m.auto:
		cmds = append(cmds, m.brief())
	}
	return tea.Batch(cmds...)
}

// onFeed adds new swarm events to the thread, and briefs the desk on the ones that need
// someone to act.
func (m *Model) onFeed(f api.Feed) tea.Cmd {
	first := m.lastFeed < 0
	m.lastFeed = f.Last
	if first {
		return nil
	}
	for _, it := range f.Items {
		kind := KindEvent
		text := it.Text
		if it.Kind == store.FeedDecision {
			kind = KindDecision
			text += fmt.Sprintf("\n   /answer %d <option or words>", it.Ref)
		}
		m.add(Line{kind, text})
		if autoKinds[it.Kind] {
			m.pending = append(m.pending, it.Text)
		}
	}
	if len(m.pending) > 0 && m.auto && !m.busy {
		return m.brief()
	}
	return nil
}

// brief continues the desk with the events waiting for it.
func (m *Model) brief() tea.Cmd {
	msg := "[Shepherd] Since your last turn:\n- " + strings.Join(m.pending, "\n- ") +
		"\nTell the person briefly what matters and what needs them. Route any request that needs routing. Do not start new work they have not asked for."
	m.pending = nil
	return m.send(msg)
}

// add appends to the thread, keeping the view at the bottom.
func (m *Model) add(l Line) {
	m.lines = append(m.lines, l)
	if m.ready {
		m.thread.SetContent(m.renderThread())
		m.thread.GotoBottom()
	}
}

func (m *Model) layout() {
	inputH := 4
	threadW := m.width - panelWidth - 1
	if threadW < 30 {
		threadW = m.width
	}
	h := m.height - inputH - 1
	if !m.ready {
		m.thread = viewport.New(threadW, h)
		m.logView = viewport.New(m.width, m.height-2)
		m.ready = true
	} else {
		m.thread.Width, m.thread.Height = threadW, h
		m.logView.Width, m.logView.Height = m.width, m.height-2
	}
	m.input.SetWidth(m.width)
	m.thread.SetContent(m.renderThread())
	m.thread.GotoBottom()
}

var (
	styleYou      = lipgloss.NewStyle().Bold(true)
	styleDesk     = lipgloss.NewStyle()
	styleTool     = lipgloss.NewStyle().Faint(true)
	styleEvent    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleDecision = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleInfo     = lipgloss.NewStyle().Faint(true).Italic(true)
	stylePanel    = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).PaddingLeft(1)
	styleHead     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
)

func (m *Model) renderThread() string {
	w := m.thread.Width - 2
	if w < 20 {
		w = 20
	}
	var b strings.Builder
	for i, l := range m.lines {
		var s string
		switch l.Kind {
		case KindYou:
			s = styleYou.Width(w).Render("you  " + l.Text)
		case KindDesk:
			s = styleDesk.Width(w).Render(l.Text)
		case KindTool:
			s = styleTool.Width(w).Render("  → " + l.Text)
		case KindEvent:
			s = styleEvent.Width(w).Render("• " + l.Text)
		case KindDecision:
			s = styleDecision.Width(w).Render("? " + l.Text)
		case KindError:
			s = styleError.Width(w).Render("! " + l.Text)
		default:
			s = styleInfo.Width(w).Render(l.Text)
		}
		b.WriteString(s)
		b.WriteString("\n")
		// A burst of tool calls stays together; everything else gets a blank line.
		if i+1 >= len(m.lines) || m.lines[i+1].Kind != KindTool {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *Model) renderPanel() string {
	var b strings.Builder
	// By lane id, not name: two repos can each have a lane called feat/x.
	running := map[int64]string{}
	names := map[string]int{}
	for _, r := range m.runs {
		if r.State == store.RunRunning {
			running[r.LaneID] = r.Agent
		}
	}
	for _, l := range m.lanes {
		names[l.Name]++
	}
	b.WriteString(styleHead.Render("LANES") + "\n")
	if len(m.lanes) == 0 {
		b.WriteString(styleInfo.Render("none open") + "\n")
	}
	for _, l := range m.lanes {
		mark, who, name := "○", "", l.Name
		if a, ok := running[l.ID]; ok {
			mark, who = "●", " "+a
		}
		if names[l.Name] > 1 {
			name = l.Repo + ":" + l.Name // say which one
		}
		b.WriteString(clipTo(fmt.Sprintf("%s %s%s", mark, name, who), panelWidth-2) + "\n")
	}
	b.WriteString("\n" + styleHead.Render("RUNS") + "\n")
	for _, r := range m.runs {
		b.WriteString(clipTo(fmt.Sprintf("%-4d %-7s %s", r.ID, r.Agent, short(r.State)), panelWidth-2) + "\n")
	}
	if len(m.runs) > 0 {
		b.WriteString(styleInfo.Render("/log <run> to watch one") + "\n")
	}
	if len(m.sessions) > 0 {
		b.WriteString("\n" + styleHead.Render("CONVERSATIONS") + "\n")
		for i, s := range m.sessions {
			if i == 5 {
				b.WriteString(styleInfo.Render(fmt.Sprintf("+%d more: /sessions", len(m.sessions)-5)) + "\n")
				break
			}
			b.WriteString(clipTo(fmt.Sprintf("%s %s", s.ID[:8], s.Title), panelWidth-2) + "\n")
		}
		b.WriteString(styleInfo.Render("/attach <id> · /ask <id> …") + "\n")
	}
	return stylePanel.Height(m.thread.Height).Render(b.String())
}

func short(state string) string {
	switch state {
	case store.RunRunning:
		return "running"
	case store.RunSucceeded:
		return "done"
	}
	return state
}

func clipTo(s string, n int) string {
	if len([]rune(s)) > n {
		return string([]rune(s)[:n-1]) + "…"
	}
	return s
}

func (m *Model) View() string {
	if !m.ready {
		return "starting…"
	}
	if m.logRun != 0 {
		state := "following"
		if m.logDone {
			state = "ended"
		}
		head := styleHead.Render(fmt.Sprintf("run %d (%s)", m.logRun, state)) + styleInfo.Render("   Esc back to the thread · PgUp/PgDn scroll")
		return head + "\n" + m.logView.View()
	}
	body := m.thread.View()
	if m.thread.Width < m.width {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.renderPanel())
	}
	status := "desk ready"
	if m.busy {
		status = "desk working…"
		if n := len(m.queue); n > 0 {
			status += fmt.Sprintf(" (%d queued)", n)
		}
	}
	if m.spend.Day != "" {
		status += fmt.Sprintf("  ·  $%.2f today", m.spend.USD)
		if m.spend.Budget > 0 {
			status += fmt.Sprintf(" of $%.0f", m.spend.Budget)
		}
	}
	return body + "\n" + styleInfo.Render(status+"  ·  Enter send · PgUp/PgDn scroll · /help · Ctrl-C quit") + "\n" + m.input.View()
}
