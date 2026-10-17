package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The adapter is hermetic under test: no config.yaml is read and no opencode is run.
func init() {
	openCodeSettings = func() config.OpenCode { return config.OpenCode{Bin: "/fake/opencode"} }
	openCodeProbe = func(string) (string, error) { return openCodeTested, nil }
}

// fakeOpenCode is a stand-in for `opencode run --format json`: it reads the prompt from
// standard input, reports what it was given, and plays the scenario in $MODE.
const fakeOpenCode = `#!/bin/sh
prompt=$(cat)
echo "ARGS: $(printf '%s ' "$@" | tr '\n' ' ')"
echo "STDIN: $(printf '%s' "$prompt" | tr '\n' ' ')"
echo "CONFIG: $OPENCODE_CONFIG_CONTENT"
S=ses_fake0001
ev() { printf '{"type":"%s","sessionID":"%s","part":%s}\n' "$1" "$S" "$2"; }
ev step_start '{"type":"step-start"}'
case "$MODE" in
  noedit)
    ev text '{"type":"text","text":"I will fix it now.\n<tool_call>"}'
    ev step_finish '{"type":"step-finish","reason":"stop","tokens":{"input":100,"output":5},"cost":0}'
    exit 0 ;;
  wait)
    sleep 1
    exit 0 ;;
  hang)
    ev text '{"type":"text","text":"working"}'
    sleep 30
    exit 0 ;;
  apierror)
    printf '{"type":"error","sessionID":"%s","error":{"name":"UnknownError","data":{"message":"Unexpected server error"}}}\n' "$S"
    exit 1 ;;
esac
mkdir -p src && echo "work" >> src/work.txt
ev tool_use '{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"src/work.txt"},"output":"ok"}}'
ev step_finish '{"type":"step-finish","reason":"tool-calls","tokens":{"input":90,"output":20},"cost":0}'
git add src/work.txt && git commit -q -m "agent work"
ev tool_use '{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"git commit -m x"},"output":""}}'
ev text '{"type":"text","text":"Done."}'
ev step_finish '{"type":"step-finish","reason":"stop","tokens":{"input":30,"output":4},"cost":0}'
exit 0
`

func openCodeFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	f := newFixture(t, mode)
	script := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(script, []byte(fakeOpenCode), 0o755); err != nil {
		t.Fatal(err)
	}
	f.runner.lookPath = func(string) (string, error) { return script, nil }
	return f
}

func resetVersions(t *testing.T) {
	t.Helper()
	versionMu.Lock()
	versionSeen = map[string]string{}
	versionMu.Unlock()
	t.Cleanup(func() {
		versionMu.Lock()
		versionSeen = map[string]string{}
		versionMu.Unlock()
	})
}

func logOf(t *testing.T, run store.Run) string {
	t.Helper()
	b, err := os.ReadFile(run.Log)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenCodeRunEndToEnd(t *testing.T) {
	old := openCodeSettings
	openCodeSettings = func() config.OpenCode {
		return config.OpenCode{Bin: "/fake/opencode", Endpoint: "http://localhost:11434/v1"}
	}
	t.Cleanup(func() { openCodeSettings = old })
	f := openCodeFixture(t, "ok")
	const task = "rename the zebra-quokka helper"
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Model: "local/qwen3-coder:30b", Prompt: task})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if done.State != store.RunSucceeded || done.Commits != 1 {
		t.Fatalf("run = %s, %d commit(s), error %q", done.State, done.Commits, done.Error)
	}
	if done.Session != "ses_fake0001" {
		t.Errorf("session = %q: it comes from the JSON events", done.Session)
	}
	if done.CostUSD != 0 {
		t.Errorf("cost = %v: a local model is not billed", done.CostUSD)
	}
	if done.CostEstimated {
		t.Errorf("a local model's cost is not an estimate: it has none")
	}
	log := logOf(t, done)
	if a := argsOf(t, done); strings.Contains(a, "zebra-quokka") || !strings.Contains(a, "run --format json --pure -m local/qwen3-coder:30b") {
		t.Errorf("args: %s", a)
	}
	if in := stdinOf(t, done); !strings.Contains(in, task) || !strings.Contains(in, "Work only inside") {
		t.Errorf("the prompt is not on standard input: %s", in)
	}
	for _, want := range []string{"→ write ", "120 tokens in, 24 out", "no billed cost", "opencode " + openCodeTested, "Done."} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "WARNING") {
		t.Errorf("a run that committed is warned about:\n%s", log)
	}
	// The config reached the agent in its environment, with the host and the model,
	// and nothing was written to the worktree.
	cfg := configOf(t, log)
	if !strings.Contains(cfg, `"baseURL":"http://localhost:11434/v1"`) || !strings.Contains(cfg, `"qwen3-coder:30b"`) {
		t.Errorf("config: %s", cfg)
	}
	if st := gitStatus(t, f); st != "" {
		t.Errorf("the worktree is not clean: %q", st)
	}
}

