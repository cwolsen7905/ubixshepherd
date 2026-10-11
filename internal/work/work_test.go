package work

import (
	"strings"
	"testing"
)

const sample = `# Work

Intro prose.

## Work

| ID | Area | Item | Status | Lane | PR | Notes |
|----|:-----|-----:|:------:|------|----|-------|
| W1 | web  | Work page | doing | feat/work-view | #12 | in review |
|W2|core|Parser|DONE|-|#3|  |
| W3 | docs | A \| pipe | todo | - | - | |

After the table: prose that is ignored.
| not | a | row |
`

func TestParse(t *testing.T) {
	r := Parse(sample)
	if len(r.Warnings) != 0 {
		t.Fatalf("warnings: %+v", r.Warnings)
	}
	if len(r.Rows) != 3 {
		t.Fatalf("rows: %+v", r.Rows)
	}
	w1 := r.Rows[0]
	if w1.ID != "W1" || w1.Status != Doing || w1.Lane != "feat/work-view" || w1.PR != 12 || w1.Notes != "in review" {
		t.Errorf("W1: %+v", w1)
	}
	if w2 := r.Rows[1]; w2.Status != Done || w2.Lane != "" || w2.PR != 3 {
		t.Errorf("W2: %+v", w2)
	}
	if r.Rows[2].Item != "A | pipe" || r.Rows[2].PR != 0 {
		t.Errorf("W3: %+v", r.Rows[2])
	}
}

func TestParseWarnings(t *testing.T) {
	head := "## Work\n\n| ID | Area | Item | Status | Lane | PR | Notes |\n|---|---|---|---|---|---|---|\n"
	cases := []struct{ name, row, want string }{
		{"status", "| A | a | i | wip | - | - | |", "status"},
		{"columns", "| A | a | i | todo | - |", "5 columns"},
		{"pr", "| A | a | i | todo | - | 12 | |", "pr"},
		{"empty id", "|  | a | i | todo | - | - | |", "empty id"},
		{"empty item", "| A | a |  | todo | - | - | |", "empty item"},
	}
	for _, c := range cases {
		r := Parse(head + c.row + "\n")
		if len(r.Rows) != 0 || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0].Reason, c.want) {
			t.Errorf("%s: %+v", c.name, r)
			continue
		}
		if r.Warnings[0].Line != 5 {
			t.Errorf("%s: line %d, want 5", c.name, r.Warnings[0].Line)
		}
	}
}

func TestParseDuplicateKeepsFirst(t *testing.T) {
	r := Parse("## Work\n| ID | Area | Item | Status | Lane | PR | Notes |\n|--|--|--|--|--|--|--|\n| A | a | one | todo | - | - | |\n| A | a | two | todo | - | - | |\n")
	if len(r.Rows) != 1 || r.Rows[0].Item != "one" || len(r.Warnings) != 1 {
		t.Errorf("%+v", r)
	}
}

func TestParseFileLevel(t *testing.T) {
	for name, text := range map[string]string{
		"empty":                   "",
		"no heading":              "# Work\n| ID | Area | Item | Status | Lane | PR | Notes |\n",
		"no table":                "## Work\n\nnothing yet\n",
		"bad header":              "## Work\n| ID | Item | Status |\n|--|--|--|\n| a | b | todo |\n",
		"wrong col":               "## Work\n| ID | Area | Item | State | Lane | PR | Notes |\n",
		"heading cuts the search": "## Work\n## Other\n| ID | Area | Item | Status | Lane | PR | Notes |\n",
	} {
		r := Parse(text)
		if len(r.Rows) != 0 || len(r.Warnings) != 1 {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestParseCRLFAndHeaderCase(t *testing.T) {
	r := Parse("## Work\r\n| id | AREA | item | Status | lane | pr | notes |\r\n|---|---|---|---|---|---|---|\r\n| A | a | i | Blocked | l | #1 | n |\r\n")
	if len(r.Rows) != 1 || r.Rows[0].Status != Blocked || len(r.Warnings) != 0 {
		t.Errorf("%+v", r)
	}
}
