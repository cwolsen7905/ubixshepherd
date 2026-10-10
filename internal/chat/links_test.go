package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	osc = "\x1b]8;;"
	st  = "\x1b\\"
)

func withLinks(t *testing.T, mode string) {
	t.Helper()
	setHyperlinks(mode, func(string) string { return "" })
	t.Cleanup(func() { SetHyperlinks("auto") })
}

func TestLinkifyBareURL(t *testing.T) {
	withLinks(t, "on")
	got := linkify("see https://example.com/a-b, then stop.")
	want := "see " + osc + "https://example.com/a-b" + st + "https://example.com/a-b" + osc + st + ", then stop."
	if ansi.Strip(got) != "see https://example.com/a-b, then stop." {
		t.Fatalf("visible text changed: %q", ansi.Strip(got))
	}
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestTrimURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.io/a.":                "https://x.io/a",
		"https://x.io/a)":                "https://x.io/a",
		"https://en.wikipedia.org/w/(a)": "https://en.wikipedia.org/w/(a)",
		"https://x.io/a?!":               "https://x.io/a",
		"https://x.io/a**":               "https://x.io/a",
	} {
		if got := trimURL(in); got != want {
			t.Errorf("trimURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkdownLinkModes(t *testing.T) {
	withLinks(t, "on")
	got := inline("[docs](https://x.io/d)")
	if got != osc+"https://x.io/d"+st+"docs"+osc+st {
		t.Fatalf("on: %q", got)
	}
	withLinks(t, "auto")
	if got := ansi.Strip(inline("[docs](https://x.io/d)")); got != "docs (https://x.io/d)" {
		t.Fatalf("auto, unknown terminal shows the URL: %q", got)
	}
	withLinks(t, "off")
	got = inline("[docs](https://x.io/d) https://y.io")
	if strings.Contains(got, "\x1b]8") || got != "docs (https://x.io/d) https://y.io" {
		t.Fatalf("off: %q", got)
	}
	withLinks(t, "on")
	if got := ansi.Strip(inline("[docs](docs/x.md)")); got != "docs (docs/x.md)" {
		t.Fatalf("relative path is not a link: %q", got)
	}
}

func TestTerminalOpensLinks(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	for name, c := range map[string]struct {
		env  func(string) string
		want bool
	}{
		"iterm":    {env("TERM_PROGRAM", "iTerm.app", "TERM", "xterm-256color"), true},
		"ghostty":  {env("TERM", "xterm-ghostty"), true},
		"wezterm":  {env("TERM_PROGRAM", "WezTerm"), true},
		"kitty":    {env("TERM", "xterm-kitty"), true},
		"vscode":   {env("TERM_PROGRAM", "vscode"), true},
		"vte":      {env("VTE_VERSION", "7200"), true},
		"terminal": {env("TERM_PROGRAM", "Apple_Terminal", "TERM", "xterm-256color"), false},
		"tmux":     {env("TERM_PROGRAM", "tmux", "ITERM_SESSION_ID", "w0t0p0"), false},
		"unknown":  {env(), false},
	} {
		if got := terminalOpensLinks(c.env); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestCodeSpanStaysLiteral(t *testing.T) {
	withLinks(t, "on")
	if got := inline("`https://x.io`"); strings.Contains(got, "\x1b]8") {
		t.Fatalf("link inside a code span: %q", got)
	}
}

func TestBoldAroundLink(t *testing.T) {
	withLinks(t, "on")
	got := ansi.Strip(inline("**see https://x.io/a_b__c**."))
	if got != "see https://x.io/a_b__c." {
		t.Fatalf("got %q", got)
	}
}

// linkPieces returns, for each line, its visible text and the URL its link opens ("" when
// the line has no link).
func linkPieces(t *testing.T, s string) (texts, urls []string) {
	t.Helper()
	for _, line := range strings.Split(s, "\n") {
		url := ""
		for _, m := range reOSC8.FindAllStringSubmatch(line, -1) {
			if m[1] != "" {
				url = m[1]
			}
		}
		// Every line closes what it opens.
		if strings.Count(line, osc+"http") != strings.Count(line, osc+st) {
			t.Errorf("line leaves a link open: %q", line)
		}
		texts = append(texts, ansi.Strip(line))
		urls = append(urls, url)
	}
	return
}

func TestWrapKeepsURLWhole(t *testing.T) {
	withLinks(t, "on")
	u := "https://example.com/some-long-path-with-hyphens/and/more"
	got := wrapText(linkify("check "+u+" now"), 56)
	texts, urls := linkPieces(t, got)
	if len(texts) != 3 || texts[0] != "check" || texts[1] != u || texts[2] != "now" {
		t.Fatalf("lines %q", texts)
	}
	if urls[0] != "" || urls[1] != u || urls[2] != "" {
		t.Fatalf("urls %q", urls)
	}
}

func TestWrapHardBreaksLongURLWithWholeLink(t *testing.T) {
	withLinks(t, "on")
	u := "https://example.com/" + strings.Repeat("abcdefghij-", 8)
	got := wrapText(linkify("go to "+u+" ok"), 30)
	texts, urls := linkPieces(t, got)
	if len(texts) < 3 {
		t.Fatalf("not broken: %q", texts)
	}
	var visible strings.Builder
	for i, l := range texts {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("line %d is %d wide", i, w)
		}
		visible.WriteString(l)
		if strings.Contains(l, "example") || strings.Contains(l, "abcdef") {
			if urls[i] != u {
				t.Errorf("line %d carries %q", i, urls[i])
			}
		}
	}
	if strings.ReplaceAll(visible.String(), " ", "") != "gotohttps://example.com/"+strings.Repeat("abcdefghij-", 8)+"ok" {
		t.Fatalf("text lost: %q", visible.String())
	}
}

func TestWrapMarkdownLinkAcrossLines(t *testing.T) {
	withLinks(t, "on")
	got := wrapText(inline("[the very long link text here](https://x.io/p)"), 12)
	texts, urls := linkPieces(t, got)
	if len(texts) < 3 {
		t.Fatalf("%q", texts)
	}
	for i := range urls {
		if urls[i] != "https://x.io/p" {
			t.Errorf("line %d: %q", i, urls[i])
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, " ") || strings.HasSuffix(ansi.Strip(l), " ") {
			t.Errorf("padding inside a line: %q", l)
		}
	}
}

func TestHangOutsideLink(t *testing.T) {
	withLinks(t, "on")
	u := "https://example.com/" + strings.Repeat("x", 50)
	out := renderLine(Line{Kind: KindInfo, Text: "u: " + u}, 30)
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, osc+"http"); i >= 0 && strings.TrimSpace(ansi.Strip(l[:i])) != "" && !strings.HasPrefix(ansi.Strip(l), "  u:") {
			t.Errorf("link opens after text: %q", l)
		}
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "  "+osc) && strings.Contains(l, osc) && strings.Index(l, osc) < 2 {
			t.Errorf("indent inside link: %q", l)
		}
	}
}