func TestOpenCodeNoEditIsNotQuietSuccess(t *testing.T) {
	f := openCodeFixture(t, "noedit")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	log := logOf(t, done)
	if !strings.Contains(log, "WARNING the model printed a tool call as text and changed nothing") {
		t.Errorf("log:\n%s", log)
	}
	// Exit 0 with nothing changed is incomplete: continued once, then a failure the
	// person sees, with the reason.
	if done.State != store.RunFailed || *done.ExitCode != 0 || !strings.Contains(done.Error, "without finishing") {
		t.Errorf("run = %s (exit %d), error %q", done.State, *done.ExitCode, done.Error)
	}
	if !strings.Contains(log, "# shepherd: ended without finishing; continued once") {
		t.Errorf("log:\n%s", log)
	}
	waitFeed(t, f, "continued once as run")
	second := f.wait(t, run.ID+1)
	if second.Parent != run.ID || second.State != store.RunFailed || second.Error != "ended twice without finishing" {
		t.Errorf("continuation = %+v", second)
	}
	if k := feedKind(t, f, "ended twice without finishing"); k != store.FeedRunFailed {
		t.Errorf("feed kind = %q", k)
	}
}

func TestOpenCodeErrorEventFailsTheRun(t *testing.T) {
	f := openCodeFixture(t, "apierror")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if done.State != store.RunFailed || done.Session != "ses_fake0001" {
		t.Errorf("run = %s, session %q", done.State, done.Session)
	}
	if log := logOf(t, done); !strings.Contains(log, "ERROR UnknownError: Unexpected server error") {
		t.Errorf("log:\n%s", log)
	}
}

func TestOpenCodeResumesWithSession(t *testing.T) {
	f := openCodeFixture(t, "ok")
	first, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "one"})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t, first.ID)
	second, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "two"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, second.ID)
	if a := argsOf(t, done); !strings.Contains(a, "-s ses_fake0001") {
		t.Errorf("a second run does not resume the session: %s", a)
	}
}

func TestOpenCodeVersionPin(t *testing.T) {
	resetVersions(t)
	old := openCodeProbe
	t.Cleanup(func() { openCodeProbe = old })
	openCodeProbe = func(string) (string, error) { return "2.0.1", nil }
	if _, err := AdapterFor("opencode"); err == nil || !strings.Contains(err.Error(), "2.0.1") || !strings.Contains(err.Error(), "1.x") {
		t.Errorf("a newer major is accepted or unexplained: %v", err)
	}
	resetVersions(t)
	openCodeProbe = func(string) (string, error) { return "1.99.0", nil }
	ad, err := AdapterFor("opencode")
	if err != nil || ad.Bin != "/fake/opencode" {
		t.Errorf("a 1.x opencode: %v, bin %q", err, ad.Bin)
	}
}

func TestOpenCodeBinaryLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	if got := openCodeBin(config.OpenCode{}); got != "" {
		t.Errorf("nothing installed, found %q", got)
	}
	std := filepath.Join(home, ".opencode", "bin", "opencode")
	os.MkdirAll(filepath.Dir(std), 0o755)
	os.WriteFile(std, []byte("#!/bin/sh\n"), 0o755)
	if got := openCodeBin(config.OpenCode{}); got != std {
		t.Errorf("standalone install: %q", got)
	}
	onPath := t.TempDir()
	os.WriteFile(filepath.Join(onPath, "opencode"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", onPath)
	if got := openCodeBin(config.OpenCode{}); got != filepath.Join(onPath, "opencode") {
		t.Errorf("PATH comes before the installer's directory: %q", got)
	}
	if got := openCodeBin(config.OpenCode{Bin: "/x/oc"}); got != "/x/oc" {
		t.Errorf("configured: %q", got)
	}
}

func TestOpenCodeConfigPermissions(t *testing.T) {
	var cfg struct {
		Permission map[string]json.RawMessage `json:"permission"`
		Provider   map[string]struct {
			NPM     string `json:"npm"`
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
			Models map[string]json.RawMessage `json:"models"`
		} `json:"provider"`
		MCP map[string]struct {
			Command []string `json:"command"`
		} `json:"mcp"`
	}
	s := openCodeConfig(Opts{Gate: "make check", Model: "local/qwen3-coder:30b", Worker: "/bin/shepherd"},
		config.OpenCode{Endpoint: "http://localhost:11434/v1"})
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	for _, k := range []string{"question", "webfetch", "websearch", "external_directory"} {
		if string(cfg.Permission[k]) != `"deny"` {
			t.Errorf("%s = %s, want deny", k, cfg.Permission[k])
		}
	}
	if string(cfg.Permission["edit"]) != `"allow"` {
		t.Errorf("edit = %s", cfg.Permission["edit"])
	}
	// "*": deny comes first and git push last: the rules are last match wins.
	bash := string(cfg.Permission["bash"])
	if !strings.HasPrefix(bash, `{"*":"deny","go test*":"allow"`) || !strings.HasSuffix(bash, `"git push*":"deny"}`) {
		t.Errorf("bash rules: %s", bash)
	}
	for _, want := range []string{`"make check":"allow"`, `"npm *":"allow"`, `"git commit*":"allow"`, `"cat *":"allow"`, `"git fetch*":"allow"`, `"git rebase origin/*":"allow"`, `"git merge-base*":"allow"`, `"git rev-parse*":"allow"`} {
		if !strings.Contains(bash, want) {
			t.Errorf("bash rules lack %s: %s", want, bash)
		}
	}
	// Check that interactive rebases are explicitly denied
	if !strings.Contains(bash, `"git rebase -i*":"deny"`) || !strings.Contains(bash, `"git rebase --interactive*":"deny"`) {
		t.Errorf("bash rules missing explicit deny for interactive rebase: %s", bash)
	}
	if strings.Contains(bash, `"make check*"`) {
		t.Errorf("the gate must match exactly, not by prefix: %s", bash)
	}
	p := cfg.Provider["local"]
	if p.NPM != "@ai-sdk/openai-compatible" || p.Options.BaseURL != "http://localhost:11434/v1" || p.Models["qwen3-coder:30b"] == nil {
		t.Errorf("provider: %+v", cfg.Provider)
	}
	if m := cfg.MCP["shepherd"]; len(m.Command) != 3 || m.Command[0] != "/bin/shepherd" {
		t.Errorf("worker tools: %+v", cfg.MCP)
	}
	if string(cfg.Permission["shepherd_*"]) != `"allow"` {
		t.Errorf("worker tools are not allowed: %v", cfg.Permission)
	}
	// An agent that may push, and merge, gets those commands; with no endpoint the
	// person's own provider is left alone.
	s = openCodeConfig(Opts{Push: true, Merge: true}, config.OpenCode{})
	if !strings.Contains(s, `"git push*":"allow"`) || !strings.Contains(s, `"glab mr merge*":"allow"`) || strings.Contains(s, `"provider"`) || strings.Contains(s, `"mcp"`) {
		t.Errorf("config: %s", s)
	}
}

// openCodeMatch mirrors how OpenCode 1.18.35 judges one command against a bash rule set:
// the last matching rule wins, "*" matches anything, and a trailing " *" also matches the
// command with no arguments.
func openCodeMatch(rules []kvRule, cmd string) string {
	verdict := ""
	for _, r := range rules {
		pat := r.pattern
		var re string
		if base, ok := strings.CutSuffix(pat, " *"); ok {
			re = regexp.QuoteMeta(base) + `( .*)?`
		} else {
			re = strings.ReplaceAll(regexp.QuoteMeta(pat), `\*`, `.*`)
		}
		if regexp.MustCompile(`^(?s)` + re + `$`).MatchString(cmd) {
			verdict = r.action
		}
	}
	return verdict
}

type kvRule struct{ pattern, action string }

// bashRules reads the bash permission block of a run's config in order.
func bashRules(t *testing.T, s string) []kvRule {
	t.Helper()
	var rules []kvRule
	// Find the block by text: it is the object after "bash":.
	i := strings.Index(s, `"bash":{`)
	if i < 0 {
		t.Fatalf("no bash block: %s", s)
	}
	dec := json.NewDecoder(strings.NewReader(s[i+len(`"bash":`):]))
	if tok, _ := dec.Token(); tok != json.Delim('{') {
		t.Fatalf("bash block: %v", tok)
	}
	for dec.More() {
		k, _ := dec.Token()
		var v string
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		rules = append(rules, kvRule{k.(string), v})
	}
	return rules
}

func TestOpenCodeBashNpmAndExactGate(t *testing.T) {
	for _, c := range []struct {
		gate string
		cmd  string
		want string
	}{
		{"make check", "npm ci", "allow"},
		{"make check", "npm", "allow"},
		{"make check", "npm test -- --run", "allow"},
		{"make check", "npm run build", "allow"},
		{"make check", "npmx run build", "deny"},
		{"make check", "npm-foo", "deny"},
		{"make check", "make check", "allow"},
		{"make check", "make check build", "deny"},
		{"make check", "make checkfoo", "deny"},
		{"make check", "make build", "deny"},
		{"make ci", "make ci", "allow"},
		{"make ci", "make check", "deny"},
		{"", "make check", "deny"},
		{"", "git push origin x", "deny"},
	} {
		rules := bashRules(t, openCodeConfig(Opts{Gate: c.gate}, config.OpenCode{}))
		if got := openCodeMatch(rules, c.cmd); got != c.want {
			t.Errorf("gate %q, %q: %q, want %q", c.gate, c.cmd, got, c.want)
		}
	}
}

// providerNames returns the provider keys of a run's config.
func providerNames(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var cfg struct {
		Provider map[string]json.RawMessage `json:"provider"`
	}
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return cfg.Provider
}

func TestOpenCodeConfigProviderRouting(t *testing.T) {
	host := config.OpenCode{Endpoint: "http://localhost:11434/v1"}
	for _, c := range []struct {
		name, model, s string
		local          bool   // a local provider block is defined
		modelID        string // its model entry, "" for none
		flag           string // the -m value
	}{
		{"bare", "qwen3-coder:30b", "host", true, "qwen3-coder:30b", "local/qwen3-coder:30b"},
		{"local prefix", "local/qwen3-coder:30b", "host", true, "qwen3-coder:30b", "local/qwen3-coder:30b"},
		{"local prefix keeps slashes", "local/org/model", "host", true, "org/model", "local/org/model"},
		{"openrouter", "openrouter/qwen/qwen3-coder", "host", false, "", "openrouter/qwen/qwen3-coder"},
		{"other prefix", "mybox/qwen", "host", false, "", "mybox/qwen"},
		{"no endpoint, bare", "qwen3-coder:30b", "none", false, "", "qwen3-coder:30b"},
		{"no endpoint, local prefix", "local/m", "none", false, "", "local/m"},
		{"no endpoint, openrouter", "openrouter/anthropic/x", "none", false, "", "openrouter/anthropic/x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := config.OpenCode{}
			if c.s == "host" {
				s = host
			}
			old := openCodeSettings
			openCodeSettings = func() config.OpenCode { return s }
			t.Cleanup(func() { openCodeSettings = old })
			prov := providerNames(t, openCodeConfig(Opts{Model: c.model}, s))
			if !c.local {
				if len(prov) != 0 {
					t.Errorf("provider block defined for %q: %v", c.model, prov)
				}
			} else {
				var p struct {
					Options struct {
						BaseURL string `json:"baseURL"`
					} `json:"options"`
					Models map[string]json.RawMessage `json:"models"`
				}
				raw, ok := prov["local"]
				if !ok || len(prov) != 1 {
					t.Fatalf("providers = %v, want only local", prov)
				}
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				if p.Options.BaseURL != host.Endpoint || len(p.Models) != 1 || p.Models[c.modelID] == nil {
					t.Errorf("local provider: %s", raw)
				}
			}
			args := strings.Join(openCodeArgs(Opts{Model: c.model}), " ")
			if !strings.Contains(args, " -m "+c.flag) || strings.Contains(args, c.flag+"/") {
				t.Errorf("args %q lack -m %s", args, c.flag)
			}
		})
	}
}

