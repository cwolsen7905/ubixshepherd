package dispatch

import "fmt"

// Forge permissions for a lane's agent where autonomy.push is agent: it may lint the CI
// configuration and reply on the discussion threads of its own lane's merge request, and
// nothing more. It may not resolve a thread, approve or merge (merge stays Opts.Merge).
//
// A reply is `glab api projects/:id/merge_requests/<iid>/discussions/<discussion>/notes -f body=<text>`:
// glab sends a POST when a field is given, so no method flag is needed or allowed.

// glabLint is the CI lint command.
const glabLint = "glab ci lint"

// glabReplyPath is the head of a reply command up to the discussion id; the
// project is the repo's own, through glab's :id placeholder.
func glabReplyPath(iid int) string {
	return fmt.Sprintf("projects/:id/merge_requests/%d/discussions/", iid)
}

const glabReplyTail = "/notes -f body="

// claudeForgeAllow is the Claude Code allow rules for the forge, empty unless the agent
// pushes. The reply rule needs the lane's merge request iid.
func claudeForgeAllow(o Opts) []string {
	if !o.Push {
		return nil
	}
	a := []string{"Bash(" + glabLint + ":*)"}
	if o.MR > 0 {
		a = append(a, "Bash(glab api "+glabReplyPath(o.MR)+"*"+glabReplyTail+"*)")
	}
	return a
}

// claudeForgeDeny is what keeps a reply a reply: with a reply rule granted, any glab api
// call naming another method, a body file, another host, header or a path that could walk
// out of the merge request is refused. Deny wins over allow in Claude Code.
func claudeForgeDeny(o Opts) []string {
	if !o.Push || o.MR <= 0 {
		return nil
	}
	var d []string
	for _, p := range glabApiFlagDenies {
		d = append(d, "Bash(glab api*"+p+"*)")
	}
	d = append(d, "Bash(glab mr approve:*)", "Bash(glab mr note:*)", "Bash(glab api*/resolve*)")
	return d
}

// glabApiFlagDenies are fragments that turn a note POST into something else.
var glabApiFlagDenies = []string{"-X", "--method", "--input", "-F", "--field", "--raw-field",
	"-h", "--hostname", "-H", "--header", "..", "%"}

// openCodeForgeAllow is the OpenCode bash rules to append after every deny already in the
// list (last match wins), then followed by openCodeForgeReplyDeny.
func openCodeForgeAllow(o Opts) []kv {
	if !o.Push {
		return nil
	}
	r := []kv{{glabLint + "*", "allow"}}
	if o.MR > 0 {
		r = append(r, kv{"glab api " + glabReplyPath(o.MR) + "*" + glabReplyTail + "*", "allow"})
		for _, p := range glabApiFlagDenies {
			r = append(r, kv{"glab api*" + p + "*", "deny"})
		}
		r = append(r, kv{"glab api*/resolve*", "deny"})
	}
	return r
}
