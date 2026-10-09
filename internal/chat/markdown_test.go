package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const sample = "## Lanes\n" +
	"Opened **fix/login** with scope `src/auth/**` ([MR](https://example.com/mr/1)).\n" +
	"\n" +
	"- first item with enough words to wrap at a narrow width\n" +
	"  - nested\n" +
	"1. numbered\n" +
	"> quoted\n" +
	"---\n" +
	"```go\n" +
	"if x := **y**; x { return } // a long line of code that is longer than the width\n" +
	"```\n" +
	"| Lane | Agent | State |\n" +
	"|------|:-----:|------:|\n" +
	"| fix/login | copilot | running |\n" +
	"| feat/a-much-longer-lane-name | claude | done |\n"

func TestMarkdownRendersBlocks(t *testing.T) {
	out := renderMarkdown(sample, 80)
	for _, want := range []string{
		"Lanes\n",
		"Opened fix/login with scope src/auth/** (MR (https://example.com/mr/1)).",
		"• first item",
		"  • nested",
		"1. numbered",
		"│ quoted",
		"────",
		"  if x := **y**; x { return }", // code kept as written
		"Lane                         │  Agent  │   State",
		"─────────────────────────────┼─────────┼────────",
		"fix/login                    │ copilot │ running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"##", "```", "|---", "**fix"} {
		if strings.Contains(out, gone) {
			t.Errorf("markup %q left in:\n%s", gone, out)
		}
	}
}

func TestMarkdownFitsNarrowWidths(t *testing.T) {
	for _, w := range []int{12, 24, 36} {
		out := renderMarkdown(sample, w)
		for _, l := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(l); lw > w {
				t.Errorf("width %d: %q is %d wide", w, l, lw)
			}
		}
		// Nothing is lost to the width: a wrapped cell reads whole down its column.
		var first []string
		for _, l := range renderTable(strings.Split(sample, "\n")[11:15], w) {
			first = append(first, strings.TrimSpace(strings.Split(l, "│")[0]))
		}
		if col := strings.Join(first, ""); !strings.Contains(col, "feat/a-much-longer-lane-name") {
			t.Errorf("width %d lost the long lane name: %q", w, col)
		}
	}
	// A list item wraps under its text, not under its bullet.
	out := renderMarkdown("- first item with enough words to wrap", 16)
	if lines := strings.Split(out, "\n"); len(lines) < 2 || !strings.HasPrefix(lines[1], "  ") || strings.HasPrefix(lines[1], "   ") {
		t.Errorf("hanging indent:\n%s", out)
	}
}

func TestDeskRepliesRenderMarkdown(t *testing.T) {
	out := renderLine(Line{Kind: KindDesk, Text: "Done: **2 lanes**.\n\n| a | b |\n|---|---|\n| 1 | 2 |"}, 40)
	if !strings.HasPrefix(out, "● Done: 2 lanes.") || !strings.Contains(out, "  a │ b") {
		t.Errorf("desk reply:\n%s", out)
	}
}