func TestOpenCodeBilledCostIsLogged(t *testing.T) {
	const session = "ses_cost"
	first := openCodeOutput(`{"type":"step_finish","sessionID":"ses_cost","part":{"type":"step-finish","reason":"tool-calls","cost":0.0125,"tokens":{"input":10,"output":2}}}`)
	if first.USD != 0.0125 {
		t.Errorf("USD = %v", first.USD)
	}
	o := openCodeOutput(`{"type":"step_finish","sessionID":"` + session + `","part":{"type":"step-finish","reason":"stop","cost":0.0075,"tokens":{"input":5,"output":1}}}`)
	if o.USD != 0.0075 || !strings.Contains(o.Show, "cost $0.0200") || strings.Contains(o.Show, "no billed cost") {
		t.Errorf("output: %+v", o)
	}
}

func TestOpenCodeHostedCostIsLabelledAnEstimate(t *testing.T) {
	step := func(session, reason string, cost float64) string {
		return fmt.Sprintf(`{"type":"step_finish","sessionID":%q,"part":{"type":"step-finish","reason":%q,"cost":%v,"tokens":{"input":10,"output":2}}}`, session, reason, cost)
	}
	hosted := openCodeReader("openrouter/moonshotai/kimi-k2")
	if o := hosted(step("or1", "tool-calls", 0.02)); o.USD != 0.02 {
		t.Errorf("USD = %v", o.USD)
	}
	o := hosted(step("or1", "stop", 0.0297))
	if !strings.Contains(o.Show, "cost $0.0497 (estimate") || strings.Contains(o.Show, "no billed cost") {
		t.Errorf("hosted with cost: %q", o.Show)
	}
	o = hosted(step("or2", "stop", 0))
	if !strings.Contains(o.Show, "cost unknown") || strings.Contains(o.Show, "no billed cost") {
		t.Errorf("hosted with no cost: %q", o.Show)
	}
	local := openCodeReader("qwen3-coder:30b")
	if o := local(step("loc", "stop", 0)); !strings.Contains(o.Show, "no billed cost (a local model") {
		t.Errorf("local: %q", o.Show)
	}
	if !openCodeHosted("openrouter/a/b") || openCodeHosted("local/x") || openCodeHosted("x") || openCodeHosted("") {
		t.Error("openCodeHosted")
	}
}

