package chat

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Calm palette for agents and lanes: distinct enough to scan, quiet enough to read.
var nameColors = []lipgloss.Color{
	lipgloss.Color("39"),  // blue
	lipgloss.Color("78"),  // green
	lipgloss.Color("180"), // sand
	lipgloss.Color("110"), // steel
	lipgloss.Color("176"), // rose
	lipgloss.Color("144"), // moss
	lipgloss.Color("117"), // sky
	lipgloss.Color("216"), // peach
}

func wrapHard(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	rs := []rune(line)
	var rows []string
	for len(rs) > width {
		rows = append(rows, string(rs[:width]))
		rs = rs[width:]
	}
	return append(rows, string(rs))
}

var (
	styleYou       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	styleDesk      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleTool      = lipgloss.NewStyle().Faint(true)
	styleEvent     = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("244"))
	styleShepherd  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("242")).Italic(true)
	styleDecision  = lipgloss.NewStyle().Foreground(lipgloss.Color("178"))
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleInfo      = lipgloss.NewStyle().Faint(true).Italic(true)
	stylePanel     = lipgloss.NewStyle().PaddingLeft(2)
	styleHead      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	styleSel       = lipgloss.NewStyle().Reverse(true)
	styleLabelYou  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	styleLabelDesk = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
)

func colorFor(name string) lipgloss.Color {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nameColors[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return nameColors[int(h.Sum32())%len(nameColors)]
}

func styleAgent(name string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	case "copilot":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	case "cursor":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("180"))
	}
	return lipgloss.NewStyle().Foreground(colorFor(name))
}

func styleLane(name string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorFor("lane:" + name))
}

// wrapBody wraps text to width while keeping markdown tables and fenced code readable.
func wrapBody(text string, width int) string {
	if width < 20 {
		width = 20
	}
	lines := strings.Split(text, "\n")
	var out []string
	inFence := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			out = append(out, clipRunes(line, width))
			continue
		}
		if inFence || isTableRow(line) {
			out = append(out, wrapHard(line, width)...)
			continue
		}
		out = append(out, wrapWords(line, width)...)
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "|") && strings.Contains(trim[1:], "|")
}

func wrapWords(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}
	// Preserve leading indent on the first wrapped line.
	indent := ""
	for _, r := range line {
		if r == ' ' || r == '\t' {
			indent += string(r)
			continue
		}
		break
	}
	if len([]rune(indent)) >= width/2 {
		indent = ""
	}
	pad := indent
	var rows []string
	var cur strings.Builder
	cur.WriteString(pad)
	for i, w := range words {
		need := w
		if cur.Len() > len(pad) {
			need = " " + w
		}
		curWidth := len([]rune(cur.String()))
		padWidth := len([]rune(pad))
		if curWidth+len([]rune(need)) > width && curWidth > padWidth {
			rows = append(rows, cur.String())
			cur.Reset()
			cur.WriteString(pad)
			need = w
		}
		// A single word longer than the width: hard-break it.
		for len([]rune(need)) > width {
			rs := []rune(need)
			rows = append(rows, string(rs[:width]))
			need = string(rs[width:])
		}
		cur.WriteString(need)
		if i == len(words)-1 {
			rows = append(rows, cur.String())
		}
	}
	if len(rows) == 0 {
		return []string{line}
	}
	return rows
}

func clipRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(rs[:n-1]) + "…"
}

func clipTo(s string, n int) string {
	return clipRunes(s, n)
}

// tintAgents colors known agent names inside a plain line (panel / event text).
func tintAgents(s string) string {
	for _, a := range []string{"claude", "copilot", "cursor"} {
		if i := indexFold(s, a); i >= 0 {
			before, mid, after := s[:i], s[i:i+len(a)], s[i+len(a):]
			return before + styleAgent(a).Render(mid) + tintAgents(after)
		}
	}
	return s
}

func indexFold(s, sub string) int {
	ls, lsub := strings.ToLower(s), strings.ToLower(sub)
	i := strings.Index(ls, lsub)
	if i < 0 {
		return -1
	}
	// Prefer whole-word matches.
	if i > 0 {
		prev := rune(s[i-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			rest := indexFold(s[i+1:], sub)
			if rest < 0 {
				return -1
			}
			return i + 1 + rest
		}
	}
	end := i + len(sub)
	if end < len(s) {
		next := rune(s[end])
		if unicode.IsLetter(next) || unicode.IsDigit(next) {
			rest := indexFold(s[i+1:], sub)
			if rest < 0 {
				return -1
			}
			return i + 1 + rest
		}
	}
	return i
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

func fmtUSD(usd, budget float64, day string) string {
	if day == "" {
		return ""
	}
	s := fmt.Sprintf("  ·  $%.2f today", usd)
	if budget > 0 {
		s += fmt.Sprintf(" of $%.0f", budget)
	}
	return s
}