func TestMarkdownBlocksLinkAndWidth(t *testing.T) {
	withLinks(t, "on")
	u := "https://example.com/a-fairly-long-path/to/a/thing"
	md := "- item with " + u + " in it\n\n> quote " + u + "\n\n| name | link |\n|---|---|\n| one | " + u + " |\n\n# head [h](https://h.io)\n"
	out := renderMarkdown(md, 36)
	if n := strings.Count(out, osc+u+st); n < 3 {
		t.Fatalf("expected link wrappers for list, quote and table, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, osc+"https://h.io"+st) {
		t.Fatalf("heading link missing:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 36 {
			t.Errorf("%d wide: %q", w, ansi.Strip(l))
		}
	}
	// No visible escape debris or lost text.
	plain := ansi.Strip(out)
	if strings.Contains(plain, "]8;") || strings.Contains(plain, "\x00") {
		t.Fatalf("debris: %q", plain)
	}
}

func TestTintAgentsLeavesLinkURL(t *testing.T) {
	withLinks(t, "on")
	u := "https://github.com/claude/repo"
	got := tintAgents(linkify("claude opened " + u))
	if !strings.Contains(got, osc+u+st) {
		t.Fatalf("URL parameter was changed: %q", got)
	}
}

func TestPagerSearchSeesLinkText(t *testing.T) {
	withLinks(t, "on")
	p := newPager("t", 40, 10)
	p.setLines(strings.Split(wrapText(linkify("open https://example.com/path now"), 40), "\n"))
	if got := p.find("example.com/path"); len(got) != 1 {
		t.Fatalf("matches %v in %q", got, p.plain)
	}
	if got := p.find("8;;"); len(got) != 0 {
		t.Fatalf("search matched an escape: %v", got)
	}
}
