package chat

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// eventMark is how the thread marks one kind of swarm event: a glyph that carries the
// meaning alone, so the thread reads the same without colour, and a tone from the
// palette. Most events are quiet; only what waits on the person stands out.
type eventMark struct {
	glyph string
	tone  func(palette) lipgloss.Style
}

var (
	toneMuted  = func(p palette) lipgloss.Style { return p.muted }
	toneAccent = func(p palette) lipgloss.Style { return p.accent }
	toneWarn   = func(p palette) lipgloss.Style { return p.warn }
)

// eventMarks covers every api.Event* value. A pipeline event is a gate or forge
// pipeline passing, failing or handed back: the item does not say which, so its mark
// is neutral and the text tells.
var eventMarks = map[string]eventMark{
	api.EventLaneOpened:       {"+", toneMuted},
	api.EventLaneClosed:       {"−", toneMuted},
	api.EventRunStarted:       {"▸", toneAccent},
	api.EventRunEnded:         {"■", toneAccent},
	api.EventReport:           {"»", toneMuted},
	api.EventDecisionAsked:    {"?", toneWarn},
	api.EventDecisionAnswer:   {"↳", toneMuted},
	api.EventRequest:          {"⇄", toneMuted},
	api.EventRequestAttention: {"!", toneWarn},
	api.EventMR:               {"◆", toneAccent},
	api.EventPipeline:         {"◎", toneAccent},
	api.EventBudget:           {"$", toneWarn},
	api.EventTag:              {"#", toneMuted},
	api.EventRelease:          {"▲", toneMuted},
	api.EventConfig:           {"~", toneMuted},
	api.EventInfo:             {"·", toneMuted},
}

// markFor is an event's mark; an event this chat does not know reads as info.
func markFor(event string) eventMark {
	if mk, ok := eventMarks[event]; ok {
		return mk
	}
	return eventMarks[api.EventInfo]
}

// eventGlyph is an event's glyph, drawn in its tone.
func eventGlyph(event string) string {
	mk := markFor(event)
	return mk.tone(pal).Render(mk.glyph)
}
