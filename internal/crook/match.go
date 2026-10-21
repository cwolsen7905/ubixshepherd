package crook

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// globs follow crook/1: * stays inside one path segment, ** crosses segments.
var (
	globMu    sync.Mutex
	globCache = map[string]*regexp.Regexp{}
)

func compileGlob(g string) (*regexp.Regexp, error) {
	globMu.Lock()
	defer globMu.Unlock()
	if re, ok := globCache[g]; ok {
		return re, nil
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); {
		switch {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i += 2
		case g[i] == '*':
			b.WriteString("[^/]*")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(g[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}
	globCache[g] = re
	return re, nil
}

func checkGlob(g string) error {
	if g == "" {
		return fmt.Errorf("an empty glob")
	}
	_, err := compileGlob(g)
	return err
}

func globMatch(globs []string, p string) bool {
	for _, g := range globs {
		if re, err := compileGlob(g); err == nil && re.MatchString(p) {
			return true
		}
	}
	return false
}

// Facts the format reads arrive as JSON-shaped values, so these read them leniently by
// type and treat a missing or mistyped fact as not matching.
func factString(f map[string]any, k string) (string, bool) {
	s, ok := f[k].(string)
	return s, ok
}

func factStrings(f map[string]any, k string) ([]string, bool) {
	switch v := f[k].(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func factNumber(f map[string]any, k string) (float64, bool) {
	switch v := f[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func factBool(f map[string]any, k string) (bool, bool) {
	b, ok := f[k].(bool)
	return b, ok
}

// matches reports whether every matcher set in w holds for the facts, the action's
// classes and the time.
func (w *When) matches(facts map[string]any, classes []string, now time.Time) bool {
	if len(w.Repo) > 0 {
		if s, ok := factString(facts, "repo"); !ok || !slices.Contains(w.Repo, s) {
			return false
		}
	}
	if len(w.Paths) > 0 {
		ps, _ := factStrings(facts, "paths")
		if !slices.ContainsFunc(ps, func(p string) bool { return globMatch(w.Paths, p) }) {
			return false
		}
	}
	if len(w.PathsAll) > 0 {
		ps, _ := factStrings(facts, "paths")
		if len(ps) == 0 {
			return false
		}
		for _, p := range ps {
			if !globMatch(w.PathsAll, p) {
				return false
			}
		}
	}
	if len(w.Branch) > 0 {
		if s, ok := factString(facts, "branch"); !ok || !globMatch(w.Branch, s) {
			return false
		}
	}
	if len(w.Class) > 0 && !slices.ContainsFunc(w.Class, func(c string) bool { return slices.Contains(classes, c) }) {
		return false
	}
	if len(w.DecisionAbout) > 0 {
		if s, ok := factString(facts, "decision_about"); !ok || !slices.Contains(w.DecisionAbout, s) {
			return false
		}
	}
	if len(w.FailingChecksOnly) > 0 {
		fails, ok := factStrings(facts, "failing_checks")
		if !ok {
			return false
		}
		for _, c := range fails {
			if !slices.Contains(w.FailingChecksOnly, c) {
				return false
			}
		}
	}
	if w.ChangedLinesMax != nil {
		if n, ok := factNumber(facts, "changed_lines"); !ok || n > float64(*w.ChangedLinesMax) {
			return false
		}
	}
	if w.CostUSDMax != nil {
		if n, ok := factNumber(facts, "cost_usd"); !ok || n > *w.CostUSDMax {
			return false
		}
	}
	if w.Time != "" && !inWindow(w.Time, now) {
		return false
	}
	if w.ItemLinked != nil {
		if b, ok := factBool(facts, "item_linked"); !ok || b != *w.ItemLinked {
			return false
		}
	}
	if len(w.Requirement) > 0 {
		ids, _ := factStrings(facts, "requirement")
		if !slices.ContainsFunc(ids, func(id string) bool { return globMatch(w.Requirement, id) }) {
			return false
		}
	}
	return true
}

// inWindow reports whether the local time is in HH:MM-HH:MM; the end is exclusive and a
// window whose end is before its start wraps midnight.
func inWindow(window string, now time.Time) bool {
	var h1, m1, h2, m2 int
	if _, err := fmt.Sscanf(window, "%d:%d-%d:%d", &h1, &m1, &h2, &m2); err != nil {
		return false
	}
	at := now.Hour()*60 + now.Minute()
	from, to := h1*60+m1, h2*60+m2
	switch {
	case from == to:
		return false
	case from < to:
		return at >= from && at < to
	}
	return at >= from || at < to
}