// The window in the config is the local endpoint's; a hosted model's is left unknown.
func TestOpenCodeUsageWindowIsLocalOnly(t *testing.T) {
	old := openCodeSettings
	openCodeSettings = func() config.OpenCode {
		return config.OpenCode{Bin: "/fake/opencode", Endpoint: "http://localhost:11434/v1"}
	}
	t.Cleanup(func() { openCodeSettings = old })
	want, _ := openCodeSettings().Limits()
	ad := adapters["opencode"]
	line := `{"type":"step_finish","sessionID":"w","part":{"tokens":{"input":30000,"output":5,"cache":{}}}}`
	for model, window := range map[string]int{"qwen3-coder:30b": want, "local/x": want, "openrouter/moonshotai/kimi-k2": 0} {
		r := ad.Usage(Opts{Model: model})
		r.Feed(line)
		if got := r.Usage().ContextWindow; got != window {
			t.Errorf("%s: window = %d, want %d", model, got, window)
		}
	}
}

func TestOpenCodeModelNaming(t *testing.T) {
	for _, c := range []struct {
		model string
		host  bool
		want  string
	}{
		{"", true, ""},
		{"qwen3-coder:30b", true, "local/qwen3-coder:30b"},
		{"bench/qwen3-coder:30b", true, "bench/qwen3-coder:30b"},
		{"openrouter/qwen/qwen3-coder", true, "openrouter/qwen/qwen3-coder"},
		{"qwen3-coder:30b", false, "qwen3-coder:30b"},
	} {
		if got := openCodeModel(c.model, c.host); got != c.want {
			t.Errorf("openCodeModel(%q, %v) = %q, want %q", c.model, c.host, got, c.want)
		}
	}
}

