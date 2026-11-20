package dispatch

import (
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

const replyCmd = "glab api projects/:id/merge_requests/12/discussions/abc123/notes -f body=Fixed"

func TestClaudeForgeRules(t *testing.T) {
	wt := "/w"
	for _, c := range []struct {
		name         string
		o            Opts
		lint, reply  bool
		denyReplyAPI bool
	}{
		{"push agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, true, true, true},
		{"push agent, no iid", Opts{Push: true, Worktree: wt}, true, false, false},
		{"no push, iid", Opts{MR: 12, Worktree: wt}, false, false, false},
		{"no push, no iid", Opts{}, false, false, false},
	} {
		args := adapters["claude"].Args(c.o)
		all := strings.Join(args, "\n")
		if got := strings.Contains(all, "Bash(glab ci lint *)"); got != c.lint {
			t.Errorf("%s: lint rule = %v", c.name, got)
		}
		reply := "Bash(glab api projects/:id/merge_requests/12/discussions/*/notes -f body=*)"
		if got := strings.Contains(all, reply); got != c.reply {
			t.Errorf("%s: reply rule = %v", c.name, got)
		}
		if got := strings.Contains(all, "Bash(glab api projects/:id/merge_requests/12/discussions/*--method*)"); got != c.denyReplyAPI {
			t.Errorf("%s: method deny = %v", c.name, got)
		}
		if got := strings.Contains(all, "Bash(glab ci lint* /*)"); got != c.lint {
			t.Errorf("%s: lint path deny = %v", c.name, got)
		}
		// The reply denies are scoped: no bare glab api deny that would cut off reads.
		if strings.Contains(all, "Bash(glab api*") {
			t.Errorf("%s: unscoped glab api deny: %s", c.name, all)
		}
		// Replying never grants resolving, approving or merging.
		if strings.Contains(all, "glab mr approve:*)") != c.denyReplyAPI {
			t.Errorf("%s: approve deny mismatch", c.name)
		}
		if strings.Contains(all, "Bash(glab mr merge") {
			t.Errorf("%s: merge granted without Merge", c.name)
		}
		// The allow rules sit before --disallowedTools, the denies after.
		if c.reply && strings.Index(all, reply) > strings.Index(all, "--disallowedTools") {
			t.Errorf("%s: reply rule is not an allow", c.name)
		}
	}
}

func TestOpenCodeForgeRules(t *testing.T) {
	wt := t.TempDir()
	for _, c := range []struct {
		name string
		o    Opts
		cmd  string
		want string
	}{
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint .gitlab-ci.yml", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd, "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/:id/merge_requests/13/discussions/abc/notes -f body=x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/:id/merge_requests/12/discussions/abc/notes -X DELETE", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " --method PUT", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " --hostname other.example", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " --input body.json", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/:id/merge_requests/12/discussions/abc/resolve -f resolved=true", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/:id/merge_requests/12/discussions/../../../x/notes -f body=a", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/:id/merge_requests/12/discussions/abc/notes -f body=$(id)", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " > /tmp/x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + "`id`", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " | sh", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + "; rm x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + " && ls", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + "\nrm x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, replyCmd + "\r\nrm x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint ../other.yml", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint ci/x.yml", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint " + wt + "/.gitlab-ci.yml", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint " + wt + "/../x.yml", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint " + wt + "/a.yml /etc/passwd", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint /etc/passwd", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint ~/.config/x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint --dry-run", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint a.yml --path=/etc/x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci lint $(id)", "deny"},
		// Reads that were allowed before stay allowed, query strings with & included.
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/pipelines?per_page=5&status=failed", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/pipelines/12/jobs?scope[]=failed&per_page=50 --paginate", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab ci trace 9", "allow"},
		// Writes stay refused.
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/pipelines?per_page=5&x=1 -X POST", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/pipelines -f ref=main", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/merge_requests/12/notes -f body=x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab mr approve 12", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab mr merge 12", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab mr note 12 -m hi", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12, Worktree: wt}, "glab api projects/7/pipelines/12/jobs", "allow"},
		{"agent, no iid", Opts{Push: true, Worktree: wt}, "glab ci lint", "allow"},
		{"agent, no iid", Opts{Push: true, Worktree: wt}, replyCmd, "deny"},
		{"no push, iid", Opts{MR: 12, Worktree: wt}, "glab ci lint", "deny"},
		{"no push, iid", Opts{MR: 12, Worktree: wt}, replyCmd, "deny"},
		{"no push, no iid", Opts{Worktree: wt}, "glab ci lint", "deny"},
		{"no push, no iid", Opts{Worktree: wt}, replyCmd, "deny"},
	} {
		rules := bashRules(t, openCodeConfig(c.o, config.OpenCode{}))
		if got := openCodeMatch(rules, c.cmd); got != c.want {
			t.Errorf("%s: %q: %q, want %q", c.name, c.cmd, got, c.want)
		}
	}
}
