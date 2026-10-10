// Package standards reads standards packs: folders of plain markdown rules that every
// lane's agent is told about the same way, whatever the agent.
//
// A pack is a directory; each rule is one file, <id>.md, with YAML front matter:
//
//	---
//	level: must            # must | should
//	check: "! git grep -n TODO -- ':!docs'"   # optional; run in the worktree
//	applies_to: ["src/**"] # optional globs; empty means everywhere
//	when: [commit]         # optional tags; empty means always
//	---
//	One line saying what the rule asks for.
//
//	## Why
//	Rationale, never put in an agent's brief.
//
// Shepherd's packs live under ~/.shepherd/standards/<pack>/. A repo's own pack directory
// (by default .shepherd/standards) and its own docs win over them: see Resolve.
package standards

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Rule levels.
const (
	Must   = "must"
	Should = "should"
)

// RepoPack names the rules that come from the repo's own pack directory.
const RepoPack = "repo"

// Rule is one standard. It is the shape a settings page reads and writes.
type Rule struct {
	ID        string   `json:"id"`
	Level     string   `json:"level"`
	Statement string   `json:"statement"`
	Rationale string   `json:"rationale,omitempty"`
	Check     string   `json:"check,omitempty"`
	AppliesTo []string `json:"applies_to,omitempty"`
	When      []string `json:"when,omitempty"`
	// Disabled, in a repo pack, removes the Shepherd rule with this id.
	Disabled bool `json:"disabled,omitempty"`
	// Pack is the pack the rule came from, or RepoPack; Path is its file.
	Pack string `json:"pack"`
	Path string `json:"path"`
}

