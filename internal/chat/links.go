package chat

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Links. The thread marks URLs as OSC 8 hyperlinks so a terminal can open them with a
// click, and keeps them whole when text wraps: a link is never cut at a hyphen, moves to
// the next line when it does not fit, and when it is longer than the width each piece
// carries the whole URL so a click on any piece opens it.

var (
	// A markdown link, or a bare http(s) URL. Control characters never belong to a URL.
	reLinkAny = regexp.MustCompile("\\[([^\\]]+)\\]\\(([^)\\s]+)\\)|https?://[^\\s\\x00-\\x1f\\x7f<>\"`]+")
	reMdLink  = regexp.MustCompile(`^\[([^\]]+)\]\(([^)\s]+)\)$`)
	// An OSC 8 sequence; the capture is its URL, empty when it closes a link.
	reOSC8 = regexp.MustCompile("\\x1b\\]8;[^;\\x07\\x1b]*;([^\\x07\\x1b]*)(?:\\x1b\\\\|\\x07)")
	reHeld = regexp.MustCompile("\\x00(\\d+)\\x00")
)

// noBreak stands in for a hyphen inside a link while text is wrapped: ansi.Wrap always
// breaks at hyphens, and a URL must not be cut there.
const noBreak = ""

var (
	// linkOSC says whether links are emitted as OSC 8 hyperlinks; linkShowURL whether a
	// markdown link's URL is also shown after its text, for terminals that may not
	// open links.
	linkOSC, linkShowURL = true, true
)

func init() { SetHyperlinks("auto") }

// SetHyperlinks chooses how links are drawn: "on" emits OSC 8 hyperlinks and shows only
// the text of a markdown link, "off" emits none and shows the URL, and "auto" (the
// default, and anything else) emits them and shows the URL unless the terminal is known
// to open them.
func SetHyperlinks(mode string) { setHyperlinks(mode, os.Getenv) }

func setHyperlinks(mode string, getenv func(string) string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off", "false", "never", "0":
		linkOSC, linkShowURL = false, true
	case "on", "true", "always", "1":
		linkOSC, linkShowURL = true, false
	default:
		linkOSC = getenv("TERM") != "dumb"
		linkShowURL = !terminalOpensLinks(getenv)
	}
}

// terminalOpensLinks reports whether the terminal is known to support OSC 8. Terminals
// that do not say, or that sit behind a multiplexer, are not known.
func terminalOpensLinks(getenv func(string) string) bool {
	term := getenv("TERM")
	if getenv("TERM_PROGRAM") == "tmux" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") {
		return false
	}
	switch getenv("TERM_PROGRAM") {
	case "iTerm.app", "ghostty", "WezTerm", "vscode", "WarpTerminal", "Hyper", "rio":
		return true
	}
	if strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") {
		return true
	}
	if getenv("KITTY_WINDOW_ID") != "" || getenv("WT_SESSION") != "" || getenv("ITERM_SESSION_ID") != "" {
		return true
	}
	// VTE 0.50 and later.
	if v, err := strconv.Atoi(getenv("VTE_VERSION")); err == nil && v >= 5000 {
		return true
	}
	return false
}

// hyperlink wraps text, which is drawn as it is, in an OSC 8 link to url.
func hyperlink(url, text string) string {
	if !linkOSC {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// linkable is whether url is safe and worth opening from a click.
func linkable(url string) bool {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "mailto:") {
		return false
	}
	for _, r := range url {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// trimURL drops what a sentence puts after a URL: punctuation, emphasis marks and an
// unbalanced closing bracket.
func trimURL(u string) string {
	for u != "" {
		last := u[len(u)-1]
		switch {
		case strings.IndexByte(".,;:!?'\"*", last) >= 0:
		case last == ')' && strings.Count(u, ")") > strings.Count(u, "("):
		case last == ']' && strings.Count(u, "]") > strings.Count(u, "["):
		default:
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}

// replaceLinks replaces each markdown link and bare URL in s with emit of its drawing.
func replaceLinks(s string, emit func(string) string) string {
	return reLinkAny.ReplaceAllStringFunc(s, func(m string) string {
		if m[0] == '[' {
			mm := reMdLink.FindStringSubmatch(m)
			return emit(markdownLink(mm[1], mm[2]))
		}
		u := trimURL(m)
		return emit(hyperlink(u, styleLink.Render(u))) + m[len(u):]
	})
}

// markdownLink draws [text](url): the text as a link, and the URL after it when the
// terminal may not open links.
func markdownLink(text, url string) string {
	if !linkable(url) {
		if text == url {
			return text
		}
		return text + " " + styleMdFaint.Render("("+url+")")
	}
	link := hyperlink(url, styleLink.Render(text))
	if text == url || !linkShowURL {
		return link
	}
	return link + " " + hyperlink(url, styleMdFaint.Render("("+url+")"))
}

// linkify marks the URLs and markdown links in plain text as links.
func linkify(s string) string {
	if !strings.Contains(s, "://") && !strings.Contains(s, "](") {
		return s
	}
	return replaceLinks(s, func(l string) string { return l })
}

// wrapLinks wraps plain text, with its URLs as links.
func wrapLinks(text string, width int) string { return wrapText(linkify(text), width) }

// wrapLine wraps one line to width, keeping each link whole: its hyphens do not break,
// and a link split across lines is closed and opened again on each, so no indent or
// padding around the lines is part of a link and a click on any piece opens it all.
func wrapLine(l string, width int) string {
	if !strings.Contains(l, "\x1b]8;") {
		return ansi.Wrap(l, width, "")
	}
	wrapped := ansi.Wrap(protectLinks(l), width, "")
	wrapped = strings.ReplaceAll(wrapped, noBreak, "-")
	lines := strings.Split(wrapped, "\n")
	open := ""
	for i, line := range lines {
		cur := open
		for _, m := range reOSC8.FindAllStringSubmatch(line, -1) {
			open = m[1]
		}
		if cur != "" {
			line = osc8(cur) + line
		}
		if open != "" {
			line += osc8("")
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func osc8(url string) string { return "\x1b]8;;" + url + "\x1b\\" }

// protectLinks swaps the hyphens inside links for noBreak.
func protectLinks(l string) string {
	var b strings.Builder
	last, inLink := 0, false
	for _, m := range reOSC8.FindAllStringSubmatchIndex(l, -1) {
		text := l[last:m[0]]
		if inLink {
			text = strings.ReplaceAll(text, "-", noBreak)
		}
		b.WriteString(text)
		b.WriteString(l[m[0]:m[1]])
		inLink = m[3] > m[2]
		last = m[1]
	}
	text := l[last:]
	if inLink {
		text = strings.ReplaceAll(text, "-", noBreak)
	}
	b.WriteString(text)
	return b.String()
}

// mapOutsideLinks applies f to the parts of s that are not the text of a link.
func mapOutsideLinks(s string, f func(string) string) string {
	var b strings.Builder
	last, inLink := 0, false
	for _, m := range reOSC8.FindAllStringSubmatchIndex(s, -1) {
		text := s[last:m[0]]
		if !inLink {
			text = f(text)
		}
		b.WriteString(text)
		b.WriteString(s[m[0]:m[1]])
		inLink = m[3] > m[2]
		last = m[1]
	}
	text := s[last:]
	if !inLink {
		text = f(text)
	}
	b.WriteString(text)
	return b.String()
}
