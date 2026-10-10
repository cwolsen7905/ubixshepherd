package standards

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/scope"
)

// DefaultBudget is the most a digest spends on rules, in bytes. Rules past it are listed
// by id and path rather than inlined.
const DefaultBudget = 2400

// alwaysTags are active in every lane: every lane commits and is shipped.
var alwaysTags = []string{"commit", "ship"}

// tagGlobs says which paths make a lane do a kind of work, for rules tagged with it.
var tagGlobs = map[string][]string{
	"test":      {"**/*_test.*", "**/*.test.*", "**/*.spec.*", "test/**", "tests/**", "**/test/**", "**/tests/**", "**/__tests__/**"},
	"docs":      {"docs/**", "**/*.md", "**/*.rst"},
	"migration": {"**/migrations/**", "**/migrate/**", "**/*.sql"},
}

// Lane is what decides which rules apply: where it may change files, and which files
// the repo has.
type Lane struct {
	Scope []string
	// Files are the repo's tracked files, repo-relative with "/".
	Files []string
	// Tags are extra tags active in every lane of the repo (the profile's).
	Tags []string
}

// reaches reports whether any of globs and the lane's scope claim a path.
func (l Lane) reaches(globs []string) bool {
	return len(scope.Overlap(l.Scope, globs, l.Files)) > 0
}

// ActiveTags are the tags in force in the lane.
func (l Lane) ActiveTags() map[string]bool {
	out := map[string]bool{}
	for _, t := range alwaysTags {
		out[t] = true
	}
	for _, t := range l.Tags {
		out[t] = true
	}
	for t, globs := range tagGlobs {
		if l.reaches(globs) {
			out[t] = true
		}
	}
	return out
}

// Applies reports whether the rule is relevant to the lane: its applies_to globs reach
// the lane's scope, and one of its when tags is active; no globs or tags means always.
func (r Rule) Applies(l Lane) bool {
	if len(r.AppliesTo) > 0 && !l.reaches(r.AppliesTo) {
		return false
	}
	if len(r.When) > 0 {
		active := l.ActiveTags()
		for _, t := range r.When {
			if active[t] {
				return true
			}
		}
		return false
	}
	return true
}

// ExistingDocs are the configured standards docs that exist in the worktree, as given.
func ExistingDocs(worktree string, docs []string) []string {
	var out []string
	for _, d := range docs {
		if _, err := statPath(filepath.Join(worktree, filepath.FromSlash(d))); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// DigestOptions shape a digest.
type DigestOptions struct {
	// Docs are the repo's own standards, which win over the digest.
	Docs []string
	// RepoPackDir is the repo-relative directory of the repo's own pack, for pointers.
	RepoPackDir string
	// Budget is DefaultBudget when zero.
	Budget int
}

const maxShould = 160

// Digest renders the brief's section: plain text, the same for every agent. Matching
// must rules are listed with their statement, matching should rules as one clipped
// line, and the rest only as a pointer to where the packs are. Rationale is never
// included. It returns "" when there is nothing to say.
func Digest(s Set, l Lane, o DigestOptions) string {
	var match, other []Rule
	for _, r := range s.Rules {
		if r.Applies(l) {
			match = append(match, r)
		} else {
			other = append(other, r)
		}
	}
	if len(match) == 0 && len(other) == 0 && len(o.Docs) == 0 {
		return ""
	}
	budget := o.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	var b strings.Builder
	b.WriteString("Standards. The repo's own standards take precedence over the Shepherd digest below: where they disagree, follow the repo's, and mention the conflict in your report; do not stop to ask.\n")
	if len(o.Docs) > 0 {
		fmt.Fprintf(&b, "The repo's own standards: %s. Read the ones that touch your task.\n", strings.Join(o.Docs, ", "))
	}
	var over []Rule
	used := 0
	if len(match) > 0 {
		b.WriteString("Shepherd's rules for this lane:\n")
	}
	for _, r := range match {
		line := ruleLine(r)
		if used+len(line) > budget {
			over = append(over, r)
			continue
		}
		used += len(line)
		b.WriteString(line)
	}
	if len(over) > 0 {
		b.WriteString("More rules that apply, by id (the file has the statement): ")
		var ids []string
		for _, r := range over {
			ids = append(ids, fmt.Sprintf("%s (%s)", r.ID, r.Path))
		}
		b.WriteString(strings.Join(ids, ", ") + "\n")
	}
	if len(other) > 0 {
		fmt.Fprintf(&b, "%d more rule(s) do not apply to this lane's scope or work; they are in: %s\n", len(other), strings.Join(s.PackDirs, ", "))
	}
	if len(s.Disabled) > 0 {
		fmt.Fprintf(&b, "The repo turned off these Shepherd rules: %s\n", strings.Join(s.Disabled, ", "))
	}
	return b.String()
}

func ruleLine(r Rule) string {
	if r.Level == Must {
		return fmt.Sprintf("- MUST %s: %s\n", r.ID, r.Statement)
	}
	st := r.Statement
	if len(st) > maxShould {
		st = strings.TrimSpace(st[:maxShould-3]) + "..."
	}
	return fmt.Sprintf("- should %s: %s\n", r.ID, st)
}