type frontMatter struct {
	ID        string   `yaml:"id"`
	Level     string   `yaml:"level"`
	Check     string   `yaml:"check"`
	AppliesTo []string `yaml:"applies_to"`
	When      []string `yaml:"when"`
	Disabled  bool     `yaml:"disabled"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidID says whether s is a usable rule or pack id: lower-case words joined by dashes.
func ValidID(s string) bool { return idPattern.MatchString(s) }

// Parse reads one rule file. stem is the filename without ".md"; the id is the stem,
// and a front matter id that differs is an error, so a rule's file and id cannot drift.
func Parse(stem string, data []byte) (Rule, error) {
	var r Rule
	if !ValidID(stem) {
		return r, fmt.Errorf("%q is not a rule id: use lower-case words joined by dashes", stem)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	var fm frontMatter
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		head, after, found := strings.Cut(rest, "\n---")
		if !found {
			return r, fmt.Errorf("front matter is not closed with ---")
		}
		dec := yaml.NewDecoder(strings.NewReader(head))
		dec.KnownFields(true)
		if err := dec.Decode(&fm); err != nil && err.Error() != "EOF" {
			return r, fmt.Errorf("front matter: %w", err)
		}
		body = strings.TrimPrefix(after, "\n")
	}
	if fm.ID != "" && fm.ID != stem {
		return r, fmt.Errorf("id %q does not match the file name %q", fm.ID, stem)
	}
	r = Rule{ID: stem, Level: fm.Level, Check: strings.TrimSpace(fm.Check),
		AppliesTo: fm.AppliesTo, When: fm.When, Disabled: fm.Disabled}
	r.Statement, r.Rationale = splitBody(body)
	if r.Disabled {
		return r, nil
	}
	if r.Level != Must && r.Level != Should {
		return r, fmt.Errorf("level %q is not must or should", r.Level)
	}
	if r.Statement == "" {
		return r, fmt.Errorf("no statement: the first paragraph of the file says what the rule asks")
	}
	for _, t := range r.When {
		if !ValidID(t) {
			return r, fmt.Errorf("when tag %q: use lower-case words joined by dashes", t)
		}
	}
	return r, nil
}

// splitBody returns the first paragraph, on one line, and the text under "## Why".
func splitBody(body string) (statement, why string) {
	lines := strings.Split(body, "\n")
	var st []string
	i := 0
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.EqualFold(l, "## why") {
			break
		}
		if l == "" {
			if len(st) > 0 {
				break
			}
			continue
		}
		st = append(st, l)
	}
	for ; i < len(lines); i++ {
		if strings.EqualFold(strings.TrimSpace(lines[i]), "## why") {
			why = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			break
		}
	}
	return strings.Join(st, " "), why
}

// Render writes a rule as a file, the inverse of Parse.
func (r Rule) Render() []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fm := map[string]any{}
	if r.Disabled {
		fm["disabled"] = true
	} else {
		fm["level"] = r.Level
	}
	if r.Check != "" {
		fm["check"] = r.Check
	}
	if len(r.AppliesTo) > 0 {
		fm["applies_to"] = r.AppliesTo
	}
	if len(r.When) > 0 {
		fm["when"] = r.When
	}
	out, _ := yaml.Marshal(fm)
	b.Write(out)
	b.WriteString("---\n")
	if r.Statement != "" {
		b.WriteString(r.Statement + "\n")
	}
	if r.Rationale != "" {
		b.WriteString("\n## Why\n\n" + r.Rationale + "\n")
	}
	return []byte(b.String())
}

// LoadDir reads every rule file in a pack directory, sorted by id. A file that does not
// parse is skipped and named in problems, so one bad rule never hides the others. A
// missing directory is an error the caller may treat as "no rules".
func LoadDir(dir, pack string) (rules []Rule, problems []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		stem, ok := strings.CutSuffix(name, ".md")
		if e.IsDir() || !ok || name == "pack.md" || strings.HasPrefix(name, ".") || strings.EqualFold(stem, "readme") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		r, err := Parse(stem, data)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		r.Pack, r.Path = pack, path
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules, problems, nil
}

// Set is the effective rules of a repo.
type Set struct {
	// Rules are in force: Shepherd's packs in the order the profile lists them, then the
	// repo's, with overrides applied; must before should, then by id.
	Rules []Rule `json:"rules"`
	// Disabled are the ids a repo pack switched off.
	Disabled []string `json:"disabled,omitempty"`
	// PackDirs are the directories rules came from, for pointers in the brief.
	PackDirs []string `json:"pack_dirs,omitempty"`
	// Problems are packs or rules that could not be read; the rest still apply.
	Problems []string `json:"problems,omitempty"`
}

// Resolve merges the named packs under root (Shepherd's standards directory), then the
// repo's own pack directory repoDir (an absolute path; "" for none). The repo wins: a
// repo rule with the same id replaces a Shepherd rule, and a repo rule marked disabled
// removes it. A later Shepherd pack replaces an earlier one's rule of the same id.
func Resolve(root string, packs []string, repoDir string) Set {
	var s Set
	byID := map[string]Rule{}
	for _, p := range packs {
		if !ValidID(p) {
			s.Problems = append(s.Problems, fmt.Sprintf("pack %q: not a pack name", p))
			continue
		}
		dir := filepath.Join(root, p)
		rules, probs, err := LoadDir(dir, p)
		if err != nil {
			s.Problems = append(s.Problems, fmt.Sprintf("pack %s: %v", p, err))
			continue
		}
		s.PackDirs = append(s.PackDirs, dir)
		s.Problems = append(s.Problems, probs...)
		for _, r := range rules {
			byID[r.ID] = r
		}
	}
	if repoDir != "" {
		rules, probs, err := LoadDir(repoDir, RepoPack)
		if err == nil {
			s.PackDirs = append(s.PackDirs, repoDir)
			s.Problems = append(s.Problems, probs...)
			for _, r := range rules {
				if r.Disabled {
					delete(byID, r.ID)
					s.Disabled = append(s.Disabled, r.ID)
					continue
				}
				byID[r.ID] = r
			}
		} else if !os.IsNotExist(err) {
			s.Problems = append(s.Problems, fmt.Sprintf("repo standards %s: %v", repoDir, err))
		}
	}
	for _, r := range byID {
		s.Rules = append(s.Rules, r)
	}
	sort.Slice(s.Rules, func(i, j int) bool {
		a, b := s.Rules[i], s.Rules[j]
		if a.Level != b.Level {
			return a.Level == Must
		}
		return a.ID < b.ID
	})
	slices.Sort(s.Disabled)
	return s
}
