package dispatch

import (
	"github.com/ubixsys/ubixshepherd/internal/config"
	"regexp"
	"strings"
	"testing"
	"time"
)

const day = 24 * time.Hour

func TestAgeText(t *testing.T) {
	for age, want := range map[time.Duration]string{
		0: "today", 23 * time.Hour: "today", 24 * time.Hour: "1 day ago", 41*day + time.Hour: "41 days ago",
	} {
		if got := AgeText(age); got != want {
			t.Errorf("AgeText(%v) = %q, want %q", age, got, want)
		}
	}
}

func TestStandingLayersWorkspaceProjectRepo(t *testing.T) {
	p := &ProjectContext{Name: "shop", Brief: "Ship checkout.", Age: 41 * day, MaxAge: 30 * day,
		Caches: []Cache{{Path: "/build/llvm", Repo: "core", Recipe: "tools/build.sh", Note: "Rebuild it."}}}
	got := Standing("adapter note", "WS rules", p, "REPO rules")
	order := []string{"adapter note", "The workspace's rules: WS rules", "Project shop's brief (approved 41 days ago",
		"past the project's 30-day limit", "Ship checkout.", "build caches", "/build/llvm: made by tools/build.sh in repo core. Rebuild it.",
		"This repo's rules", "REPO rules"}
	at := -1
	for _, want := range order {
		i := strings.Index(got, want)
		if i < 0 || i < at {
			t.Fatalf("%q missing or out of order (after offset %d) in:\n%s", want, at, got)
		}
		at = i
	}
	if strings.Contains(got, "do not hand-patch") == false {
		t.Errorf("caches do not say to rebuild:\n%s", got)
	}
}

func TestStandingStaleBoundary(t *testing.T) {
	at := func(age time.Duration) string {
		return Standing("", "", &ProjectContext{Name: "p", Brief: "b", Age: age, MaxAge: 30 * day}, "")
	}
	if got := at(30 * day); strings.Contains(got, "limit") {
		t.Errorf("a brief exactly at its limit is stale:\n%s", got)
	}
	if got := at(30*day + time.Second); !strings.Contains(got, "limit") {
		t.Errorf("a brief past its limit is not marked:\n%s", got)
	}
}

func TestStandingWithoutProjectIsTheRepoRules(t *testing.T) {
	for _, p := range []*ProjectContext{nil, {Name: "p"}} {
		if got := Standing("note", "ignored workspace", p, "be kind"); got != "note\nThis repo's rules: be kind" {
			t.Errorf("standing = %q", got)
		}
	}
	if got := Standing("", "", nil, ""); got != "" {
		t.Errorf("standing = %q", got)
	}
}

func TestStandingRedactsTheBrief(t *testing.T) {
	got := Standing("", "", &ProjectContext{Name: "p", Brief: "use glpat-AbCdEfGhIjKlMnOpQrStUv here"}, "")
	if strings.Contains(got, "glpat-") {
		t.Errorf("brief not redacted:\n%s", got)
	}
}

// Every brief tells the agent not to skip hooks, whoever pushes; Claude denies the bypass
// even when the repo lets the agent push.
func TestBriefAndClaudeForbidHookBypass(t *testing.T) {
	for _, push := range []string{config.Agent, config.Shepherd, ""} {
		b := Brief("t", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{Push: push}, true, "", "")
		if !strings.Contains(b, "Never bypass git hooks") || !strings.Contains(b, "--no-verify") {
			t.Errorf("push %q: brief does not forbid skipping hooks:\n%s", push, b)
		}
	}
	denied := func(args []string, flag string) []string {
		for i, a := range args {
			if a == flag {
				return args[i+1:]
			}
		}
		return nil
	}
	has := func(list []string, want string) bool {
		for _, a := range list {
			if a == want {
				return true
			}
		}
		return false
	}
	for _, push := range []bool{true, false} {
		list := denied(adapters["claude"].Args(Opts{Push: push}), "--disallowedTools")
		for _, rule := range BypassDenied {
			if !has(list, rule) {
				t.Errorf("push %v: claude does not deny %q: %q", push, rule, list)
			}
		}
		if has(list, "Bash(git push:*)") == push {
			t.Errorf("push %v: claude push deny = %v", push, !push)
		}
		var cop []string
		args := adapters["copilot"].Args(Opts{Push: push})
		for i, a := range args {
			if a == "--deny-tool" && i+1 < len(args) {
				cop = append(cop, args[i+1])
			}
		}
		for _, rule := range CopilotBypassDenied {
			if !has(cop, rule) {
				t.Errorf("push %v: copilot does not deny %q: %q", push, rule, cop)
			}
		}
		if has(cop, "shell(git push)") == push {
			t.Errorf("push %v: copilot push deny = %v", push, !push)
		}
	}
}