func TestOpenCodeOutputEvents(t *testing.T) {
	o := openCodeOutput(`{"type":"step_finish","sessionID":"s1","part":{"reason":"tool-calls","cost":0.5,"tokens":{"input":10,"output":2}}}`)
	if o.USD != 0.5 || o.Show != "" {
		t.Errorf("a step's cost is read and a mid-run step is quiet: %+v", o)
	}
	if o := openCodeOutput("not json at all"); o.Show != "not json at all" {
		t.Errorf("a plain line is kept: %+v", o)
	}
	o = openCodeOutput(`{"type":"tool_use","sessionID":"s2","part":{"tool":"bash","state":{"status":"error","input":{"command":"rm x"},"error":"denied by a rule"}}}`)
	if !strings.Contains(o.Show, "bash") || !strings.Contains(o.Show, "denied by a rule") {
		t.Errorf("a refused tool call: %+v", o)
	}
	if id := openCodeSession(`{"type":"text","sessionID":"ses_abc","part":{}}`); id != "ses_abc" {
		t.Errorf("session = %q", id)
	}
	if id := openCodeSession("plain"); id != "" {
		t.Errorf("session from a plain line = %q", id)
	}
}

func TestOpenCodeAttachResumesSession(t *testing.T) {
	ad, err := AdapterFor("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ad.Attach("ses_x", "/w"), " "); got != "-s ses_x" {
		t.Errorf("attach = %q", got)
	}
}

// configOf is the CONFIG line the fake agent printed.
func configOf(t *testing.T, log string) string {
	t.Helper()
	for _, l := range strings.Split(log, "\n") {
		if rest, ok := strings.CutPrefix(l, "CONFIG: "); ok {
			return rest
		}
	}
	t.Fatalf("no CONFIG line in:\n%s", log)
	return ""
}

func gitStatus(t *testing.T, f *fixture) string {
	t.Helper()
	lane, err := f.st.Lane(context.Background(), f.lane.ID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", lane.Worktree, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestOpenCodeNoChangeIsNotFailedWhenTheRunAsked(t *testing.T) {
	f := openCodeFixture(t, "wait")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "ask first"})
	if err != nil {
		t.Fatal(err)
	}
	// A run that reported through Shepherd's tools did something, even if it changed no file.
	if _, err := f.runner.Record(context.Background(), store.Event{RunID: run.ID, Kind: EventReport, Status: "blocked", Text: "need the schema"}); err != nil {
		t.Fatal(err)
	}
	if done := f.wait(t, run.ID); done.State != store.RunSucceeded {
		t.Errorf("run = %s, error %q", done.State, done.Error)
	}
}

