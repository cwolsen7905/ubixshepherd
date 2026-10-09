package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/convo"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// DefaultHistory is how many earlier entries the chat shows when it starts.
const DefaultHistory = 50

// Entry is a line of the thread with when it happened, for replaying history.
type Entry struct {
	At   time.Time
	Line Line
}

// Historian is a desk that can read back its conversation: the person's messages, its
// replies and its tool calls, the last n of them, oldest first. The swarm's events come
// from the daemon's feed, so a desk leaves out the briefs it was given about them.
type Historian interface {
	History(session string, n int) ([]Entry, error)
}

// History reads the conversation back from the session file Claude Code keeps for it.
func (d ClaudeDesk) History(session string, n int) ([]Entry, error) {
	if session == "" || n <= 0 || strings.ContainsAny(session, `/\.`) {
		return nil, nil
	}
	projects := d.Projects
	if projects == "" {
		projects = convo.ClaudeHome()
	}
	files, err := filepath.Glob(filepath.Join(projects, "*", session+".jsonl"))
	if err != nil || len(files) == 0 {
		return nil, err
	}
	fh, err := os.Open(files[0])
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out []Entry
	keep := func(at time.Time, l Line) {
		l.Text = redact.String(l.Text)
		out = append(out, Entry{at, l})
		if len(out) > n {
			out = out[1:]
		}
	}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		var r struct {
			Type        string `json:"type"`
			Timestamp   string `json:"timestamp"`
			IsMeta      bool   `json:"isMeta"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.IsMeta || r.IsSidechain {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		switch r.Type {
		case "user":
			if t := said(r.Message.Content); t != "" && !strings.HasPrefix(t, "[Shepherd]") {
				keep(at, Line{KindYou, t})
			}
		case "assistant":
			var parts []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(r.Message.Content, &parts) != nil {
				continue
			}
			for _, p := range parts {
				switch {
				case p.Type == "text" && strings.TrimSpace(p.Text) != "":
					keep(at, Line{KindDesk, strings.TrimSpace(p.Text)})
				case p.Type == "tool_use" && p.Name != "ToolSearch":
					keep(at, Line{KindTool, toolLine(p.Name, p.Input)})
				}
			}
		}
	}
	return out, sc.Err()
}

// said is what the person wrote in a user record: a plain string, or the text parts of a
// list. Tool results and Claude Code's own wrappers are not.
func said(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "<") {
			return ""
		}
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &parts)
	var b []string
	for _, p := range parts {
		if t := strings.TrimSpace(p.Text); p.Type == "text" && t != "" && !strings.HasPrefix(t, "<") {
			b = append(b, t)
		}
	}
	return strings.Join(b, "\n")
}

// feedLine is a feed item as a line of the thread.
func feedLine(it store.FeedItem, live bool) Line {
	if it.Kind != store.FeedDecision {
		return Line{KindEvent, it.Text}
	}
	text := it.Text
	if live {
		text += fmt.Sprintf("\n   /answer %d <option or words>", it.Ref)
	}
	return Line{KindDecision, text}
}

type historyMsg struct {
	session string
	entries []Entry
	last    int64 // the feed's newest id, to poll after
	deskErr error
}

// loadHistory reads the desk's session, the last entries of its conversation and of the
// feed, merged in time order, and where the feed ends so polling picks up from there.
func (m *Model) loadHistory() tea.Cmd {
	n := m.HistoryItems
	return func() tea.Msg {
		session, err := m.api.Setting(m.ctx, settingSession)
		if err != nil {
			return errorMsg{err}
		}
		f, err := m.api.Feed(m.ctx, -1)
		if err != nil {
			return errorMsg{err}
		}
		h := historyMsg{session: session, last: f.Last}
		if n <= 0 {
			return h
		}
		after := f.Last - int64(n)
		if after < 0 {
			after = 0
		}
		for after < h.last {
			page, err := m.api.Feed(m.ctx, after)
			if err != nil {
				return errorMsg{err}
			}
			for _, it := range page.Items {
				if it.ID <= h.last {
					h.entries = append(h.entries, Entry{it.Created, feedLine(it, false)})
				}
			}
			if len(page.Items) == 0 || page.Last <= after {
				break
			}
			after = page.Last
		}
		if hd, ok := m.desk.(Historian); ok {
			es, err := hd.History(session, n)
			h.deskErr = err
			h.entries = append(h.entries, es...)
		}
		sort.SliceStable(h.entries, func(i, j int) bool { return h.entries[i].At.Before(h.entries[j].At) })
		if len(h.entries) > n {
			h.entries = h.entries[len(h.entries)-n:]
		}
		return h
	}
}

func (m *Model) onHistory(h historyMsg) {
	m.loadingHistory = false
	m.session = h.session
	m.lastFeed = h.last
	if len(h.entries) > 0 {
		m.add(Line{KindInfo, fmt.Sprintf("Earlier: the last %d entries, from %s.", len(h.entries), when(h.entries[0].At))})
		for _, e := range h.entries {
			m.add(e.Line)
		}
	}
	if h.deskErr != nil {
		m.add(Line{KindError, "could not read the desk's earlier conversation: " + h.deskErr.Error()})
	}
	m.welcome()
}

// welcome says where the chat is, once history is printed (or could not be read).
func (m *Model) welcome() {
	if m.welcomed {
		return
	}
	m.welcomed = true
	m.add(Line{KindInfo, fmt.Sprintf("Shepherd, workspace %s (%s). The desk delegates to agents in lanes; their events appear here. /help for commands.", m.workspace.Name, m.workspace.Path)})
}

func when(t time.Time) string {
	if t.IsZero() {
		return "earlier"
	}
	t = t.Local()
	if y, mo, d := time.Now().Date(); t.Year() == y && t.Month() == mo && t.Day() == d {
		return "today " + t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}
