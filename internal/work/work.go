// Package work reads a repo's work matrix: docs/WORK.md, a `## Work` heading and one
// markdown table with the columns ID | Area | Item | Status | Lane | PR | Notes.
//
// The parser is tolerant of whitespace, alignment rows and case in the header, and strict
// on columns and status. A row it cannot read is reported as a warning, never dropped
// silently. It knows no product: the file's format is documented in docs/work-matrix.md.
package work

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Path is where a repo keeps its work matrix, relative to the repo root.
const Path = "docs/WORK.md"

// Statuses a row may have.
const (
	Todo    = "todo"
	Doing   = "doing"
	Blocked = "blocked"
	Done    = "done"
	Dropped = "dropped"
)

// Statuses lists the valid statuses in the order they are shown.
var Statuses = []string{Doing, Blocked, Todo, Done, Dropped}

// Columns are the table's header cells, in order.
var Columns = []string{"ID", "Area", "Item", "Status", "Lane", "PR", "Notes"}

// Row is one line of the matrix.
type Row struct {
	ID     string `json:"id"`
	Area   string `json:"area"`
	Item   string `json:"item"`
	Status string `json:"status"`
	// Lane is a Shepherd lane name, or empty when the cell was `-`.
	Lane string `json:"lane,omitempty"`
	// PR is the pull or merge request number, or 0 when the cell was `-`.
	PR    int    `json:"pr,omitempty"`
	Notes string `json:"notes,omitempty"`
}

// Warning is a line the parser could not use.
type Warning struct {
	// Line is 1-based; 0 when the warning is about the file as a whole.
	Line   int    `json:"line,omitempty"`
	Text   string `json:"text,omitempty"`
	Reason string `json:"reason"`
}

// Result is a parsed file.
type Result struct {
	Rows     []Row     `json:"rows"`
	Warnings []Warning `json:"warnings"`
}

var (
	headingRe = regexp.MustCompile(`^#{2}\s+Work\s*#*\s*$`)
	anyHeadRe = regexp.MustCompile(`^#{1,6}\s`)
	alignCell = regexp.MustCompile(`^:?-{1,}:?$`)
	prRe      = regexp.MustCompile(`^#(\d+)$`)
)

// Parse reads the matrix from a WORK.md's text. Rows are in file order. Prose before the
// heading and after the table is ignored.
func Parse(text string) Result {
	res := Result{Rows: []Row{}, Warnings: []Warning{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for ; i < len(lines); i++ {
		if headingRe.MatchString(strings.TrimSpace(lines[i])) {
			break
		}
	}
	if i == len(lines) {
		res.warn(0, "", "no `## Work` heading")
		return res
	}
	i++
	// The first table line after the heading is the header; a heading before it ends
	// the search.
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if anyHeadRe.MatchString(t) {
			break
		}
		if strings.HasPrefix(t, "|") {
			break
		}
	}
	if i >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
		res.warn(0, "", "no table under the `## Work` heading")
		return res
	}
	if why := checkHeader(cells(lines[i])); why != "" {
		res.warn(i+1, strings.TrimSpace(lines[i]), why)
		return res
	}
	seen := map[string]int{}
	for i++; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "|") {
			break // the table ends at the first line that is not a row
		}
		cs := cells(t)
		if isAlignment(cs) {
			continue
		}
		row, why := parseRow(cs)
		if why == "" {
			if first, dup := seen[row.ID]; dup {
				why = fmt.Sprintf("duplicate id %s (first on line %d)", row.ID, first)
			}
		}
		if why != "" {
			res.warn(i+1, t, why)
			continue
		}
		seen[row.ID] = i + 1
		res.Rows = append(res.Rows, row)
	}
	return res
}

func (r *Result) warn(line int, text, reason string) {
	r.Warnings = append(r.Warnings, Warning{Line: line, Text: text, Reason: reason})
}

// cells splits a table line into trimmed cells, dropping the outer pipes. A `\|` is a
// literal pipe.
func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func isAlignment(cs []string) bool {
	for _, c := range cs {
		if !alignCell.MatchString(c) {
			return false
		}
	}
	return len(cs) > 0
}

func checkHeader(cs []string) string {
	if len(cs) != len(Columns) {
		return fmt.Sprintf("header has %d columns, want %d: %s", len(cs), len(Columns), strings.Join(Columns, " | "))
	}
	for i, c := range cs {
		if !strings.EqualFold(c, Columns[i]) {
			return fmt.Sprintf("header column %d is %q, want %q", i+1, c, Columns[i])
		}
	}
	return ""
}

func parseRow(cs []string) (Row, string) {
	if len(cs) != len(Columns) {
		return Row{}, fmt.Sprintf("%d columns, want %d", len(cs), len(Columns))
	}
	r := Row{ID: cs[0], Area: cs[1], Item: cs[2], Status: strings.ToLower(cs[3]), Lane: cs[4], Notes: cs[6]}
	if r.ID == "" {
		return Row{}, "empty id"
	}
	if r.Item == "" {
		return Row{}, "empty item"
	}
	valid := false
	for _, s := range Statuses {
		valid = valid || s == r.Status
	}
	if !valid {
		return Row{}, fmt.Sprintf("status %q is not one of todo, doing, blocked, done, dropped", cs[3])
	}
	if r.Lane == "-" {
		r.Lane = ""
	}
	switch pr := cs[5]; {
	case pr == "-":
	case prRe.MatchString(pr):
		n, err := strconv.Atoi(prRe.FindStringSubmatch(pr)[1])
		if err != nil || n == 0 {
			return Row{}, fmt.Sprintf("pr %q is not #N or -", pr)
		}
		r.PR = n
	default:
		return Row{}, fmt.Sprintf("pr %q is not #N or -", pr)
	}
	return r, ""
}
