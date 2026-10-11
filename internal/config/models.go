package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ModelEntry is one model in an agent's catalogue: what the front desk reads to choose.
type ModelEntry struct {
	// Use is a line on what the model is for.
	Use string `yaml:"use,omitempty" json:"use,omitempty"`
	// Efforts are the effort values the model takes, in the agent's own words (Claude
	// Code: low, medium, high, xhigh, max). Empty means the model takes none, and an
	// effort asked of it is not applied.
	Efforts []string `yaml:"efforts,omitempty" json:"efforts,omitempty"`
	// Effort is the effort this model runs at when none is asked and the repo sets none.
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// AgentModels is the workspace's model policy for one agent (models.<agent>).
type AgentModels struct {
	// Default is the model a run gets when it asks for none and the repo's agent.model
	// names none (or names one a rule removes).
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
	// Effort is the effort a run gets when none is asked and nothing narrower sets one.
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// Catalogue is the allow-list: once set, a model outside it is refused. Names are
	// as the CLI takes them.
	Catalogue map[string]ModelEntry `yaml:"catalogue,omitempty" json:"catalogue,omitempty"`
	// Deny removes models from the catalogue everywhere (names or * globs).
	Deny []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// ModelRules narrow what a project or a repo may run, by agent. Allow, when set for an
// agent, is the only models it may use; Deny removes models. Deny wins over Allow, and
// a level can only narrow what the wider ones left.
type ModelRules struct {
	Allow map[string][]string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny  map[string][]string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// Empty says no rule is set.
func (m ModelRules) Empty() bool { return len(m.Allow) == 0 && len(m.Deny) == 0 }

// mergeRules narrows base by over: denies add up, and allows intersect.
func mergeRules(base, over ModelRules) ModelRules {
	if over.Empty() {
		return base
	}
	out := ModelRules{Deny: map[string][]string{}, Allow: map[string][]string{}}
	for _, r := range []ModelRules{base, over} {
		for a, l := range r.Deny {
			out.Deny[a] = append(out.Deny[a], l...)
		}
	}
	for a, l := range base.Allow {
		out.Allow[a] = l
	}
	for a, l := range over.Allow {
		if cur, ok := out.Allow[a]; ok {
			var both []string
			for _, m := range l {
				if slices.Contains(cur, m) {
					both = append(both, m)
				}
			}
			l = both
		}
		out.Allow[a] = l
	}
	if len(out.Deny) == 0 {
		out.Deny = nil
	}
	if len(out.Allow) == 0 {
		out.Allow = nil
	}
	return out
}

// globMatch says name matches pattern, where * stands for any run of characters.
func globMatch(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(name)
}

// nameForms are the spellings a model may be listed under. OpenCode runs a bare name
// under the provider "local", so the two spellings are one model there.
func nameForms(agent, name string) []string {
	if agent != "opencode" {
		return []string{name}
	}
	if rest, ok := strings.CutPrefix(name, "local/"); ok {
		return []string{name, rest}
	}
	if !strings.Contains(name, "/") {
		return []string{name, "local/" + name}
	}
	return []string{name}
}

func matchAny(agent string, patterns []string, name string) (string, bool) {
	for _, p := range patterns {
		for _, f := range nameForms(agent, name) {
			if globMatch(p, f) {
				return p, true
			}
		}
	}
	return "", false
}

// level is one layer of the policy, named for messages.
type level struct {
	at    string
	allow []string
	deny  []string
}

// AgentPolicy is what one repo may run on one agent.
type AgentPolicy struct {
	Agent, Repo string
	// Catalogued says the workspace lists this agent's models: a model outside it is
	// refused.
	Catalogued bool
	catalogue  map[string]ModelEntry
	levels     []level
	// Default and Effort are the repo's defaults before any rule: its agent.model and
	// agent.effort for the agent, else the workspace's.
	Default, DefaultAt string
	// Effort is the default effort shown for the agent: the repo's, else the workspace's.
	Effort string
	// wsDefault, repoEffort and wsEffort are the pieces Resolve and ResolveEffort use.
	wsDefault, repoEffort, wsEffort string
}

// ModelPolicy is the policy for repo on agent: the workspace catalogue and denies, then
// the project's rules, the defaults profile's and the repo's own.
func (c Config) ModelPolicy(repo, agent string) AgentPolicy {
	p := AgentPolicy{Agent: agent, Repo: repo}
	ws, have := c.Models[agent]
	if have {
		p.catalogue = ws.Catalogue
		p.Catalogued = len(ws.Catalogue) > 0
		if len(ws.Deny) > 0 {
			p.levels = append(p.levels, level{at: "models." + agent + ".deny", deny: ws.Deny})
		}
	}
	add := func(at string, r ModelRules) {
		if r.Empty() {
			return
		}
		p.levels = append(p.levels, level{at: at, allow: r.Allow[agent], deny: r.Deny[agent]})
	}
	if name, ok := c.ProjectOf(repo); ok {
		add("projects."+name+".models", c.Projects[name].Models)
	}
	add("defaults.agent.models", c.Defaults.Agent.Models)
	if r, ok := c.Repos[repo]; ok {
		add("repos."+repo+".agent.models", r.Agent.Models)
	}
	prof := c.Profile(repo)
	switch {
	case prof.Agent.Model[agent] != "":
		p.Default, p.DefaultAt = prof.Agent.Model[agent], "agent.model"
	case ws.Default != "":
		p.Default, p.DefaultAt = ws.Default, "models."+agent+".default"
	}
	p.wsDefault, p.repoEffort, p.wsEffort = ws.Default, prof.Agent.Effort[agent], ws.Effort
	p.Effort = p.repoEffort
	if p.Effort == "" {
		p.Effort = ws.Effort
	}
	return p
}

// Check says why model may not run, naming the rule, or nil when it may.
func (p AgentPolicy) Check(model string) error {
	if why := p.removedBy(model); why != "" {
		if model == "" {
			return fmt.Errorf("no model for %s in repo %s: %s%s", p.Agent, p.Repo, why, p.permittedHint())
		}
		return fmt.Errorf("model %q is not permitted for %s in repo %s: %s%s", model, p.Agent, p.Repo, why, p.permittedHint())
	}
	return nil
}

func (p AgentPolicy) removedBy(model string) string {
	if p.Catalogued {
		if model == "" {
			return "no model was chosen and models." + p.Agent + ".default names none"
		}
		if _, ok := matchAny(p.Agent, keys(p.catalogue), model); !ok {
			return "it is not in models." + p.Agent + ".catalogue"
		}
	}
	for _, l := range p.levels {
		if len(l.allow) > 0 {
			if model == "" {
				return "no model was chosen, and " + l.at + ".allow." + p.Agent + " limits the models"
			}
			if _, ok := matchAny(p.Agent, l.allow, model); !ok {
				return "it is not in " + l.at + ".allow." + p.Agent
			}
		}
		if pat, ok := matchAny(p.Agent, l.deny, model); ok && model != "" {
			return fmt.Sprintf("denied by %s.deny.%s (%q)", l.at, p.Agent, pat)
		}
	}
	return ""
}

func (p AgentPolicy) permittedHint() string {
	if m := p.Permitted(); len(m) > 0 {
		return "; permitted: " + strings.Join(m, ", ")
	}
	return ""
}

// Permitted lists the catalogued models that may run, in name order. Without a
// catalogue it is empty: the rules still apply to whatever name is asked for.
func (p AgentPolicy) Permitted() []string {
	var out []string
	for _, m := range keys(p.catalogue) {
		if p.removedBy(m) == "" {
			out = append(out, m)
		}
	}
	return out
}

// Removed maps each catalogued model a rule removes to the reason.
func (p AgentPolicy) Removed() map[string]string {
	out := map[string]string{}
	for _, m := range keys(p.catalogue) {
		if why := p.removedBy(m); why != "" {
			out[m] = why
		}
	}
	return out
}

// Entry is the catalogue entry for a model.
func (p AgentPolicy) Entry(model string) (ModelEntry, bool) {
	for _, f := range nameForms(p.Agent, model) {
		if e, ok := p.catalogue[f]; ok {
			return e, true
		}
	}
	return ModelEntry{}, false
}

// Resolve picks the model of a run: asked, else (for a continued run) the model it
// inherits, else the repo's default, else the workspace's. A model that was asked for
// is refused when a rule removes it. One that was not falls through to the next
// candidate and says so in note; when none remains the run is refused.
func (p AgentPolicy) Resolve(asked, inherited string) (model, note string, err error) {
	if asked != "" {
		return asked, "", p.Check(asked)
	}
	var cands []string
	for _, m := range []string{inherited, p.Default, p.wsDefault} {
		if m != "" && !slices.Contains(cands, m) {
			cands = append(cands, m)
		}
	}
	var first error
	for _, m := range cands {
		e := p.Check(m)
		if e == nil {
			if first != nil {
				note = fmt.Sprintf("model %s used: %v", m, first)
			}
			return m, note, nil
		}
		if first == nil {
			first = e
		}
	}
	if first != nil {
		return "", "", first
	}
	// No candidate: the agent's own default, which a catalogue or an allow-list cannot vouch for.
	return "", "", p.Check("")
}

// Effort picks a run's effort for model: asked, else the repo's, else the model's own,
// else the workspace's. applied is "" when the model takes none; note says why an
// effort that was asked for is not applied. A value the model does not list is an
// error when asked for.
func (p AgentPolicy) ResolveEffort(model, asked string) (effort, note string, err error) {
	e, listed := p.Entry(model)
	if asked != "" {
		switch {
		case listed && len(e.Efforts) == 0:
			return "", fmt.Sprintf("effort %s not applied: %s takes none", asked, model), nil
		case listed && !slices.Contains(e.Efforts, asked):
			return "", "", fmt.Errorf("effort %q is not one %s takes on %s (%s)", asked, model, p.Agent, strings.Join(e.Efforts, ", "))
		}
		return asked, "", nil
	}
	eff := p.repoEffort
	if eff == "" && listed {
		eff = e.Effort
	}
	if eff == "" {
		eff = p.wsEffort
	}
	if listed && (len(e.Efforts) == 0 || !slices.Contains(e.Efforts, eff)) {
		return "", "", nil
	}
	return eff, "", nil
}

func keys(m map[string]ModelEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// badEffort says an effort value is not one: blank, or with spaces inside.
func badEffort(e string) bool { return e != strings.TrimSpace(e) || strings.ContainsAny(e, " \t\n") }

// validateModels checks the workspace policy, and every default a rule removes.
func (c Config) validateModels() []error {
	var errs []error
	agents := map[string]bool{"claude": true, "copilot": true, "cursor": true, "opencode": true}
	names := make([]string, 0, len(c.Models))
	for a := range c.Models {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		m := c.Models[a]
		at := "models." + a
		if !agents[a] {
			errs = append(errs, fmt.Errorf("models: %q is not an agent (claude, copilot, cursor or opencode)", a))
			continue
		}
		if badModel(m.Default) {
			errs = append(errs, fmt.Errorf("%s.default: %q is not a model name", at, m.Default))
		}
		if badEffort(m.Effort) {
			errs = append(errs, fmt.Errorf("%s.effort: %q is not an effort value", at, m.Effort))
		}
		for _, name := range keys(m.Catalogue) {
			e := m.Catalogue[name]
			if name == "" || badModel(name) {
				errs = append(errs, fmt.Errorf("%s.catalogue: %q is not a model name", at, name))
			}
			for _, v := range append([]string{e.Effort}, e.Efforts...) {
				if badEffort(v) {
					errs = append(errs, fmt.Errorf("%s.catalogue.%s: %q is not an effort value", at, name, v))
				}
			}
			if e.Effort != "" && !slices.Contains(e.Efforts, e.Effort) {
				errs = append(errs, fmt.Errorf("%s.catalogue.%s.effort: %q is not in its efforts", at, name, e.Effort))
			}
		}
		if len(m.Catalogue) > 0 && m.Default != "" {
			if _, ok := (AgentPolicy{Agent: a, catalogue: m.Catalogue}).Entry(m.Default); !ok {
				errs = append(errs, fmt.Errorf("%s.default: %q is not in %s.catalogue", at, m.Default, at))
			}
		}
		for _, p := range m.Deny {
			if p == "" || badModel(p) {
				errs = append(errs, fmt.Errorf("%s.deny: %q is not a model name", at, p))
			}
		}
	}
	check := func(at string, r ModelRules) {
		for _, set := range []struct {
			kind string
			m    map[string][]string
		}{{"allow", r.Allow}, {"deny", r.Deny}} {
			for a, l := range set.m {
				if !agents[a] {
					errs = append(errs, fmt.Errorf("%s.%s: %q is not an agent", at, set.kind, a))
				}
				for _, p := range l {
					if p == "" || badModel(p) {
						errs = append(errs, fmt.Errorf("%s.%s.%s: %q is not a model name", at, set.kind, a, p))
					}
				}
			}
		}
	}
	check("defaults.agent.models", c.Defaults.Agent.Models)
	for name, pr := range c.Projects {
		check("projects."+name+".models", pr.Models)
	}
	for name, r := range c.Repos {
		check("repos."+name+".agent.models", r.Agent.Models)
	}
	for _, name := range c.repoNames() {
		prof := c.Profile(name)
		for _, a := range sortedKeys(prof.Agent.Effort) {
			if badEffort(prof.Agent.Effort[a]) {
				errs = append(errs, fmt.Errorf("repos.%s.agent.effort.%s: %q is not an effort value", name, a, prof.Agent.Effort[a]))
			}
		}
	}
	return errs
}

// ModelWarnings reports each default a rule removes: a repo's agent.model or a
// follow's model that the policy refuses. The config loads (a deny must not stop the
// daemon); a run that would have used the default falls back to the workspace default
// or is refused, naming the rule.
func (c Config) ModelWarnings() []string {
	var out []string
	for _, a := range sortedKeys(c.Defaults.Agent.Model) {
		if err := c.ModelPolicy("", a).Check(c.Defaults.Agent.Model[a]); err != nil {
			out = append(out, fmt.Sprintf("defaults.agent.model.%s: default removed: %v", a, err))
		}
	}
	for _, name := range c.repoNames() {
		prof := c.Profile(name)
		for _, a := range sortedKeys(prof.Agent.Model) {
			if _, own := c.Repos[name].Agent.Model[a]; !own && c.ModelPolicy("", a).Check(prof.Agent.Model[a]) != nil {
				continue // the defaults' entry, already reported above
			}
			if err := c.ModelPolicy(name, a).Check(prof.Agent.Model[a]); err != nil {
				out = append(out, fmt.Sprintf("repos.%s.agent.model.%s: default removed: %v", name, a, err))
			}
		}
		for _, f := range prof.Follows {
			f = f.Effective()
			if f.Model == "" {
				continue
			}
			if err := c.ModelPolicy(name, f.Agent).Check(f.Model); err != nil {
				out = append(out, fmt.Sprintf("repos.%s.follows %s: model removed: %v", name, f.Repo, err))
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// repoNames is every repo with a profile entry or a place in a project.
func (c Config) repoNames() []string {
	seen := map[string]bool{}
	for n := range c.Repos {
		seen[n] = true
	}
	for _, p := range c.Projects {
		for _, r := range p.Repos {
			seen[r] = true
		}
	}
	return sortedKeys(seen)
}
