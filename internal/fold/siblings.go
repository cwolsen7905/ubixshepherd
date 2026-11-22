package fold

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Sibling auto-widen. A lane's scope names the files it works on; three kinds of file
// travel with that work, and a lane may change them without widening its scope, as long
// as no other open lane holds them:
//
//   - the test file of a Go source file in scope: foo_test.go beside foo.go;
//   - the changelog at the repo root, CHANGELOG.md;
//   - the docs pages the lane was opened with (OpenRequest.Docs).
//
// Nothing is written to the lane's scope. The rule is applied each time a path is judged
// (the pre-push check, the diff), so a sibling another lane takes later is refused from
// then on, and one it releases is allowed again. A path another open lane's scope covers
// is held by that lane and stays outside this one, with the holder named. Approved check
// files are judged before any of this, as for every lane.

// changelogFile is the sibling every lane may touch.
const changelogFile = "CHANGELOG.md"

// docsKey is the repo config key holding the docs pages a lane was opened with, one value
// per page, kept beside adoptedKey and cleared when the lane closes.
func docsKey(branch string) string { return "branch." + branch + ".shepherddocs" }

// docsOf reads the docs pages recorded for a branch.
func docsOf(ctx context.Context, repo, branch string) []string {
	out, err := git.Run(ctx, repo, "config", "--get-all", docsKey(branch))
	if err != nil {
		return nil
	}
	var pages []string
	for _, p := range strings.Split(out, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			pages = append(pages, p)
		}
	}
	return pages
}

// saveDocs records the docs pages for a lane's branch, replacing any left by an earlier
// lane on the same branch name.
func saveDocs(ctx context.Context, repo, branch string, pages []string) error {
	clearDocs(ctx, repo, branch)
	for _, p := range pages {
		if _, err := git.Run(ctx, repo, "config", "--add", docsKey(branch), p); err != nil {
			clearDocs(ctx, repo, branch)
			return err
		}
	}
	return nil
}

func clearDocs(ctx context.Context, repo, branch string) {
	git.Run(ctx, repo, "config", "--unset-all", docsKey(branch))
}

// checkDocs normalises the docs pages a lane is opened with: exact repo-relative file
// paths, no globs, none that leave the repo.
func checkDocs(pages []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range pages {
		clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"))
		switch {
		case strings.TrimSpace(p) == "" || clean == ".":
			return nil, refuse("empty docs page")
		case strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../"):
			return nil, refuse("docs page %q: name a file inside the repo, relative to it", p)
		case strings.ContainsAny(clean, "*?[\\"):
			return nil, refuse("docs page %q: name the page, not a glob; a wider set goes in the scope", p)
		}
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out, nil
}

// Reach answers, for one lane, whether a path is inside its scope or one of its siblings.
// It is read once and then asked about each path, so a diff of many files lists the
// repo's other lanes once.
type Reach struct {
	lane   store.Lane
	others []store.Lane // the repo's other lanes that are not closed
	docs   map[string]bool
}

// Reach reads what the lane's scope reaches, with its siblings.
func (f *Fold) Reach(ctx context.Context, lane store.Lane) (*Reach, error) {
	lanes, err := f.Store.Lanes(ctx, lane.RepoID)
	if err != nil {
		return nil, err
	}
	r := &Reach{lane: lane, docs: map[string]bool{}}
	for _, o := range lanes {
		if o.ID != lane.ID && o.State != store.LaneClosed {
			r.others = append(r.others, o)
		}
	}
	if repo, err := f.Store.Repo(ctx, lane.RepoID); err == nil {
		for _, p := range docsOf(ctx, repo.Path, lane.Branch) {
			r.docs[p] = true
		}
	}
	return r, nil
}

// readReach is Reach for a read-only view: when the other lanes cannot be listed it
// judges by the lane's own scope alone, as the diff did before siblings, instead of failing.
func (f *Fold) readReach(ctx context.Context, lane store.Lane) *Reach {
	if r, err := f.Reach(ctx, lane); err == nil {
		return r
	}
	return &Reach{lane: lane, docs: map[string]bool{}}
}

// Status is where a path stands for the lane.
type Status struct {
	// In: the lane may change the path, by its scope or as a sibling.
	In bool
	// Sibling: In by the sibling rule, not by the scope. Of names the file it travels
	// with ("" for the changelog and a docs page).
	Sibling bool
	Of      string
	// HeldBy: a sibling the lane would have, but another open lane's scope covers it.
	HeldBy string
}

// Judge says whether file (repo-relative, "/"-separated) is inside the lane's reach.
func (r *Reach) Judge(file string) Status {
	file = path.Clean(file)
	if scope.Any(r.lane.Scope, file) {
		return Status{In: true}
	}
	var of string
	switch {
	case file == changelogFile:
	case r.docs[file]:
	case strings.HasSuffix(file, "_test.go"):
		src := strings.TrimSuffix(file, "_test.go") + ".go"
		if !scope.Any(r.lane.Scope, src) {
			return Status{}
		}
		of = src
	default:
		return Status{}
	}
	for _, o := range r.others {
		if scope.Any(o.Scope, file) {
			return Status{HeldBy: o.Name}
		}
	}
	return Status{In: true, Sibling: true, Of: of}
}

// In reports whether the lane may change file.
func (r *Reach) In(file string) bool { return r.Judge(file).In }

// outsideMessage words the files a push changes outside the lane's reach. A file held by
// another lane says so, naming it.
func outsideMessage(lane store.Lane, r *Reach, outside []string) string {
	lines := make([]string, 0, len(outside))
	for _, file := range outside {
		if h := r.Judge(file).HeldBy; h != "" {
			file = fmt.Sprintf("%s (held by lane %s)", file, h)
		}
		lines = append(lines, file)
	}
	return fmt.Sprintf("changes outside lane %s's scope (%s):\n    %s",
		lane.Name, strings.Join(lane.Scope, ", "), strings.Join(firstLinesN(lines, 20), "\n    "))
}
