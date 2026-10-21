package mergerule

import (
	"context"
	"path"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

// Input is what the caller knows about a lane's open merge request. Each field comes
// from proof (the store's gate record, the forge, git), never from an agent's word.
type Input struct {
	Repo, Lane string
	// Dir is a checkout of the repo (the lane's worktree is fine: only history is read),
	// Base the ref the lane merges into and Head the merge request's head commit.
	Dir, Base, Head string
	// MR is the merge request's number and URL, for the feed.
	MR    int
	MRURL string

	// ItemLinked is whether the lane is tied to a next-up item; Requirements are the
	// requirement ids that item cites.
	ItemLinked   bool
	Requirements []string
	// ApprovedChecks is "pass" or "fail", or "" when it cannot be told (the check files
	// are not recorded yet). CheckFiles are the approved check files, and
	// CheckFilesKnown says the list is real: with it false the rule cannot be met.
	ApprovedChecks  string
	CheckFiles      []string
	CheckFilesKnown bool
	// GateSHA is the commit of the lane's latest gate record and GatePassed whether it
	// passed; the gate counts only when GateSHA is Head.
	GateSHA    string
	GatePassed bool
	// Pipeline is the head pipeline's status as the forge words it ("success", "failed",
	// "running", ...), or "" for none.
	Pipeline string
	// AIReview is "ok", "blocking" or "unreviewed". Anything else is read as unreviewed.
	AIReview string
	// OpenThreads is the unresolved thread count, or -1 when unknown.
	OpenThreads int
}

// Facts builds the fact map crook.Evaluate reads for a merge. A fact that cannot be
// found is left out, so a rule that needs it is not met: unknown is never a pass.
func Facts(ctx context.Context, in Input) map[string]any {
	f := map[string]any{
		"item_linked": in.ItemLinked,
		"requirement": nonNil(in.Requirements),
		"gate":        gateState(in),
		"pipeline":    pipelineState(in.Pipeline),
		"ai_review":   reviewState(in.AIReview),
	}
	if in.ApprovedChecks == "pass" || in.ApprovedChecks == "fail" {
		f["approved_checks"] = in.ApprovedChecks
	}
	if in.OpenThreads >= 0 {
		f["open_threads"] = in.OpenThreads
	}
	if in.Dir == "" || in.Base == "" || in.Head == "" {
		return f
	}
	f["up_to_date"] = git.Ok(ctx, in.Dir, "merge-base", "--is-ancestor", in.Base, in.Head)
	stats, err := git.DiffStat(ctx, in.Dir, in.Base+"..."+in.Head)
	if err != nil {
		return f
	}
	var paths []string
	lines, testsEdited, checksEdited := 0, false, false
	checks := map[string]bool{}
	for _, c := range in.CheckFiles {
		checks[path.Clean(c)] = true
	}
	for _, s := range stats {
		paths = append(paths, s.Path)
		if s.OldPath != "" {
			paths = append(paths, s.OldPath)
		}
		lines += s.Added + s.Removed
		// A test counts as edited when it existed before: changed, deleted or renamed.
		// A test the lane adds is new work, not a weakened measure.
		if before := s.OldPath; s.Status != "A" {
			if before == "" {
				before = s.Path
			}
			if isTestFile(before) {
				testsEdited = true
			}
			if checks[path.Clean(before)] {
				checksEdited = true
			}
		}
		if checks[path.Clean(s.Path)] {
			checksEdited = true
		}
	}
	f["paths"] = nonNil(paths)
	f["changed_lines"] = lines
	f["tests_edited"] = testsEdited
	if in.CheckFilesKnown {
		f["check_files_edited"] = checksEdited
	}
	return f
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func gateState(in Input) string {
	if in.GateSHA != "" && in.GateSHA == in.Head {
		if in.GatePassed {
			return "green"
		}
		return "red"
	}
	return "none"
}

func pipelineState(status string) string {
	switch status {
	case "success":
		return "green"
	case "failed", "canceled":
		return "red"
	case "":
		return "none"
	default:
		return "running"
	}
}

func reviewState(s string) string {
	switch s {
	case "ok", "blocking":
		return s
	}
	return "unreviewed"
}

// isTestFile reports whether p is a test file: a Go test, a web test or spec, or
// anything under a testdata folder.
func isTestFile(p string) bool {
	p = path.Clean(p)
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.Contains(base, ".test.") || strings.Contains(base, ".spec."):
		return true
	}
	return strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/")
}
