package update

import (
	"strconv"
	"strings"
)

// semver is a version as semantic versioning 2.0 reads it, with an optional leading "v".
// Build metadata (+...) is dropped: it never changes precedence.
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

// parseSemver reads v1.2.3, 1.2.3-beta.4 and v1.2.3-rc.1+build5. A string that is not
// one (dev, a branch name, a short version) reports false.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]uint64
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil || (len(p) > 1 && p[0] == '0') {
			return semver{}, false
		}
		n[i] = v
	}
	out := semver{major: n[0], minor: n[1], patch: n[2]}
	if hasPre {
		if pre == "" {
			return semver{}, false
		}
		out.pre = strings.Split(pre, ".")
		for _, id := range out.pre {
			if id == "" {
				return semver{}, false
			}
		}
	}
	return out, true
}

// prerelease says the version carries a -suffix.
func (v semver) prerelease() bool { return len(v.pre) > 0 }

// compare orders two versions: -1, 0 or 1. A pre-release sorts below its release, and
// pre-release identifiers compare as the specification says: numbers numerically and
// below words, a longer list above its prefix (beta.2 < beta.10 < rc.1).
func (v semver) compare(o semver) int {
	for _, d := range [][2]uint64{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case !v.prerelease() && !o.prerelease():
		return 0
	case !v.prerelease():
		return 1
	case !o.prerelease():
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(o.pre); i++ {
		if c := compareIdent(v.pre[i], o.pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.pre) < len(o.pre):
		return -1
	case len(v.pre) > len(o.pre):
		return 1
	}
	return 0
}

func compareIdent(a, b string) int {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	switch {
	case aerr == nil && berr == nil:
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
		return 0
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}
