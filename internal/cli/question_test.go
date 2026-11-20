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

func TestQuestionOpenToolPassesTheLinks(t *testing.T) {
	var open mcpTool
	for _, tl := range mcpTools() {
		if tl.Name == "question_open" {
			open = tl
		}
	}
	if open.Name == "" {
		t.Fatal("no question_open tool")
	}
	args, err := open.args(map[string]any{"text": "Ship?", "lane": float64(4), "item": float64(9)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "question open Ship? --lane 4 --item 9" {
		t.Errorf("linked: %q", got)
	}
	args, _ = open.args(map[string]any{"text": "Ship?"})
	if got := strings.Join(args, " "); got != "question open Ship?" {
		t.Errorf("plain: %q", got)
	}
}
