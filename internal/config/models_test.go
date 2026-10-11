package config

import (
	"strings"
	"testing"
)

const policyYAML = `
models:
  claude:
    default: sonnet
    effort: medium
    catalogue:
      haiku: {use: "mechanical edits"}
      sonnet: {use: "everyday work", efforts: [low, medium, high], effort: high}
      opus: {use: "design", efforts: [low, high, max]}
  opencode:
    default: "qwen3-coder:30b"
    catalogue:
      "qwen3-coder:30b": {use: "small edits"}
      "local/other": {use: "other"}
projects:
  shop:
    repos: [api, web]
    models:
      deny: {claude: ["opus*"]}
defaults:
  agent:
    model: {claude: sonnet}
repos:
  api:
    agent:
      effort: {claude: low}
      models:
        allow: {opencode: ["local/*"]}
        deny: {opencode: ["*other"]}
  tool:
    agent:
      model: {claude: opus}
`

func policyConfig(t *testing.T) Config {
	t.Helper()
	c, err := Parse([]byte(policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestModelPolicyPrecedenceAndMessages(t *testing.T) {
	c := policyConfig(t)
	cases := []struct {
		repo, agent, model string
		want               string // "" permitted, else a substring of the refusal
	}{
		{"api", "claude", "sonnet", ""},
		{"api", "claude", "opus", `denied by projects.shop.models.deny.claude ("opus*")`},
		{"api", "claude", "mystery", "not in models.claude.catalogue"},
		{"tool", "claude", "opus", ""}, // not in the project
		{"api", "opencode", "qwen3-coder:30b", ""},
		{"api", "opencode", "local/other", `repos.api.agent.models.deny.opencode ("*other")`},
		{"api", "opencode", "bench/qwen", "not in models.opencode.catalogue"},
		{"api", "copilot", "anything", ""}, // no catalogue, no rule
	}
	for _, tc := range cases {
		err := c.ModelPolicy(tc.repo, tc.agent).Check(tc.model)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s/%s %s: %v", tc.repo, tc.agent, tc.model, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s/%s %s: err = %v, want %q", tc.repo, tc.agent, tc.model, err, tc.want)
		}
	}
	err := c.ModelPolicy("api", "claude").Check("opus")
	if !strings.Contains(err.Error(), "permitted: haiku, sonnet") {
		t.Errorf("the refusal lists what is permitted: %v", err)
	}
}

func TestModelPolicyAllowNarrowsAndCannotWiden(t *testing.T) {
	c, err := Parse([]byte(`
models:
  claude:
    catalogue: {a: {}, b: {}, c: {}}
projects:
  p:
    repos: [r]
    models: {allow: {claude: [a, b]}}
defaults:
  agent:
    models: {allow: {claude: [a, b]}}
repos:
  r:
    agent:
      models: {allow: {claude: [b, c]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	p := c.ModelPolicy("r", "claude")
	if got := strings.Join(p.Permitted(), ","); got != "b" {
		t.Errorf("permitted = %q: each level narrows, the repo cannot re-allow c", got)
	}
	if why := p.Removed()["c"]; !strings.Contains(why, "not in projects.p.models.allow.claude") {
		t.Errorf("c is removed by the project's allow: %q", why)
	}
	if got := c.Profile("r").Agent.Models.Allow["claude"]; len(got) != 1 || got[0] != "b" {
		t.Errorf("a merged profile's allow is the intersection: %v", got)
	}
}

func TestResolveFallsBackFromRemovedDefault(t *testing.T) {
	c := policyConfig(t)
	// tool's own default is opus; nothing denies it there.
	if m, note, err := c.ModelPolicy("tool", "claude").Resolve("", ""); err != nil || m != "opus" || note != "" {
		t.Errorf("tool = %q %q %v", m, note, err)
	}
	// Deny the repo's default at the project: the workspace default takes over, with a note.
	c.Projects["shop"] = Project{Repos: []string{"api", "web", "tool"}, Models: ModelRules{Deny: map[string][]string{"claude": {"opus"}}}}
	m, note, err := c.ModelPolicy("tool", "claude").Resolve("", "")
	if err != nil || m != "sonnet" || !strings.Contains(note, "denied") {
		t.Errorf("fallback = %q %q %v", m, note, err)
	}
	// A model asked for is refused, never swapped.
	if _, _, err := c.ModelPolicy("tool", "claude").Resolve("opus", ""); err == nil {
		t.Error("an explicit denied model must be refused")
	}
	// An inherited one falls back like a default.
	if m, _, err := c.ModelPolicy("tool", "claude").Resolve("", "opus"); err != nil || m != "sonnet" {
		t.Errorf("inherited = %q %v", m, err)
	}
	// With a catalogue and nothing to fall back to, the run is refused: no silent pick.
	delete(c.Models["claude"].Catalogue, "sonnet")
	cl := c.Models["claude"]
	cl.Default = ""
	c.Models["claude"] = cl
	if _, _, err := c.ModelPolicy("api", "claude").Resolve("", ""); err == nil {
		t.Error("a catalogue with no default and no repo model must refuse")
	}
}

func TestResolveEffort(t *testing.T) {
	c := policyConfig(t)
	p := c.ModelPolicy("api", "claude") // repo effort low
	if e, n, err := p.ResolveEffort("sonnet", ""); e != "low" || n != "" || err != nil {
		t.Errorf("repo effort = %q %q %v", e, n, err)
	}
	if e, _, _ := c.ModelPolicy("web", "claude").ResolveEffort("sonnet", ""); e != "high" {
		t.Errorf("the model's own effort beats the workspace's: %q", e)
	}
	if e, _, _ := c.ModelPolicy("web", "claude").ResolveEffort("haiku", ""); e != "" {
		t.Errorf("haiku takes none, so the default is dropped: %q", e)
	}
	if e, n, err := p.ResolveEffort("haiku", "high"); e != "" || !strings.Contains(n, "takes none") || err != nil {
		t.Errorf("asked of a model with none: %q %q %v", e, n, err)
	}
	if _, _, err := p.ResolveEffort("sonnet", "max"); err == nil || !strings.Contains(err.Error(), "low, medium, high") {
		t.Errorf("a value the model does not list is refused: %v", err)
	}
	// No catalogue entry: the value passes through for the CLI to judge.
	if e, _, _ := c.ModelPolicy("api", "claude").ResolveEffort("unknown", "xhigh"); e != "xhigh" {
		t.Errorf("unlisted model: %q", e)
	}
}

func TestModelValidation(t *testing.T) {
	bad := map[string]string{
		"models:\n  gemini: {default: x}\n":                                        "not an agent",
		"models:\n  claude:\n    catalogue: {a: {}}\n    default: b\n":             "not in models.claude.catalogue",
		"models:\n  claude:\n    effort: \"very high\"\n":                          "not an effort value",
		"models:\n  claude:\n    catalogue: {a: {effort: high, efforts: [low]}}\n": "not in its efforts",
		"defaults:\n  agent:\n    models: {deny: {nope: [x]}}\n":                   "not an agent",
		"repos:\n  r:\n    agent:\n      effort: {claude: \" high\"}\n":            "not an effort value",
	}
	for src, want := range bad {
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", src, err, want)
		}
	}
}

func TestRemovedDefaultIsAWarningNotAnError(t *testing.T) {
	_, warns, err := ParseWarn([]byte(`
models:
  claude: {catalogue: {sonnet: {}, opus: {}}, default: sonnet}
defaults:
  agent:
    model: {claude: sonnet}
    models: {deny: {claude: [sonnet]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "default removed") || !strings.Contains(warns[0], "defaults.agent.models.deny.claude") {
		t.Errorf("warnings = %q", warns)
	}
}

func TestTodaysAgentModelMapsLoadUnchanged(t *testing.T) {
	c, warns, err := ParseWarn([]byte("defaults:\n  agent:\n    model: {claude: sonnet, opencode: \"qwen3-coder:30b\"}\n"))
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v %q", err, warns)
	}
	p := c.ModelPolicy("any", "opencode")
	if p.Catalogued || p.Default != "qwen3-coder:30b" {
		t.Errorf("policy = %+v", p)
	}
	if err := p.Check("whatever/else"); err != nil {
		t.Errorf("no policy means no restriction: %v", err)
	}
	if m, _, err := p.Resolve("", ""); err != nil || m != "qwen3-coder:30b" {
		t.Errorf("resolve = %q %v", m, err)
	}
}
