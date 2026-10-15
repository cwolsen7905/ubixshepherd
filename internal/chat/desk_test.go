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

// The brief tells the desk to open a question when it asks the person one and to mark
// it answered only when a main-chat reply plainly covers it.
func TestDeskBriefTracksQuestions(t *testing.T) {
	for _, want := range []string{"question_open", "question_answer", "plainly covers it", "never mark one answered on a guess"} {
		if !strings.Contains(DeskBrief, want) {
			t.Errorf("DeskBrief lacks %q", want)
		}
	}
}

func TestDeskBriefHasLinkRule(t *testing.T) {
	for _, want := range []string{"#note_<first_note_id>", "/-/merge_requests/<iid>", "links_thread"} {
		if !strings.Contains(DeskBrief, want) {
			t.Errorf("DeskBrief lacks %q", want)
		}
	}
}

func TestDeskBriefMentionsArtifactTools(t *testing.T) {
	for _, want := range []string{"artifact_list", "artifact_read"} {
		if !strings.Contains(DeskBrief, want) {
			t.Errorf("DeskBrief lacks %q", want)
		}
	}
}
