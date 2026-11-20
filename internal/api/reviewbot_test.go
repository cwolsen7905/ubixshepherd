package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/reviewbot"
)

// The page reads these names: renaming one is a change to the web client.
func TestReviewBotsWire(t *testing.T) {
	r := ReviewBots{AsOf: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Bots: []reviewbot.Bot{{
		Name: "Reviewer", Projects: []reviewbot.Project{{Name: "app", Counts: reviewbot.Counts{Jobs: 1, Proposals: 2, Dismissals: 3, NeedsYou: 4, Fixes: 5, SpendUSD: 0.5}}},
	}}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"as_of"`, `"bots"`, `"jobs":1`, `"proposals":2`, `"dismissals":3`, `"needs_you":4`, `"fixes":5`, `"spend_usd":0.5`, `"projects"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s missing from %s", want, b)
		}
	}
}
