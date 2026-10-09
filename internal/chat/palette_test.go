package chat

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestColorFollowsNoColorAndDumbTerminals(t *testing.T) {
	for _, c := range []struct {
		vars map[string]string
		want bool
	}{
		{map[string]string{"TERM": "xterm-256color"}, true},
		{map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}, false},
		{map[string]string{"TERM": "xterm-256color", "NO_COLOR": ""}, true},
		{map[string]string{"TERM": "dumb"}, false},
	} {
		if got := colorEnabled(env(c.vars)); got != c.want {
			t.Errorf("colorEnabled(%v) = %v, want %v", c.vars, got, c.want)
		}
	}
}

// The styles a palette chooses, every one of them.
func paletteStyles(p palette) map[string]lipgloss.Style {
	return map[string]lipgloss.Style{
		"you": p.you, "deskMark": p.deskMark, "tool": p.tool, "event": p.event, "decision": p.decision,
		"err": p.err, "info": p.info, "head": p.head, "sel": p.sel,
		"mdHead": p.mdHead, "mdBold": p.mdBold, "mdCode": p.mdCode, "mdBlock": p.mdBlock, "mdFaint": p.mdFaint,
		"ok": p.ok, "bad": p.bad, "broken": p.broken, "warn": p.warn, "accent": p.accent, "muted": p.muted,
	}
}

func TestNoColorPaletteHasNoColour(t *testing.T) {
	p := newPalette(false)
	for name, st := range paletteStyles(p) {
		if _, ok := st.GetForeground().(lipgloss.NoColor); !ok {
			t.Errorf("%s has foreground %#v", name, st.GetForeground())
		}
		if _, ok := st.GetBackground().(lipgloss.NoColor); !ok {
			t.Errorf("%s has background %#v", name, st.GetBackground())
		}
	}
	for _, n := range []string{"claude", "copilot", "someone", "lane:feat/x"} {
		if _, ok := p.agentColor(n).(lipgloss.NoColor); !ok {
			t.Errorf("agent %s has colour %#v", n, p.agentColor(n))
		}
	}
	// Emphasis stays where it carries meaning.
	if !p.you.GetBold() || !p.bad.GetBold() || !p.event.GetFaint() {
		t.Error("no-colour palette lost bold or dim")
	}
}

func TestColourPaletteAdaptsToTheBackground(t *testing.T) {
	p := newPalette(true)
	for name, st := range paletteStyles(p) {
		switch c := st.GetForeground().(type) {
		case lipgloss.NoColor:
			// Plain styles (bold, faint, reverse) take the terminal's own colour.
		case lipgloss.AdaptiveColor:
			if c.Light == "" || c.Dark == "" || c.Light == c.Dark {
				t.Errorf("%s: %#v does not adapt", name, c)
			}
		default:
			t.Errorf("%s: fixed colour %#v", name, c)
		}
	}
	for _, n := range []string{"claude", "copilot", "cursor", "someone"} {
		if _, ok := p.agentColor(n).(lipgloss.AdaptiveColor); !ok {
			t.Errorf("agent %s: colour %#v does not adapt", n, p.agentColor(n))
		}
	}
	if p.agentColor("Claude") != p.agentColor("claude") || p.nameColor("x") != p.nameColor("x") {
		t.Error("a name's colour is not stable")
	}
}

// The chat's own styles (the pager's and the markdown's too) are the palette's.
func TestStylesComeFromThePalette(t *testing.T) {
	defer usePalette(pal)
	usePalette(newPalette(false))
	for name, st := range map[string]lipgloss.Style{
		"styleYou": styleYou, "styleHead": styleHead, "styleMdCode": styleMdCode, "styleDecision": styleDecision,
		"agent": styleAgent("claude"), "lane": styleLane("feat/x"),
	} {
		if _, ok := st.GetForeground().(lipgloss.NoColor); !ok {
			t.Errorf("%s keeps colour %#v under NO_COLOR", name, st.GetForeground())
		}
	}
	usePalette(newPalette(true))
	if _, ok := styleMdCode.GetForeground().(lipgloss.AdaptiveColor); !ok {
		t.Error("styleMdCode does not follow the palette")
	}
}
