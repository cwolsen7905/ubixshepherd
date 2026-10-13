package chat

import (
	"strings"
	"testing"
)

// The brief tells the desk to register work agreed in chat as planned, and still never
// to add items from its own ideas.
func TestDeskBriefRegistersAgreedWork(t *testing.T) {
	for _, want := range []string{
		"without being asked",
		"call next_list first",
		"as planned, never approved",
		"quoting what the person said",
		"never add items from your own ideas that the person has not discussed",
	} {
		if !strings.Contains(DeskBrief, want) {
			t.Errorf("DeskBrief lacks %q", want)
		}
	}
}
