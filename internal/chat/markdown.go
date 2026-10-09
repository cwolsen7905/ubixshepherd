package chat

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The desk writes markdown. renderMarkdown draws what a terminal reader needs of it:
// headings, lists with hanging indents, quotes, rules, fenced code kept as written,
// tables laid out in columns that fit the width, and inline bold, code and links.
// Anything else stays as the desk wrote it.

var (
	reHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	reRule    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	reList    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$`)
	reQuote   = regexp.MustCompile(`^\s*>\s?(.*)$`)
	reBold    = regexp.MustCompile(`\*\*([^*\s](?:[^*]*[^*\s])?)\*\*|__([^_\s](?:[^_]*[^_\s])?)__`)
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reAlign   = regexp.MustCompile(`^:?-+:?$`)
)

func renderMarkdown(text string, width int) string {
	if width < 10 {
		width = 10
	}
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~"):
			fence := trim[:3]
			i++
			for ; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence); i++ {
				code := strings.ReplaceAll(strings.TrimRight(lines[i], " \t"), "\t", "    ")
				for _, l := range strings.Split(ansi.Hardwrap(code, width-2, true), "\n") {
					out = append(out, "  "+styleMdBlock.Render(l))
				}
			}
		case isTableRow(line):
			start := i
			for i+1 < len(lines) && isTableRow(lines[i+1]) {
				i++
			}
			out = append(out, renderTable(lines[start:i+1], width)...)
		case reHeading.MatchString(trim):
			h := reHeading.FindStringSubmatch(trim)[2]
			out = append(out, styleLines(styleMdHead, wrapText(inline(h), width)))
		case reRule.MatchString(line):
			out = append(out, styleMdFaint.Render(strings.Repeat("─", min(width, 40))))
		case reList.MatchString(line):
			mm := reList.FindStringSubmatch(line)
			indent := strings.Repeat(" ", min(len(strings.ReplaceAll(mm[1], "\t", "  ")), width/3))
			mark := mm[2]
			if mark == "-" || mark == "*" || mark == "+" {
				mark = "•"
			}
			lead := indent + mark + " "
			pad := strings.Repeat(" ", ansi.StringWidth(lead))
			body := wrapText(inline(mm[3]), width-len(pad))
			out = append(out, hang(lead, pad, body))
		case reQuote.MatchString(line):
			body := wrapText(inline(reQuote.FindStringSubmatch(line)[1]), width-2)
			out = append(out, hang(styleMdFaint.Render("│ "), styleMdFaint.Render("│ "), styleLines(styleInfo, body)))
		default:
			out = append(out, wrapText(inline(line), width))
		}
	}
	return strings.Join(out, "\n")
}

// inline styles bold, code spans and links. Code spans are kept as written.
func inline(s string) string {
	parts := strings.Split(s, "`")
	if len(parts)%2 == 0 {
		// An unmatched backtick: not a code span.
		parts = []string{s}
	}
	for i, p := range parts {
		if i%2 == 1 {
			parts[i] = styleMdCode.Render(p)
			continue
		}
		p = reBold.ReplaceAllStringFunc(p, func(m string) string {
			return styleMdBold.Render(m[2 : len(m)-2])
		})
		p = reLink.ReplaceAllStringFunc(p, func(m string) string {
			mm := reLink.FindStringSubmatch(m)
			if mm[1] == mm[2] {
				return mm[1]
			}
			return mm[1] + " " + styleMdFaint.Render("("+mm[2]+")")
		})
		parts[i] = p
	}
	return strings.Join(parts, "")
}

func isTableRow(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "|") && strings.Contains(trim[1:], "|")
}

