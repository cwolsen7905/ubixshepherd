package dispatch

import "fmt"

// Forge permissions for a lane's agent where autonomy.push is agent: it may lint the CI
// configuration and reply on the discussion threads of its own lane's merge request, and
// nothing more. It may not resolve a thread, approve or merge (merge stays Opts.Merge).
//
// A reply is `glab api projects/:id/merge_requests/<iid>/discussions/<discussion>/notes -f body=<text>`:
// glab sends a POST when a field is given, so no method flag is needed or allowed.
// The denies that keep a reply a reply are scoped to the reply form, so the read-only
// glab api calls a run already had (query strings with & included) are untouched.
//
// `glab ci lint` sends a file to the forge, so it takes no flags and a path only inside
// the worktree: none, a relative one, or (OpenCode) an absolute one under the worktree.

// glabLint is the CI lint command.
const glabLint = "glab ci lint"

// glabReplyPath is the head of a reply command up to the discussion id; the
// project is the repo's own, through glab's :id placeholder.
func glabReplyPath(iid int) string {
	return fmt.Sprintf("projects/:id/merge_requests/%d/discussions/", iid)
}

const glabReplyTail = "/notes -f body="

// glabShellDenies are fragments no forge command of the run may carry: shell
// substitution, redirects, separators and line breaks.
var glabShellDenies = []string{"$", "`", ">", "<", "|", ";", "&", "\n", "\r", "\\"}

// glabReplyDenies are fragments that turn a note POST into something else, or smuggle a
// command in its body.
var glabReplyDenies = append([]string{"-X", "--method", "--input", "-F", "--field", "--raw-field",
	"-h", "--hostname", "-H", "--header", "..", "%", "/resolve"}, glabShellDenies...)

// glabLintDenies are what a lint path or argument may not hold: a flag, a home or parent
// reference, an absolute path (second positional included), or shell syntax.
var glabLintDenies = append([]string{" -", "..", "~", " /", "%"}, glabShellDenies...)

// claudeForgeAllow is the Claude Code allow rules for the forge, empty unless the agent
// pushes. The reply rule needs the lane's merge request iid.
func claudeForgeAllow(o Opts) []string {
	if !o.Push {
		return nil
	}
	a := []string{"Bash(" + glabLint + " *)"}
	if o.MR > 0 {
		a = append(a, "Bash(glab api "+glabReplyPath(o.MR)+"*"+glabReplyTail+"*)")
	}
	return a
}

// claudeForgeDeny: in Claude Code a deny wins over an allow, so each deny is scoped to the
// command it guards.
func claudeForgeDeny(o Opts) []string {
	if !o.Push {
		return nil
	}
	var d []string
	for _, p := range glabLintDenies {
		d = append(d, "Bash("+glabLint+"*"+p+"*)")
	}
	if o.MR > 0 {
		for _, p := range glabReplyDenies {
			d = append(d, "Bash(glab api "+glabReplyPath(o.MR)+"*"+p+"*)")
		}
		d = append(d, "Bash(glab mr approve:*)", "Bash(glab mr note:*)")
	}
	return d
}

// openCodeForgeAllow is the OpenCode bash rules to append after every deny already in the
// list (last match wins). worktrees are the forms of the worktree path.
func openCodeForgeAllow(o Opts, worktrees []string) []kv {
	if !o.Push {
		return nil
	}
	lintDenies := func(abs bool) (r []kv) {
		for _, p := range glabLintDenies {
			if p == " /" && !abs {
				continue
			}
			r = append(r, kv{glabLint + "*" + p + "*", "deny"})
		}
		return r
	}
	r := []kv{{glabLint + " *", "allow"}}
	r = append(r, lintDenies(true)...)
	// An absolute path is allowed only under the worktree, then the denies run again so
	// it cannot carry a second path, a flag or a dot segment.
	for _, w := range worktrees {
		r = append(r, kv{glabLint + " " + w + "/*", "allow"})
	}
	if len(worktrees) > 0 {
		r = append(r, lintDenies(false)...)
		r = append(r, kv{glabLint + "*/* /*", "deny"})
	}
	if o.MR > 0 {
		r = append(r, kv{"glab api " + glabReplyPath(o.MR) + "*" + glabReplyTail + "*", "allow"})
		for _, p := range glabReplyDenies {
			r = append(r, kv{"glab api " + glabReplyPath(o.MR) + "*" + p + "*", "deny"})
		}
	}
	return r
}