func TestNoChangeStaysSucceededForOtherAgents(t *testing.T) {
	// Another agent's no-change run stays succeeded: the verdict is the adapter's.
	f := newFixture(t, "quick")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if done := f.wait(t, run.ID); done.State != store.RunSucceeded || done.Commits != 0 {
		t.Errorf("claude run = %s, %d commit(s), error %q", done.State, done.Commits, done.Error)
	}
}

func TestOpenCodeIdleRunIsStoppedAndFailed(t *testing.T) {
	old := openCodeIdle
	openCodeIdle = func() time.Duration { return 400 * time.Millisecond }
	t.Cleanup(func() { openCodeIdle = old })
	f := openCodeFixture(t, "hang")
	start := time.Now()
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if time.Since(start) > 15*time.Second {
		t.Errorf("took %s: not stopped for being idle", time.Since(start))
	}
	if done.State != store.RunFailed || !strings.Contains(done.Error, "printed nothing for 400ms") {
		t.Errorf("run = %s, error %q", done.State, done.Error)
	}
	if k := feedKind(t, f, "printed nothing"); k != store.FeedRunFailed {
		t.Errorf("feed kind = %q", k)
	}
}

func TestOpenCodeSilenceUnderTheLimitIsNotIdle(t *testing.T) {
	// A run that was quiet for less than the limit is not idle.
	old := openCodeIdle
	openCodeIdle = func() time.Duration { return 2500 * time.Millisecond }
	t.Cleanup(func() { openCodeIdle = old })
	f := openCodeFixture(t, "wait") // prints, then sleeps 1s: under the limit
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	f.runner.Record(context.Background(), store.Event{RunID: run.ID, Kind: EventReport, Status: "progress", Text: "p"})
	if done := f.wait(t, run.ID); done.State != store.RunSucceeded {
		t.Errorf("run = %s, error %q", done.State, done.Error)
	}
}

func TestOpenCodeConfigIsInTheRunsEnvironmentOnly(t *testing.T) {
	t.Setenv(openCodeEnvVar, "") // so the check below is of this test's runs
	os.Unsetenv(openCodeEnvVar)
	f := openCodeFixture(t, "ok")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Prompt: "one"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if !strings.Contains(configOf(t, logOf(t, done)), `"permission"`) {
		t.Errorf("the run did not get its config:\n%s", logOf(t, done))
	}
	if v, ok := os.LookupEnv(openCodeEnvVar); ok {
		t.Errorf("%s is set in the daemon after Start: %.40s", openCodeEnvVar, v)
	}
	// Another agent started afterwards does not see it.
	printer := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(printer, []byte("#!/bin/sh\ncat >/dev/null\necho \"OC=[$OPENCODE_CONFIG_CONTENT]\"\n"), 0o755)
	f.runner.lookPath = func(string) (string, error) { return printer, nil }
	other, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if log := logOf(t, f.wait(t, other.ID)); !strings.Contains(log, "OC=[]") {
		t.Errorf("another agent inherited the config:\n%s", log)
	}
}

// OpenCode treats a linked worktree as outside its project, so the worktree's own path is
// the one external directory the run may use; everything else stays denied, last match wins.
func TestOpenCodeConfigAllowsOnlyTheWorktree(t *testing.T) {
	wt := t.TempDir()
	var cfg struct {
		Permission map[string]json.RawMessage `json:"permission"`
	}
	s := openCodeConfig(Opts{Worktree: wt}, config.OpenCode{})
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	ext := string(cfg.Permission["external_directory"])
	if !strings.HasPrefix(ext, `{"*":"deny",`) {
		t.Errorf("external_directory must deny by default first: %s", ext)
	}
	var rules map[string]string
	if err := json.Unmarshal([]byte(ext), &rules); err != nil {
		t.Fatal(err)
	}
	for k, v := range rules {
		if k == "*" {
			continue
		}
		if v != "allow" || !strings.Contains(k, filepath.Base(wt)) {
			t.Errorf("unexpected external_directory rule %q: %q", k, v)
		}
	}
	if rules[filepath.Join(wt, "*")] != "allow" {
		t.Errorf("worktree not allowed: %s", ext)
	}
	// No worktree known: nothing outside is allowed.
	if got := string(openCodeConfig(Opts{}, config.OpenCode{})); !strings.Contains(got, `"external_directory":"deny"`) {
		t.Errorf("no worktree should keep a plain deny: %s", got)
	}
	a := strings.Join(openCodeArgs(Opts{Worktree: wt}), " ")
	if !strings.Contains(a, "--dir "+wt) {
		t.Errorf("args lack --dir: %s", a)
	}
}

