package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestWriteQuestionShowsWhatItIsAbout(t *testing.T) {
	var b bytes.Buffer
	writeQuestion(&b, store.Question{ID: 3, State: "open", Turn: 2, Text: "Ship?", Repo: "app", LaneID: 4, NextID: 9})
	if got := b.String(); !strings.Contains(got, "app  lane 4  item 9  Ship?") {
		t.Errorf("linked: %q", got)
	}
	b.Reset()
	writeQuestion(&b, store.Question{ID: 3, State: "open", Turn: 2, Text: "Ship?"})
	if got := b.String(); got != "3  open  turn 2  Ship?\n" {
		t.Errorf("plain: %q", got)
	}
}