func tableCells(line string) []string {
	trim := strings.TrimSpace(line)
	trim = strings.TrimPrefix(trim, "|")
	trim = strings.TrimSuffix(trim, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(trim); i++ {
		switch {
		case trim[i] == '\\' && i+1 < len(trim) && trim[i+1] == '|':
			cur.WriteByte('|')
			i++
		case trim[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(trim[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// renderTable lays a markdown table out in columns. Columns too wide for the terminal
// share the width, wider ones giving way first, and their cells wrap. Too narrow even
// for that, each row becomes a list of "header: value" lines.
func renderTable(lines []string, width int) []string {
	var rows [][]string
	var align []string
	for _, l := range lines {
		cells := tableCells(l)
		if align == nil && len(rows) == 1 && isAlignRow(cells) {
			align = cells
			continue
		}
		for i := range cells {
			cells[i] = inline(cells[i])
		}
		rows = append(rows, cells)
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			widths[c] = max(widths[c], ansi.StringWidth(cell))
		}
	}
	const sep = " │ "
	avail := width - len([]rune(sep))*(cols-1)
	if avail < cols*4 {
		return tableAsList(rows, width)
	}
	fitWidths(widths, avail)

	var out []string
	for ri, r := range rows {
		wrapped := make([][]string, cols)
		height := 1
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			wrapped[c] = strings.Split(ansi.Wrap(cell, widths[c], ""), "\n")
			height = max(height, len(wrapped[c]))
		}
		for y := 0; y < height; y++ {
			var b strings.Builder
			for c := 0; c < cols; c++ {
				if c > 0 {
					b.WriteString(styleMdFaint.Render(sep))
				}
				cell := ""
				if y < len(wrapped[c]) {
					cell = wrapped[c][y]
				}
				if ri == 0 && len(rows) > 1 {
					cell = styleMdBold.Render(cell)
				}
				b.WriteString(pad(cell, widths[c], alignOf(align, c), c == cols-1))
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
		if ri == 0 && len(rows) > 1 {
			var rule []string
			for _, w := range widths {
				rule = append(rule, strings.Repeat("─", w))
			}
			out = append(out, styleMdFaint.Render(strings.Join(rule, "─┼─")))
		}
	}
	return out
}

func isAlignRow(cells []string) bool {
	for _, c := range cells {
		if !reAlign.MatchString(strings.ReplaceAll(c, " ", "")) {
			return false
		}
	}
	return len(cells) > 0
}

func alignOf(align []string, c int) string {
	if c >= len(align) {
		return "left"
	}
	a := strings.TrimSpace(align[c])
	switch {
	case strings.HasPrefix(a, ":") && strings.HasSuffix(a, ":"):
		return "center"
	case strings.HasSuffix(a, ":"):
		return "right"
	}
	return "left"
}

func pad(cell string, w int, align string, last bool) string {
	gap := w - ansi.StringWidth(cell)
	if gap <= 0 {
		return cell
	}
	switch align {
	case "right":
		return strings.Repeat(" ", gap) + cell
	case "center":
		left := gap / 2
		cell = strings.Repeat(" ", left) + cell
		gap -= left
	}
	if last {
		return cell
	}
	return cell + strings.Repeat(" ", gap)
}

// fitWidths shrinks column widths to fit avail: each column keeps its width if it is
// under a fair share of what is left, and the widest share the rest.
func fitWidths(widths []int, avail int) {
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= avail {
		return
	}
	order := make([]int, len(widths))
	for i := range order {
		order[i] = i
	}
	// Narrowest first.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && widths[order[j]] < widths[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	left := avail
	for k, c := range order {
		share := left / (len(order) - k)
		widths[c] = max(1, min(widths[c], share))
		left -= widths[c]
	}
}

func tableAsList(rows [][]string, width int) []string {
	if len(rows) == 0 {
		return nil
	}
	head := rows[0]
	var out []string
	for ri, r := range rows[1:] {
		if ri > 0 {
			out = append(out, "")
		}
		for c, cell := range r {
			label := ""
			if c < len(head) && head[c] != "" {
				label = styleMdBold.Render(head[c]) + ": "
			}
			out = append(out, strings.Split(wrapText(label+cell, width), "\n")...)
		}
	}
	if len(rows) == 1 {
		out = append(out, strings.Split(wrapText(strings.Join(head, " · "), width), "\n")...)
	}
	return out
}
