package crook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type conformanceCase struct {
	Name    string
	Spec    string
	Rules   map[string]string
	Limits  Limits
	Request struct {
		Action string
		Actor  Actor
		Facts  map[string]any
	}
	Expect struct {
		Verdict Kind
		Rule    string
		Shadow  []ShadowResult
	}
}

// layersFromCase groups a case's "<layer>/rules/<id>.md" keys into layers.
func layersFromCase(t *testing.T, rules map[string]string) []LayerFiles {
	t.Helper()
	by := map[string]LayerFiles{}
	for key, text := range rules {
		parts := strings.Split(key, "/")
		if len(parts) != 3 || parts[1] != "rules" {
			t.Fatalf("rule key %q is not <layer>/rules/<id>.md", key)
		}
		l, ok := by[parts[0]]
		if !ok {
			l = LayerFiles{Layer: parts[0], Files: map[string][]byte{}}
		}
		l.Files[parts[2]] = []byte(text)
		by[parts[0]] = l
	}
	var out []LayerFiles
	for _, l := range by {
		out = append(out, l)
	}
	return out
}

func TestConformance(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "crook", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no conformance cases found (%v)", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c conformanceCase
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		t.Run(c.Name, func(t *testing.T) {
			if c.Spec != "crook/1" {
				t.Fatalf("spec %q, want crook/1", c.Spec)
			}
			set := Merge(layersFromCase(t, c.Rules)...)
			if len(set.Errors) > 0 {
				t.Fatalf("load errors: %v", set.Errors)
			}
			got := Evaluate(set, Query{Action: c.Request.Action, Actor: c.Request.Actor, Facts: c.Request.Facts, Limits: c.Limits})
			if got.Verdict != c.Expect.Verdict || got.Rule != c.Expect.Rule {
				t.Errorf("got %s by %s (%s), want %s by %s", got.Verdict, got.Rule, got.Reason, c.Expect.Verdict, c.Expect.Rule)
			}
			if c.Expect.Shadow != nil {
				if len(got.Shadow) != len(c.Expect.Shadow) {
					t.Fatalf("shadow = %+v, want %+v", got.Shadow, c.Expect.Shadow)
				}
				for i, w := range c.Expect.Shadow {
					if got.Shadow[i].Rule != w.Rule || got.Shadow[i].Verdict != w.Verdict {
						t.Errorf("shadow[%d] = %+v, want %+v", i, got.Shadow[i], w)
					}
				}
			}
		})
	}
}
