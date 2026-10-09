package chat

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keys(p *pager, s ...string) {
	for _, k := range s {
		switch k {
		case "enter":
			p.update(tea.KeyMsg{Type: tea.KeyEnter})
		case "esc":
			p.update(tea.KeyMsg{Type: tea.KeyEsc})
		case "backspace":
			p.update(tea.KeyMsg{Type: tea.KeyBackspace})
		case "pgup":
			p.update(tea.KeyMsg{Type: tea.KeyPgUp})
		case "pgdown":
			p.update(tea.KeyMsg{Type: tea.KeyPgDown})
		default:
			for _, r := range k {
				p.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
		}
	}
}

func numbered(n int, marks map[int]string) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("line %d %s", i, marks[i]))
	}
	return out
}

func TestPagerSearchesAndMoves(t *testing.T) {
	p := newPager("transcript", 60, 12) // 10 lines of body
	p.setLines(numbered(100, map[int]string{20: "the Needle", 50: "a needle", 90: "NEEDLE again"}))
	if p.top != 90 || !p.follow {
		t.Fatalf("opens at top %d, follow %v; want the bottom", p.top, p.follow)
	}

	keys(p, "g")
	if p.top != 0 {
		t.Fatalf("g: top %d", p.top)
	}
	keys(p, "/", "needlx", "backspace", "e", "enter")
	if p.searching || p.term != "needle" || len(p.matches) != 3 {
		t.Fatalf("search: term %q, matches %v", p.term, p.matches)
	}
	if v := p.view(); !strings.Contains(v, "line 20 the Needle") || !strings.Contains(v, "/needle  1 of 3") {
		t.Fatalf("first match not shown:\n%s", v)
	}
	keys(p, "n")
	if p.matches[p.cur] != 50 || !strings.Contains(p.view(), "line 50 a needle") {
		t.Fatalf("n: at match %d", p.matches[p.cur])
	}
	keys(p, "n", "n")
	if p.matches[p.cur] != 20 {
		t.Fatalf("n wraps around: at %d", p.matches[p.cur])
	}
	keys(p, "N")
	if p.matches[p.cur] != 90 || !strings.Contains(p.view(), "line 90 NEEDLE again") {
		t.Fatalf("N: at %d", p.matches[p.cur])
	}

	keys(p, "/", "haystack", "enter")
	if !strings.Contains(p.view(), "not found: haystack") || len(p.matches) != 0 {
		t.Fatalf("missing term:\n%s", p.view())
	}
	keys(p, "/", "zzz", "esc")
	if p.searching || p.term != "haystack" {
		t.Fatal("Esc did not cancel the search being typed")
	}

	keys(p, "G")
	if p.top != 90 {
		t.Fatalf("G: top %d", p.top)
	}
	keys(p, "pgup")
	if p.top != 81 || p.follow {
		t.Fatalf("PgUp: top %d, follow %v", p.top, p.follow)
	}
	keys(p, "pgdown")
	if p.top != 90 || !p.follow {
		t.Fatalf("PgDn: top %d, follow %v", p.top, p.follow)
	}
	p.update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if p.top != 87 {
		t.Fatalf("wheel: top %d", p.top)
	}
	// Not following: new lines leave the reader where they are.
	p.setLines(numbered(120, nil))
	if p.top != 87 {
		t.Fatalf("lost place on new lines: top %d", p.top)
	}
	if v := p.view(); strings.Count(v, "\n") != 11 {
		t.Errorf("view is %d lines, want 12", strings.Count(v, "\n")+1)
	}
}

func TestCtrlOOpensTheTranscript(t *testing.T) {
	m, _, _ := newTestModel()
	printed := capturePrints(m)
	for i := 0; i < 40; i++ {
		m.Update(lineMsg{KindEvent, fmt.Sprintf("event %d.", i)})
	}
	m.Update(lineMsg{KindDesk, "the webhook secret is in the vault"})
	n := len(*printed)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.pager == nil || cmd == nil {
		t.Fatal("Ctrl-O did not open the transcript")
	}
	if v := m.View(); !strings.Contains(v, "transcript") || !strings.Contains(v, "the webhook secret") {
		t.Fatalf("transcript does not open at the bottom:\n%s", v)
	}
	for _, r := range "/event 3." {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v := m.View(); !strings.Contains(v, "· event 3.") || !strings.Contains(v, "1 of 1") {
		t.Fatalf("search in the transcript:\n%s", v)
	}
	if m.input.Value() != "" {
		t.Errorf("keys for the pager reached the input: %q", m.input.Value())
	}
	// Entries arriving meanwhile show in the transcript, and print once it closes.
	m.Update(lineMsg{KindEvent, "run 9 started"})
	if len(*printed) != n {
		t.Fatal("printed while the transcript was open")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pager != nil || len(*printed) != n+1 || !strings.Contains(strings.Join(*printed, ""), "run 9 started") {
		t.Fatalf("Esc: open %v, printed %q", m.pager != nil, (*printed)[n:])
	}
	if !strings.Contains(m.View(), "Ctrl-O transcript") {
		t.Errorf("live region does not say how to open the transcript:\n%s", m.View())
	}
}
