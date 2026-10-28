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

// Rule is a protected glob and the reason it is protected.
type Rule struct{ Glob, Why string }

// Refusal is a requested file left out of an auto-widen, with the glob that held it and
// why. The desk uses it to ask the person for an explicit lane_widen.
type Refusal struct{ File, Glob, Why string }

// Denylist is the built-in set of globs no auto-widen may reach, with the reason for
// each. It is deliberately conservative: a miss costs the person one explicit widen, a
// wrong yes can put a lane's hands on CI, credentials or the checks its own work must pass.
var Denylist = []Rule{
	// CI definitions.
	{".gitlab-ci.yml", "CI configuration"}, {"**/.gitlab-ci.yml", "CI configuration"},
	{".gitlab/**", "CI configuration"}, {".github/**", "CI configuration"},
	{".circleci/**", "CI configuration"}, {"ci/**", "CI configuration"},
	// Git internals and hooks.
	{".git", "git internals"}, {".git/**", "git internals"}, {"**/.git/**", "git internals"},
	{".gitmodules", "git internals"}, {".gitattributes", "git internals"},
	{".githooks/**", "git hooks"}, {".husky/**", "git hooks"}, {".pre-commit-config.yaml", "git hooks"},
	// Secrets and credentials.
	{"**/secrets/**", "secrets-like name"}, {"**/secret", "secrets-like name"},
	{"**/secret.*", "secrets-like name"}, {"**/secrets.*", "secrets-like name"},
	{"**/*[\\-_.]secret[\\-_.]*", "secrets-like name"}, {"**/*[\\-_.]secrets[\\-_.]*", "secrets-like name"},
	{"**/*[\\-_.]secret", "secrets-like name"}, {"**/*[\\-_.]secrets", "secrets-like name"},
	{"**/*credential*", "credentials-like name"}, {"**/.env", "environment file"}, {"**/.env.*", "environment file"},
	{"**/*.pem", "key file"}, {"**/*.key", "key file"}, {"**/id_rsa*", "key file"},
	{"**/id_ed25519*", "key file"}, {"**/*.p12", "key file"}, {"**/*.pfx", "key file"},
	{"**/.npmrc", "package registry credentials"}, {"**/.netrc", "network credentials"},
	{"**/kubeconfig", "cluster credentials"}, {"**/kubeconfig.*", "cluster credentials"},
	// Deploys and schema changes.
	{"deploy/**", "deploy files"}, {"**/deploy/**", "deploy files"},
	{"**/migrations/**", "schema migrations"}, {"**/migrate/**", "schema migrations"}, {"**/*.sql", "schema migrations"},
	{"**/Dockerfile", "deploy files"}, {"**/docker-compose*.yml", "deploy files"},
	{"**/CODEOWNERS", "review ownership"}, {"docs/WORK.md", "the work tracker"},
	// Dependency manifests and lockfiles: they change what the build runs and fetches,
	// and are reviewed as such, so a lane touching one is a decision, not a side effect.
	{"**/go.mod", "dependency manifest"}, {"**/go.sum", "lockfile"}, {"**/package.json", "dependency manifest"},
	{"**/package-lock.json", "lockfile"}, {"**/npm-shrinkwrap.json", "lockfile"},
	{"**/yarn.lock", "lockfile"}, {"**/pnpm-lock.yaml", "lockfile"}, {"**/bun.lock", "lockfile"}, {"**/bun.lockb", "lockfile"},
	{"**/Cargo.toml", "dependency manifest"}, {"**/Cargo.lock", "lockfile"},
	{"**/Gemfile", "dependency manifest"}, {"**/Gemfile.lock", "lockfile"},
	{"**/composer.json", "dependency manifest"}, {"**/composer.lock", "lockfile"},
	{"**/poetry.lock", "lockfile"}, {"**/Pipfile.lock", "lockfile"},
	{"**/pyproject.toml", "dependency manifest"}, {"**/requirements*.txt", "dependency manifest"},
	// What defines the checks a lane's own work must pass: build and gate scripts and
	// lint and test configuration.
	{"**/Makefile", "build and gate script"}, {"**/GNUmakefile", "build and gate script"}, {"**/*.mk", "build and gate script"},
	{"bin/**", "build and gate script"}, {"scripts/**", "build and gate script"},
	{"**/.golangci*", "lint or test configuration"}, {"**/.eslintrc*", "lint or test configuration"},
	{"**/eslint.config.*", "lint or test configuration"}, {"**/vitest.config.*", "lint or test configuration"},
	{"**/jest.config.*", "lint or test configuration"}, {"**/tsconfig*.json", "lint or test configuration"},
}

// DefaultProtected are the globs of Denylist.
var DefaultProtected = func() []string {
	out := make([]string, len(Denylist))
	for i, r := range Denylist {
		out[i] = r.Glob
	}
	return out
}()

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
	// Refused are requested files left out because they are protected. Present on an OK
	// verdict (the rest was widened) and on a decline (nothing was left to widen).
	Refused []Refusal
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
	rules := append([]Rule{}, Denylist...)
	for _, g := range in.Protected {
		rules = append(rules, Rule{g, "protected by the repo's widen_protected"})
	}
	var refused []Refusal
	var allowed []string
	for _, f := range need {
		if r := protectedBy(rules, f); r != nil {
			refused = append(refused, Refusal{f, r.Glob, r.Why})
			continue
		}
		allowed = append(allowed, f)
	}
	if len(allowed) == 0 {
		v := no("%s; they need an explicit widen", describe(refused))
		v.Refused = refused
		return v
	}
	need = allowed
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
			v := no("%s overlaps another open lane's scope", both[0])
			v.Refused = refused
			return v
		}
	}
	return Verdict{OK: true, Files: need, Refused: refused, Checks: []string{
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

// Describe says which files were refused, by which glob and why, for the desk to relay.
func (v Verdict) Describe() string { return describe(v.Refused) }

func describe(rs []Refusal) string {
	var parts []string
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%s is protected (%s: %s)", r.File, r.Glob, r.Why))
	}
	return strings.Join(parts, "; ")
}

func protectedBy(rules []Rule, file string) *Rule {
	lower := strings.ToLower(file)
	for i, r := range rules {
		if scope.Match(r.Glob, file) || scope.Match(strings.ToLower(r.Glob), lower) {
			return &rules[i]
		}
	}
	return nil
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
