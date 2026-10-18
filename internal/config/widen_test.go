package config

import (
	"slices"
	"strings"
	"testing"
)

func TestWidenProfile(t *testing.T) {
	c, err := Parse([]byte(`
defaults:
  widen: auto
  widen_cap: 2
  widen_protected: ["vendor/**"]
repos:
  a:
    widen_max_files: 5
    widen_protected: ["gen/**"]
  b:
    widen: human
`))
	if err != nil {
		t.Fatal(err)
	}
	a, b, none := c.Profile("a"), c.Profile("b"), c.Profile("zzz")
	if a.Widen != "auto" || a.WidenCap != 2 || a.WidenMaxFiles != 5 || !slices.Equal(a.WidenProtected, []string{"vendor/**", "gen/**"}) {
		t.Errorf("a: %+v", a)
	}
	if b.Widen != "human" || none.Widen != "auto" {
		t.Errorf("b %q, unlisted %q", b.Widen, none.Widen)
	}
	def, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if def.Profile("x").Widen != "" {
		t.Errorf("default is not unset (human): %q", def.Profile("x").Widen)
	}
}

func TestWidenValidation(t *testing.T) {
	for in, want := range map[string]string{
		"defaults:\n  widen: always\n":             "widen",
		"defaults:\n  widen_max_files: -1\n":       "widen_max_files",
		"defaults:\n  widen_cap: -1\n":             "widen_cap",
		"defaults:\n  widen_protected: [\"/x\"]\n": "widen_protected",
	} {
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want an error naming %s, got %v", in, want, err)
		}
	}
}
