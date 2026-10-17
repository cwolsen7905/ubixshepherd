// Package autowiden decides, by rule and nothing else, whether a lane's scope may be
// widened without asking the person. Decide is a pure function: it reads no repo, store
// or config, so the same inputs always give the same verdict.
package autowiden

import (
	"fmt"
	"path"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/scope"
)

// Defaults for a repo that opts in and says nothing more.
const (
	DefaultMaxFiles = 3
	DefaultCap      = 3
)

// DefaultProtected are the globs no auto-widen may reach: CI definitions, migrations,
// dependency manifests and lockfiles, deploy and secret files, the work tracker.
var DefaultProtected = []string{
	".gitlab-ci.yml", "**/.gitlab-ci.yml", "ci/**", ".gitlab/**", ".github/**", ".circleci/**",
	"**/migrations/**", "**/migrate/**", "**/*.sql",
	"**/go.mod", "**/go.sum", "**/package.json", "**/package-lock.json", "**/npm-shrinkwrap.json",
	"**/yarn.lock", "**/pnpm-lock.yaml", "**/bun.lock", "**/bun.lockb",
	"**/Cargo.toml", "**/Cargo.lock", "**/Gemfile", "**/Gemfile.lock", "**/composer.json", "**/composer.lock",
	"**/poetry.lock", "**/Pipfile.lock", "**/pyproject.toml", "**/requirements*.txt",
	"deploy/**", "**/deploy/**", "**/secrets/**", "**/secret*", "**/.env", "**/.env.*",
	"**/*.pem", "**/*.key", "**/id_rsa*", "**/Dockerfile", "**/docker-compose*.yml",
	"**/CODEOWNERS", "docs/WORK.md",
}

// Input is everything the rule looks at.
type Input struct {
	// Files are the exact repo-relative files the request names.
	Files []string
	// Scope is the lane's scope; Changed the files the lane has changed (repo-relative),
	// which with the literal scope entries anchor "near".
	Scope, Changed []string
	// Others are the scopes of the other open lanes in the repo; RepoFiles the repo's
	// tracked files, which the overlap check uses to judge globs.
	Others    [][]string
	RepoFiles []string
	// Used is the auto-widens this lane has had already.
	Used int
	// Settings, as the repo's profile gives them.
	Mode      string // "auto" opts in; anything else does not
	MaxFiles  int    // 0 is DefaultMaxFiles
	Cap       int    // 0 is DefaultCap; negative allows none
	Protected []string
}

// Verdict is the rule's answer. When OK, Checks names what passed and Files is the
// normalised list to add. When not, Reason says which condition failed.
type Verdict struct {
	OK     bool
	Reason string
	Files  []string
	Checks []string
}

func no(format string, a ...any) Verdict { return Verdict{Reason: fmt.Sprintf(format, a...)} }

// Decide applies the rule.
func Decide(in Input) Verdict {
	if in.Mode != "auto" {
		return no("the repo does not auto-widen (widen: %s)", orDefault(in.Mode, "human"))
	}
	if len(in.Files) == 0 {
		return no("the request names no files")
	}
	max := in.MaxFiles
	if max == 0 {
		max = DefaultMaxFiles
	}
	cp := in.Cap
	if cp == 0 {
		cp = DefaultCap
	}
	if in.Used >= cp {
		return no("the lane has used its %d auto-widens", cp)
	}
	files, err := exact(in.Files)
	if err != nil {
		return no("%v", err)
	}
	// Files already in scope need nothing; only the rest is widened for.
	var need []string
	for _, f := range files {
		if !scope.Any(in.Scope, f) {
			need = append(need, f)
		}
	}
	if len(need) == 0 {
		return no("the files are already in scope")
	}
	if len(need) > max {
		return no("%d files is more than the %d an auto-widen may add", len(need), max)
	}
	protected := append(append([]string{}, DefaultProtected...), in.Protected...)
	for _, f := range need {
		if g := protectedBy(protected, f); g != "" {
			return no("%s is protected (%s)", f, g)
		}
	}
	anchors := anchorsOf(in.Scope, in.Changed)
	var why []string
	for _, f := range need {
		w := near(f, anchors)
		if w == "" {
			return no("%s is not near the lane's work: not beside a file it changed, and not a test of one", f)
		}
		why = append(why, w)
	}
	for _, o := range in.Others {
		if both := scope.Overlap(need, o, in.RepoFiles); len(both) > 0 {
			return no("%s overlaps another open lane's scope", both[0])
		}
	}
	return Verdict{OK: true, Files: need, Checks: []string{
		"repo opts in",
		"exact files",
		fmt.Sprintf("%d of at most %d files", len(need), max),
		"not protected",
		"near the lane's work (" + strings.Join(uniq(why), "; ") + ")",
		"free of other open lanes",
		fmt.Sprintf("auto-widen %d of %d", in.Used+1, cp),
	}}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// exact cleans the request's files and refuses anything that is not one file of the repo.
func exact(files []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, f := range files {
		f = strings.TrimSpace(f)
		c := path.Clean(f)
		switch {
		case f == "":
			return nil, fmt.Errorf("the request names an empty path")
		case strings.ContainsAny(f, `*?[]{}\`):
			return nil, fmt.Errorf("%q is a pattern, not an exact file", f)
		case strings.HasSuffix(f, "/") || c == ".":
			return nil, fmt.Errorf("%q is a directory, not an exact file", f)
		case path.IsAbs(c) || strings.Contains(c, ":") || c == ".." || strings.HasPrefix(c, "../"):
			return nil, fmt.Errorf("%q is not a path inside the repo", f)
		case c != f && "./"+c != f:
			return nil, fmt.Errorf("%q is not a clean repo-relative path", f)
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}

func protectedBy(globs []string, file string) string {
	lower := strings.ToLower(file)
	for _, g := range globs {
		if scope.Match(g, file) || scope.Match(strings.ToLower(g), lower) {
			return g
		}
	}
	return ""
}

// anchorsOf are the files the lane's work is at: those it changed that are in scope,
// and scope entries that name a single file.
func anchorsOf(sc, changed []string) []string {
	var out []string
	for _, c := range changed {
		if scope.Any(sc, c) {
			out = append(out, path.Clean(c))
		}
	}
	for _, g := range sc {
		if !strings.ContainsAny(g, `*?[\`) && path.Ext(g) != "" {
			out = append(out, path.Clean(g))
		}
	}
	return out
}

// near says how f is close to an anchor, or "" if it is not.
func near(f string, anchors []string) string {
	for _, a := range anchors {
		if isTestOf(f, a) {
			return f + " tests " + a
		}
	}
	for _, a := range anchors {
		if path.Dir(f) == path.Dir(a) {
			return f + " is beside " + a
		}
	}
	return ""
}

// isTestOf reports whether test is, by name, a test of src: the same directory or its
// __tests__ directory, and a test-file name built on src's.
func isTestOf(test, src string) bool {
	td, sd := path.Dir(test), path.Dir(src)
	if td != sd && td != path.Join(sd, "__tests__") {
		return false
	}
	tb, sb := path.Base(test), path.Base(src)
	ext := path.Ext(sb)
	stem := strings.TrimSuffix(sb, ext)
	if stem == "" || tb == sb {
		return false
	}
	for _, n := range []string{
		stem + "_test" + ext, stem + ".test" + ext, stem + ".spec" + ext,
		"test_" + stem + ext, stem + "_spec" + ext, stem + "Test" + ext, stem + "Tests" + ext,
	} {
		if tb == n {
			return true
		}
	}
	return false
}

func uniq(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
