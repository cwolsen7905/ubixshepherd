package dispatch

import (
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

const replyCmd = "glab api projects/:id/merge_requests/12/discussions/abc123/notes -f body=Fixed"

func TestClaudeForgeRules(t *testing.T) {
	for _, c := range []struct {
		name         string
		o            Opts
		lint, reply  bool
		denyReplyAPI bool
	}{
		{"push agent, iid", Opts{Push: true, MR: 12}, true, true, true},
		{"push agent, no iid", Opts{Push: true}, true, false, false},
		{"no push, iid", Opts{MR: 12}, false, false, false},
		{"no push, no iid", Opts{}, false, false, false},
	} {
		args := adapters["claude"].Args(c.o)
		all := strings.Join(args, "\n")
		if got := strings.Contains(all, "Bash(glab ci lint:*)"); got != c.lint {
			t.Errorf("%s: lint rule = %v", c.name, got)
		}
		reply := "Bash(glab api projects/:id/merge_requests/12/discussions/*/notes -f body=*)"
		if got := strings.Contains(all, reply); got != c.reply {
			t.Errorf("%s: reply rule = %v", c.name, got)
		}
		if got := strings.Contains(all, "Bash(glab api*--method*)"); got != c.denyReplyAPI {
			t.Errorf("%s: method deny = %v", c.name, got)
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
	for _, c := range []struct {
		name string
		o    Opts
		cmd  string
		want string
	}{
		{"agent, iid", Opts{Push: true, MR: 12}, "glab ci lint", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab ci lint .gitlab-ci.yml", "allow"},
		{"agent, iid", Opts{Push: true, MR: 12}, replyCmd, "allow"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab api projects/:id/merge_requests/13/discussions/abc/notes -f body=x", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab api projects/:id/merge_requests/12/discussions/abc/notes -X DELETE", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, replyCmd + " --method PUT", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, replyCmd + " --hostname other.example", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, replyCmd + " --input body.json", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab api projects/:id/merge_requests/12/discussions/abc/resolve -f resolved=true", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab api projects/:id/merge_requests/12/discussions/../../../x/notes -f body=a", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab mr approve 12", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab mr merge 12", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab mr note 12 -m hi", "deny"},
		{"agent, iid", Opts{Push: true, MR: 12}, "glab api projects/7/pipelines/12/jobs", "allow"},
		{"agent, no iid", Opts{Push: true}, "glab ci lint", "allow"},
		{"agent, no iid", Opts{Push: true}, replyCmd, "deny"},
		{"no push, iid", Opts{MR: 12}, "glab ci lint", "deny"},
		{"no push, iid", Opts{MR: 12}, replyCmd, "deny"},
		{"no push, no iid", Opts{}, "glab ci lint", "deny"},
		{"no push, no iid", Opts{}, replyCmd, "deny"},
	} {
		rules := bashRules(t, openCodeConfig(c.o, config.OpenCode{}))
		if got := openCodeMatch(rules, c.cmd); got != c.want {
			t.Errorf("%s: %q: %q, want %q", c.name, c.cmd, got, c.want)
		}
	}
}
