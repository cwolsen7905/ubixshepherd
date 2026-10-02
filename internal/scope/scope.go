// Package scope matches repo paths against a lane's scope: globs relative to the repo,
// with "/" separators on every OS.
//
//	README.md          that file, or everything below it if it is a directory
//	docs/*.md          * matches within one path segment
//	internal/lease/**  ** matches any number of segments, including none
package scope

import (
	"path"
	"strings"
)

// Match reports whether file (repo-relative, "/"-separated) is inside glob.
func Match(glob, file string) bool {
	glob, file = path.Clean(glob), path.Clean(file)
	if !hasMeta(glob) {
		return file == glob || strings.HasPrefix(file, glob+"/")
	}
	return matchSegs(strings.Split(glob, "/"), strings.Split(file, "/"))
}

// Any reports whether file is inside any of the globs.
func Any(globs []string, file string) bool {
	for _, g := range globs {
		if Match(g, file) {
			return true
		}
	}
	return false
}

func matchSegs(g, f []string) bool {
	for len(g) > 0 {
		if g[0] == "**" {
			// ** at the end matches the rest; otherwise try every split point.
			if len(g) == 1 {
				return true
			}
			for i := 0; i <= len(f); i++ {
				if matchSegs(g[1:], f[i:]) {
					return true
				}
			}
			return false
		}
		if len(f) == 0 {
			return false
		}
		if ok, err := path.Match(g[0], f[0]); err != nil || !ok {
			return false
		}
		g, f = g[1:], f[1:]
	}
	return len(f) == 0
}

func hasMeta(s string) bool { return strings.ContainsAny(s, `*?[\`) }

// prefix is the glob's leading segments with no wildcard: the directory it is confined to.
func prefix(glob string) string {
	segs := strings.Split(path.Clean(glob), "/")
	var out []string
	for _, s := range segs {
		if hasMeta(s) {
			break
		}
		out = append(out, s)
	}
	return strings.Join(out, "/")
}

// within reports whether p is dir or below it ("" is the repo root, which holds all).
func within(dir, p string) bool {
	return dir == "" || p == dir || strings.HasPrefix(p, dir+"/")
}

// Overlap returns the paths two scopes both claim, among the repo's files, plus a
// description of any overlap between globs that no file shows yet. It errs towards
// reporting: two lanes refused wrongly is a message, two lanes allowed wrongly is a
// collision.
func Overlap(a, b []string, files []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, f := range files {
		if Any(a, f) && Any(b, f) {
			add(f)
		}
	}
	// Paths that do not exist yet: a literal that the other side's glob would match, or
	// two globs confined to the same directory where one reaches into the other.
	for _, ga := range a {
		for _, gb := range b {
			switch {
			case !hasMeta(ga) && Match(gb, ga), !hasMeta(gb) && Match(ga, gb):
				add(shorter(ga, gb))
			case hasMeta(ga) && hasMeta(gb):
				pa, pb := prefix(ga), prefix(gb)
				if (within(pa, pb) && reaches(ga, pb)) || (within(pb, pa) && reaches(gb, pa)) {
					add(ga + " and " + gb)
				}
			}
		}
	}
	return out
}

// reaches reports whether glob can match below dir: it contains ** or has segments
// beyond dir. Two globs in the same directory with different single-segment patterns
// (docs/*.md and docs/*.go) are only judged by the files that exist.
func reaches(glob, dir string) bool {
	if strings.Contains(glob, "**") {
		return true
	}
	return prefix(glob) != dir
}

func shorter(a, b string) string {
	if len(a) <= len(b) {
		return a
	}
	return b
}
