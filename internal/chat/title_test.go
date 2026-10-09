package chat

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// titles runs a command and collects the window titles it sets.
func titles(cmd tea.Cmd) []string {
	var out []string
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			// Bubble Tea's title message is unexported; it prints as its string.
			if s := fmt.Sprintf("%T", msg); s == "tea.setWindowTitleMsg" {
				out = append(out, fmt.Sprint(msg))
			}
		}
	}
	return out
}

func TestWindowTitleCarriesTheCounts(t *testing.T) {
	if got := windowTitle(nil); got != "shepherd" {
		t.Errorf("empty title = %q", got)
	}
	m := dockModel()
	if got := windowTitle(m.dockItems()); got != "shepherd · 3 needs you · 2 broken · 1 to review · 1 working · 2 done unseen" {
		t.Errorf("title = %q", got)
	}

	m, _, _ = newTestModel()
	if m.Title() != "shepherd" {
		t.Fatalf("first title %q", m.Title())
	}
	// It is set again only when the counts change.
	_, cmd := m.Update(panelMsg{panelUpdated: true, decisions: dockModel().decisions})
	if got := titles(cmd); len(got) != 1 || got[0] != "shepherd · 2 needs you" {
		t.Errorf("titles set: %q", got)
	}
	_, cmd = m.Update(panelMsg{panelUpdated: true, decisions: dockModel().decisions})
	if got := titles(cmd); len(got) != 0 {
		t.Errorf("unchanged counts set the title again: %q", got)
	}
}

func TestQuitClearsTheTitleAndTheCallerRestoresIt(t *testing.T) {
	m, _, _ := newTestModel()
	m.decisions = dockModel().decisions
	// quit is a sequence (an unexported slice of commands): the flush, an empty title,
	// then quit.
	seq := reflect.ValueOf(m.quit()())
	var set []string
	quits := false
	for i := 0; seq.Kind() == reflect.Slice && i < seq.Len(); i++ {
		c, _ := seq.Index(i).Interface().(tea.Cmd)
		if c == nil {
			continue
		}
		msg := c()
		if _, ok := msg.(tea.QuitMsg); ok {
			quits = true
		}
		set = append(set, titles(func() tea.Msg { return msg })...)
	}
	if !quits || len(set) != 1 || set[0] != "" {
		t.Fatalf("quit: titles %q, quits %v", set, quits)
	}
	m.Update(nil)
	if m.Title() == "shepherd · 2 needs you" {
		t.Error("the title was set while quitting")
	}

	t.Setenv("TERM", "xterm-256color")
	var b bytes.Buffer
	restore := SaveTitle(&b)
	restore()
	if b.String() != "\x1b[22;0t\x1b[23;0t" {
		t.Errorf("save and restore wrote %q", b.String())
	}
	t.Setenv("TERM", "dumb")
	b.Reset()
	SaveTitle(&b)()
	if b.Len() != 0 {
		t.Errorf("a dumb terminal got %q", b.String())
	}
}
