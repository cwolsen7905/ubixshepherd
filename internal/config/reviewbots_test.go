package config

import (
	"strings"
	"testing"
)

func TestReviewBotsParse(t *testing.T) {
	c, err := Parse([]byte(`
review_bots:
  - name: Reviewer
    user: review-bot
    label_prefix: "bot::"
    projects: [app, lib]
  - name: Other
    user: other-bot
    label_prefix: "o::"
    projects: [app]
    window_days: 7
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ReviewBots) != 2 {
		t.Fatalf("review bots = %+v", c.ReviewBots)
	}
	if c.ReviewBots[0].Window() != 30 || c.ReviewBots[1].Window() != 7 {
		t.Errorf("windows = %d, %d", c.ReviewBots[0].Window(), c.ReviewBots[1].Window())
	}
	if c.ReviewBots[0].LabelPrefix != "bot::" || len(c.ReviewBots[0].Projects) != 2 {
		t.Errorf("first bot = %+v", c.ReviewBots[0])
	}
}

func TestReviewBotsValidate(t *testing.T) {
	cases := map[string]string{
		"no user":      "review_bots:\n  - {name: A, label_prefix: 'a::', projects: [x]}\n",
		"no prefix":    "review_bots:\n  - {name: A, user: u, projects: [x]}\n",
		"no projects":  "review_bots:\n  - {name: A, user: u, label_prefix: 'a::'}\n",
		"twice":        "review_bots:\n  - {name: A, user: u, label_prefix: 'a::', projects: [x]}\n  - {name: A, user: v, label_prefix: 'b::', projects: [x]}\n",
		"long window":  "review_bots:\n  - {name: A, user: u, label_prefix: 'a::', projects: [x], window_days: 900}\n",
		"bad username": "review_bots:\n  - {name: A, user: '@u', label_prefix: 'a::', projects: [x]}\n",
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil || !strings.Contains(err.Error(), "review_bots[") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