func TestOpenCodeConfigLimits(t *testing.T) {
	limit := func(s config.OpenCode) string {
		var cfg struct {
			Provider map[string]struct {
				Models map[string]struct {
					Limit map[string]int `json:"limit"`
				} `json:"models"`
			} `json:"provider"`
		}
		out := openCodeConfig(Opts{Model: "local/m"}, s)
		if err := json.Unmarshal([]byte(out), &cfg); err != nil {
			t.Fatalf("not JSON: %v", err)
		}
		l := cfg.Provider["local"].Models["m"].Limit
		return strconv.Itoa(l["context"]) + "/" + strconv.Itoa(l["output"])
	}
	if got := limit(config.OpenCode{Endpoint: "http://h/v1"}); got != "32768/8192" {
		t.Errorf("default limits = %s", got)
	}
	if got := limit(config.OpenCode{Endpoint: "http://h/v1", Context: 65536, Output: 4096}); got != "65536/4096" {
		t.Errorf("configured limits = %s", got)
	}
}

func TestOpenCodeConfigTimeouts(t *testing.T) {
	type provider struct {
		Options struct {
			Timeout      any   `json:"timeout"`
			ChunkTimeout int64 `json:"chunkTimeout"`
		} `json:"options"`
	}
	got := func(s config.OpenCode) (timeout any, chunk int64, has bool) {
		var cfg struct {
			Provider map[string]provider `json:"provider"`
		}
		out := openCodeConfig(Opts{Model: "local/m"}, s)
		if err := json.Unmarshal([]byte(out), &cfg); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		p, ok := cfg.Provider["local"]
		if !ok {
			return nil, 0, false
		}
		return p.Options.Timeout, p.Options.ChunkTimeout, true
	}
	timeout, chunk, has := got(config.OpenCode{Endpoint: "http://h/v1"})
	if !has || timeout != float64(3600000) || chunk != 600000 {
		t.Errorf("default timeouts = %v / %d (present %v), want 3600000 / 600000", timeout, chunk, has)
	}
	timeout, chunk, has = got(config.OpenCode{Endpoint: "http://h/v1", RequestTimeout: "30m", ChunkTimeout: "30s"})
	if !has || timeout != float64(1800000) || chunk != 30000 {
		t.Errorf("configured timeouts = %v / %d, want 1800000 / 30000", timeout, chunk)
	}
	timeout, chunk, has = got(config.OpenCode{Endpoint: "http://h/v1", RequestTimeout: "off", ChunkTimeout: "10s"})
	if !has || timeout != false || chunk != 10000 {
		t.Errorf("off request timeout = %v / %d, want false / 10000", timeout, chunk)
	}
	if _, _, has = got(config.OpenCode{}); has {
		t.Error("no endpoint should write no provider")
	}
}

func TestOpenCodeHostedRunIsMarkedEstimated(t *testing.T) {
	old := openCodeSettings
	openCodeSettings = func() config.OpenCode {
		return config.OpenCode{Bin: "/fake/opencode", Endpoint: "http://localhost:11434/v1"}
	}
	t.Cleanup(func() { openCodeSettings = old })
	f := openCodeFixture(t, "ok")
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "opencode", Model: "openrouter/qwen/qwen3-coder", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	done := f.wait(t, run.ID)
	if !done.CostEstimated {
		t.Errorf("a hosted run is not marked as an estimate")
	}
	u, err := f.st.RunUsage(context.Background(), done.ID)
	if err != nil || !u.CostEstimated {
		t.Errorf("usage = %+v, %v: the record lacks the estimate mark", u, err)
	}
}
