package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// pager is a full-screen reader over lines already wrapped to its width: the transcript
// (Ctrl-O) and a run's log (/log). It runs in the alternate screen, so it scrolls itself:
// keys and the mouse wheel.
type pager struct {
	title         string
	lines         []string // rendered
	plain         []string // the same without styling, for search
	top           int
	width, height int
	follow        bool // keep to the bottom as lines arrive

	searching bool   // typing a search
	query     string // the search being typed
	term      string // the last search run
	matches   []int  // lines that match term
	cur       int    // the current match, an index into matches
	note      string // "not found" and the like
}

const pagerHint = "/ search · n/N next/prev · g/G top/bottom · PgUp/PgDn · q back"

func newPager(title string, width, height int) *pager {
	return &pager{title: title, width: width, height: height, follow: true}
}

// setLines replaces the content, keeping the reader's place, or the bottom when following.
func (p *pager) setLines(lines []string) {
	p.lines = lines
	p.plain = make([]string, len(lines))
	for i, l := range lines {
		p.plain[i] = ansi.Strip(l)
	}
	if p.term != "" {
		p.matches = p.find(p.term)
		if p.cur >= len(p.matches) {
			p.cur = 0
		}
	}
	if p.follow {
		p.top = p.maxTop()
	}
	p.clamp()
}

func (p *pager) resize(width, height int) {
	p.width, p.height = width, height
	p.clamp()
}

func (p *pager) bodyHeight() int {
	if h := p.height - 2; h > 0 {
		return h
	}
	return 1
}

func (p *pager) maxTop() int {
	if n := len(p.lines) - p.bodyHeight(); n > 0 {
		return n
	}
	return 0
}

func (p *pager) clamp() {
	if p.top > p.maxTop() {
		p.top = p.maxTop()
	}
	if p.top < 0 {
		p.top = 0
	}
}

func (p *pager) scroll(n int) {
	p.top += n
	p.clamp()
	p.follow = p.top >= p.maxTop()
}

func (p *pager) find(term string) []int {
	low := strings.ToLower(term)
	var hits []int
	for i, l := range p.plain {
		if strings.Contains(strings.ToLower(l), low) {
			hits = append(hits, i)
		}
	}
	return hits
}

// search runs a search and shows the first match at or below the top of the screen.
func (p *pager) search(term string) {
	p.term, p.note = term, ""
	p.matches, p.cur = nil, 0
	if term == "" {
		return
	}
	p.matches = p.find(term)
	if len(p.matches) == 0 {
		p.note = "not found: " + term
		return
	}
	for i, at := range p.matches {
		if at >= p.top {
			p.cur = i
			break
		}
	}
	p.show(p.matches[p.cur])
}

// next moves to the next match (dir 1) or the previous one (dir -1), wrapping around.
func (p *pager) next(dir int) {
	if len(p.matches) == 0 {
		if p.term != "" {
			p.note = "not found: " + p.term
		}
		return
	}
	p.cur = (p.cur + dir + len(p.matches)) % len(p.matches)
	p.show(p.matches[p.cur])
}

// show scrolls so a line is on screen, a third of the way down.
func (p *pager) show(line int) {
	h := p.bodyHeight()
	if line < p.top || line >= p.top+h {
		p.top = line - h/3
	}
	p.clamp()
	p.follow = p.top >= p.maxTop()
}

// update handles a key or the wheel, and reports whether the pager should close.
func (p *pager) update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			p.scroll(-3)
		case tea.MouseButtonWheelDown:
			p.scroll(3)
		}
	case tea.KeyMsg:
		if p.searching {
			switch msg.Type {
			case tea.KeyEnter:
				p.searching = false
				p.search(p.query)
			case tea.KeyEsc:
				p.searching = false
			case tea.KeyBackspace:
				if rs := []rune(p.query); len(rs) > 0 {
					p.query = string(rs[:len(rs)-1])
				}
			case tea.KeyRunes, tea.KeySpace:
				if len(msg.Runes) == 0 && msg.Type == tea.KeySpace {
					p.query += " "
				}
				p.query += string(msg.Runes)
			}
			return false
		}
		page := p.bodyHeight() - 1
		if page < 1 {
			page = 1
		}
		switch msg.String() {
		case "q", "esc", "ctrl+o":
			return true
		case "/":
			p.searching, p.query, p.note = true, "", ""
		case "n":
			p.next(1)
		case "N":
			p.next(-1)
		case "g", "home":
			p.top, p.follow = 0, false
		case "G", "end":
			p.top, p.follow = p.maxTop(), true
		case "pgup", "b", "ctrl+b":
			p.scroll(-page)
		case "pgdown", " ", "f", "ctrl+f":
			p.scroll(page)
		case "ctrl+u":
			p.scroll(-page / 2)
		case "ctrl+d":
			p.scroll(page / 2)
		case "up", "k":
			p.scroll(-1)
		case "down", "j", "enter":
			p.scroll(1)
		}
	}
	return false
}

func (p *pager) view() string {
	h := p.bodyHeight()
	end := p.top + h
	if end > len(p.lines) {
		end = len(p.lines)
	}
	head := styleHead.Render(p.title)
	if len(p.lines) > 0 {
		head += styleInfo.Render(fmt.Sprintf("   %d–%d of %d", p.top+1, end, len(p.lines)))
	}
	switch {
	case p.note != "":
		head += "   " + styleError.Render(p.note)
	case len(p.matches) > 0:
		head += styleInfo.Render(fmt.Sprintf("   /%s  %d of %d", p.term, p.cur+1, len(p.matches)))
	}
	rows := []string{fit(head, p.width)}
	current := -1
	if len(p.matches) > 0 {
		current = p.matches[p.cur]
	}
	for i := p.top; i < end; i++ {
		line := p.lines[i]
		if i == current {
			line = highlight(p.plain[i], p.term)
		}
		rows = append(rows, fit(line, p.width))
	}
	for len(rows) < h+1 {
		rows = append(rows, "")
	}
	foot := styleInfo.Render(pagerHint)
	if p.searching {
		foot = "/" + p.query + "█"
	}
	return strings.Join(append(rows, fit(foot, p.width)), "\n")
}

// highlight marks each occurrence of term in a plain line.
func highlight(plain, term string) string {
	low, lowTerm := strings.ToLower(plain), strings.ToLower(term)
	if term == "" || len(low) != len(plain) || len(lowTerm) != len(term) {
		return styleSel.Render(plain)
	}
	var b strings.Builder
	for {
		i := strings.Index(low, lowTerm)
		if i < 0 {
			b.WriteString(plain)
			return b.String()
		}
		b.WriteString(plain[:i])
		b.WriteString(styleSel.Render(plain[i : i+len(term)]))
		plain, low = plain[i+len(term):], low[i+len(term):]
	}
}
