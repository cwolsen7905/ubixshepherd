package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWaitingSealVersion(t *testing.T) {
	a := Waiting{Items: []WaitingItem{{Kind: WaitingRequest, Action: "route", RequestID: 1, Title: "x"}}}
	a.Seal()
	b := Waiting{Items: []WaitingItem{{Kind: WaitingRequest, Action: "route", RequestID: 1, Title: "x"}}}
	b.Seal()
	if a.Version == "" || a.Version != b.Version || a.Count != 1 {
		t.Fatalf("same list, different version: %q %q", a.Version, b.Version)
	}
	b.Items[0].Title = "y"
	b.Seal()
	if a.Version == b.Version {
		t.Error("version did not move when an item changed")
	}
	raw, _ := json.Marshal(a)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"count", "counts", "items", "version"} {
		if _, ok := m[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
}

func TestWaitingLine(t *testing.T) {
	long := "Kelpie trial: (a) dry run only (`kelpie:triage` on the threads of !244), or (b) advisory mode (`kelpie:respond` posts **nothing**) and then a great many more words follow here"
	w := Waiting{Items: []WaitingItem{
		{Kind: WaitingQuestion, QuestionID: 27, Title: "question 27: question 27: " + long},
		{Kind: WaitingDecision, DecisionID: 295, Repo: "r", Lane: "l", Detail: "decision 295: see [MR !288](https://x/y) now"},
		{Kind: WaitingShip, Repo: "r", Lane: "l"},
	}}
	w.Seal()
	line := w.Line()
	if strings.Contains(line, "question 27: question 27") || strings.Count(line, "decision 295") != 1 {
		t.Errorf("label doubled: %s", line)
	}
	body := strings.TrimPrefix(line, "[Waiting on you (3)] ")
	if strings.ContainsAny(body, "`*[]") || strings.Contains(body, "https://") {
		t.Errorf("markdown left in: %s", body)
	}
	if !strings.Contains(body, "question 27: Kelpie trial: (a) dry run only") || !strings.Contains(body, "decision 295 (r:l): see MR !288 now") || !strings.HasSuffix(body, "ship (r:l)") {
		t.Errorf("unexpected line: %s", body)
	}
}

func TestShortenAtWord(t *testing.T) {
	for _, c := range []struct {
		in  string
		max int
		out string
	}{
		{"short", 10, "short"},
		{"alpha beta gamma delta", 14, "alpha beta…"},
		{"alpha beta gamma", 11, "alpha beta…"},
		{"unbroken-word-with-no-spaces", 10, "unbroken…"},
	} {
		if got := shorten(c.in, c.max); got != c.out {
			t.Errorf("shorten(%q, %d) = %q, want %q", c.in, c.max, got, c.out)
		}
	}
	if got := plainText("a `b` **c** [d](http://e) [f"); got != "a b c d f" {
		t.Errorf("plainText = %q", got)
	}
	if got := plainText("retry while n < 3 and m > 1, see <https://x.example/y>"); got != "retry while n < 3 and m > 1, see https://x.example/y" {
		t.Errorf("plainText kept no comparison signs: %q", got)
	}
}

func TestWaitingStableIDs(t *testing.T) {
	w := Waiting{Items: []WaitingItem{
		{Kind: WaitingDecision, DecisionID: 410},
		{Kind: WaitingQuestion, QuestionID: 271},
		{Kind: WaitingRequest, RequestID: 111},
		{Kind: WaitingNext, NextID: 255},
		{Kind: WaitingMR, Repo: "app", MR: 175, LaneID: 4},
		{Kind: WaitingMR, Repo: "lib", MR: 175, LaneID: 5},
		{Kind: WaitingShip, LaneID: 42},
		{Kind: WaitingBlocked, LaneID: 42, RunID: 318},
		{Kind: WaitingScope, DecisionID: 410, LaneID: 42},
		{Kind: WaitingArtifact, LaneID: 42},
		{Kind: WaitingDecision},
	}}
	w.Seal()
	want := []string{"D410", "Q271", "R111", "N255", "app!175", "lib!175", "S42", "B318", "W410", "F42", ""}
	seen := map[string]bool{}
	for i, it := range w.Items {
		if it.ID != want[i] {
			t.Errorf("item %d (%s): id %q, want %q", i, it.Kind, it.ID, want[i])
		}
		if it.ID != "" && seen[it.ID] {
			t.Errorf("id %q appears twice", it.ID)
		}
		seen[it.ID] = true
	}
	// The id does not depend on anything that changes between reads.
	a := w.Items[0]
	a.Title, a.Detail = "later words", "more"
	if a.StableID() != "D410" {
		t.Error("id moved with the text")
	}
	if line := w.Line(); !strings.HasPrefix(line, "[Waiting on you (11)] D410 decision 410; Q271 question 271; R111 request 111") {
		t.Errorf("line does not lead each item with its id: %s", line)
	}
}
