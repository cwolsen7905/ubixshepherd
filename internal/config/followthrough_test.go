package config

import (
	"strings"
	"testing"
	"time"
)

func TestFollowThroughDefaultsToShepherd(t *testing.T) {
	ft := Default().Profile("app").Watch.FollowThrough
	if ft.External() || ft.Label() != "external" || ft.Wait() != 30*time.Minute {
		t.Errorf("default follow-through = %+v", ft)
	}
}

func TestFollowThroughInheritsAndOverrides(t *testing.T) {
	c, err := Parse([]byte(`
defaults:
  watch:
    follow_through:
      owner: external
      name: reviewbot
      patience: 45m
      author: [review-bot]
repos:
  app:
    watch:
      follow_through:
        patience: 10m
  own:
    watch:
      follow_through:
        owner: shepherd
        author: []
`))
	if err != nil {
		t.Fatal(err)
	}
	app := c.Profile("app").Watch.FollowThrough
	if !app.External() || app.Label() != "reviewbot" || app.Wait() != 10*time.Minute || !app.OwnsAuthor("Review-Bot") || app.OwnsAuthor("sam") {
		t.Errorf("app = %+v", app)
	}
	if own := c.Profile("own").Watch.FollowThrough; own.External() {
		t.Errorf("own = %+v", own)
	}
	if other := c.Profile("other").Watch.FollowThrough; !other.External() || other.Wait() != 45*time.Minute {
		t.Errorf("other = %+v", other)
	}
}

func TestFollowThroughRejects(t *testing.T) {
	for in, want := range map[string]string{
		"owner: robot":    "owner",
		"owner: external": "author",
		"owner: external\n        author: [a]\n        patience: soon":  "patience",
		"owner: external\n        author: [a]\n        patience: 5s":    "patience",
		"owner: external\n        author: [a]\n        name: two words": "name",
		"author: [a]": "set only with owner: external",
	} {
		_, err := Parse([]byte("repos:\n  app:\n    watch:\n      follow_through:\n        " + in + "\n"))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "repos.app.watch.follow_through") {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
}