// Agents cannot signal processes by name, so one lane cannot kill another's runs; a
// plain kill of a PID is not denied.
func TestKillByNameDenied(t *testing.T) {
	for _, rule := range []string{"Bash(*pkill*)", "Bash(*killall*)", "Bash(kill*$(*)", "Bash(kill 0*)", "Bash(kill* -1)", "Bash(kill* -- *)", "Bash(kill -* -*)", "Bash(*pgrep*kill*)", "Bash(*xargs*kill*)"} {
		found := false
		for _, r := range KillDenied {
			found = found || r == rule
		}
		if !found {
			t.Errorf("KillDenied lacks %q", rule)
		}
	}
	for _, r := range KillDenied {
		if r == "Bash(kill*)" || r == "Bash(kill:*)" {
			t.Errorf("KillDenied %q would deny a plain kill of the agent's own PID", r)
		}
	}
	for _, push := range []bool{true, false} {
		j := strings.Join(adapters["claude"].Args(Opts{Push: push}), " ")
		_, list, _ := strings.Cut(j, "--disallowedTools")
		for _, rule := range KillDenied {
			if !strings.Contains(list, rule) {
				t.Errorf("push %v: claude does not deny %q", push, rule)
			}
		}
		cj := strings.Join(adapters["copilot"].Args(Opts{Push: push}), " ")
		for _, rule := range CopilotKillDenied {
			if !strings.Contains(cj, "--deny-tool "+rule) {
				t.Errorf("push %v: copilot does not deny %q", push, rule)
			}
		}
	}
	oc := openCodeConfig(Opts{}, config.OpenCode{})
	if strings.Contains(oc, "kill") {
		t.Errorf("opencode config allows kill: %s", oc)
	}
}

// killGlob reads a Bash(...) rule as a glob where * matches any text, which is how the
// rules are written to match.
func killGlob(rule string) *regexp.Regexp {
	body := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
	parts := strings.Split(body, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// This models Claude's matcher (a * matches any text) and does not verify it: it shows the
// rule strings agree with the commands listed, not that the CLI denies them. Check a rule
// change against the real claude CLI by hand, as for any adapter change.
func TestKillDeniedMatchesCommands(t *testing.T) {
	denies := func(cmd string) bool {
		for _, r := range KillDenied {
			if killGlob(r).MatchString(cmd) {
				return true
			}
		}
		return false
	}
	for _, cmd := range []string{
		"pkill x", "pkill -f lane", "/usr/bin/pkill x", "sudo killall x", "timeout 5 pkill x", "sh -c 'pkill x'",
		"taskkill /F /IM x.exe", "kill $(pgrep x)", "kill `pidof x`", "kill -9 $(pgrep -f lane)",
		"for p in $(pgrep -f lane); do kill $p; done", "pgrep -f lane; kill $p", "pidof x | xargs kill",
		"kill 0", "kill -9 0", "kill -1", "kill -9 -1", "kill -- -123", "kill -9 -123", "kill -s TERM -123",
	} {
		if !denies(cmd) {
			t.Errorf("no kill rule denies %q", cmd)
		}
	}
	for _, cmd := range []string{"kill 1234", "kill -9 1234", "kill -TERM 1234", "kill -15 1234", "git status", "echo skill"} {
		if denies(cmd) {
			t.Errorf("a kill rule denies %q", cmd)
		}
	}
}
