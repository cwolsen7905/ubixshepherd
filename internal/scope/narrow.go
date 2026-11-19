package scope

import (
	"fmt"
	"path"
	"strings"
)

// Covers reports whether every path inside glob is also inside outer. It is
// conservative: true only when it can tell, so a glob it cannot prove is a subset is
// treated as new ground.
func Covers(outer, glob string) bool {
	outer, glob = path.Clean(outer), path.Clean(glob)
	if !hasMeta(outer) {
		// A plain path holds itself and everything below; every match of glob starts
		// with glob's wildcard-free leading directory.
		return within(outer, prefix(glob))
	}
	gs := strings.Split(glob, "/")
	if !hasMeta(glob) {
		// A plain path also holds everything below it.
		gs = append(gs, "**")
	}
	return coverSegs(strings.Split(outer, "/"), gs)
}

func coverSegs(o, g []string) bool {
	if len(o) == 0 {
		return len(g) == 0
	}
	if o[0] == "**" {
		if len(o) == 1 {
			return true
		}
		// ** takes no segment, or absorbs one of g's (including a g "**": o's takes any number).
		if coverSegs(o[1:], g) {
			return true
		}
		return len(g) > 0 && coverSegs(o, g[1:])
	}
	if len(g) == 0 || g[0] == "**" {
		return false
	}
	if o[0] != g[0] && o[0] != "*" {
		if hasMeta(g[0]) {
			return false
		}
		if ok, err := path.Match(o[0], g[0]); err != nil || !ok {
			return false
		}
	}
	return coverSegs(o[1:], g[1:])
}

// Narrow returns a lane's scope after dropping globs and adding replacements. It can
// only shrink the scope: every drop must be a glob the scope holds, and every
// replacement must lie inside the scope as it was before, so no new ground is claimed.
// The result is never empty.
func Narrow(current, drop, to []string) ([]string, error) {
	if len(drop) == 0 && len(to) == 0 {
		return nil, fmt.Errorf("name globs to drop, or a subset to narrow to")
	}
	have := map[string]bool{}
	for _, g := range current {
		have[g] = true
	}
	dropped := map[string]bool{}
	for _, d := range drop {
		if !have[d] {
			return nil, fmt.Errorf("cannot drop %q: it is not in the lane's scope (%s)", d, strings.Join(current, ", "))
		}
		dropped[d] = true
	}
	var next []string
	seen := map[string]bool{}
	keep := func(g string) {
		if !seen[g] {
			seen[g] = true
			next = append(next, g)
		}
	}
	for _, g := range current {
		if !dropped[g] {
			keep(g)
		}
	}
	for _, t := range to {
		if t == "" {
			return nil, fmt.Errorf("empty scope glob")
		}
		ok := false
		for _, g := range current {
			if Covers(g, t) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("narrowing only shrinks a scope: %q is not inside the lane's scope (%s)", t, strings.Join(current, ", "))
		}
		keep(t)
	}
	if len(next) == 0 {
		return nil, fmt.Errorf("a lane needs a scope: that would leave none")
	}
	return next, nil
}

// PlanScope reads the scope a plan document names: the first line of the form
// "Scope: internal/x/**, docs/y.md" (the label may be bold or in a list) whose values
// all look like paths: each has a "/", a ".", or a wildcard. A prose line such as
// "Scope: see the section below" is skipped, and so is a line that mixes the two.
// It returns nil when the text names none.
func PlanScope(text string) []string {
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimLeft(strings.TrimSpace(line), "-*> ")
		i := strings.Index(l, ":")
		if i < 0 || !strings.EqualFold(strings.Trim(l[:i], "*_ "), "scope") {
			continue
		}
		rest := strings.TrimLeft(l[i+1:], "* ")
		var out []string
		for _, g := range strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if g = strings.Trim(g, "`"); g != "" {
				out = append(out, g)
			}
		}
		if len(out) > 0 && allPathLike(out) {
			return out
		}
	}
	return nil
}

func allPathLike(globs []string) bool {
	for _, g := range globs {
		if !strings.ContainsAny(g, "/.*?[") {
			return false
		}
	}
	return true
}
