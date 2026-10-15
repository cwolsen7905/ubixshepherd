package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingFileIsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Daemon.Listen != "127.0.0.1:0" {
		t.Errorf("listen = %q", c.Daemon.Listen)
	}
	p := c.Profile("anything")
	if p.BaseBranch != "main" || p.BranchModel != Trunk {
		t.Errorf("default profile = %+v", p)
	}
	if p.Autonomy.Merge != Human || p.Autonomy.Tag != Human || p.Autonomy.Deploy != Human {
		t.Errorf("default autonomy is not cautious: %+v", p.Autonomy)
	}
	if p.Autonomy.PlanFirst == nil || !*p.Autonomy.PlanFirst {
		t.Error("default plan_first should be true")
	}
}

func TestRepoProfileInherits(t *testing.T) {
	c, err := Parse([]byte(`
defaults:
  gate: make check
repos:
  framework:
    shared_paths: [README.md, ".gitlab-ci.yml"]
    autonomy:
      tag: agent
  product:
    base_branch: dev
    branch_model: promotion
    promotion: [dev, staging, main]
    autonomy:
      plan_first: false
`))
	if err != nil {
		t.Fatal(err)
	}
	fw := c.Profile("framework")
	if fw.Gate != "make check" || fw.BaseBranch != "main" || fw.Autonomy.Tag != Agent || fw.Autonomy.Merge != Human {
		t.Errorf("framework = %+v", fw)
	}
	if len(fw.SharedPaths) != 2 {
		t.Errorf("framework shared paths = %v", fw.SharedPaths)
	}
	pr := c.Profile("product")
	if pr.BaseBranch != "dev" || pr.BranchModel != Promotion || *pr.Autonomy.PlanFirst {
		t.Errorf("product = %+v", pr)
	}
}

func TestRepoMaxRuns(t *testing.T) {
	c, err := Parse([]byte("defaults:\n  max_runs: 3\nrepos:\n  big:\n    max_runs: 2\n  plain: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Profile("big").MaxRuns; got != 2 {
		t.Errorf("big = %d", got)
	}
	if got := c.Profile("plain").MaxRuns; got != 3 {
		t.Errorf("plain inherits defaults: %d", got)
	}
	if got := Default().Profile("x").MaxRuns; got != 0 {
		t.Errorf("unset = %d, want 0", got)
	}
	if _, err := Parse([]byte("repos:\n  x:\n    max_runs: -1\n")); err == nil {
		t.Error("negative max_runs accepted")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":       "defaults:\n  base_brnch: dev\n",
		"bad model":         "defaults:\n  branch_model: gitflow\n",
		"promotion too few": "repos:\n  x:\n    branch_model: promotion\n    promotion: [dev]\n",
		"bad autonomy":      "repos:\n  x:\n    autonomy:\n      merge: sometimes\n",
		"public listen":     "daemon:\n  listen: 0.0.0.0:7400\n",
		"bad listen":        "daemon:\n  listen: nope\n",
		"bad push":          "repos:\n  x:\n    autonomy:\n      push: robot\n",
		"shepherd no gate":  "repos:\n  x:\n    autonomy:\n      push: shepherd\n",
		"bad permission":    "defaults:\n  agent:\n    permission_mode: yolo\n",
		"bad log level":     "daemon:\n  log_level: loud\n",
		"log level case":    "daemon:\n  log_level: DEBUG\n",
		"desk model spaces": "desk:\n  model: \"my model\"\n",
		"model agent":       "defaults:\n  agent:\n    model: {gemini: pro}\n",
		"model empty":       "repos:\n  x:\n    agent:\n      model: {claude: \"\"}\n",
	}
	for name, in := range cases {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseNamesTheField(t *testing.T) {
	_, err := Parse([]byte("repos:\n  x:\n    autonomy:\n      deploy: robot\n"))
	if err == nil || !strings.Contains(err.Error(), "repos.x.autonomy.deploy") {
		t.Errorf("error does not name the field: %v", err)
	}
}

func TestAgentPowers(t *testing.T) {
	if p := Default().Profile("x"); p.Agent.PermissionMode != PermAuto || p.Autonomy.Push != Human {
		t.Errorf("default agent powers = %+v %+v", p.Agent, p.Autonomy)
	}
	// An agent pushing needs no gate: Shepherd runs none before the agent's push.
	c, err := Parse([]byte(`
defaults:
  agent:
    permission_mode: acceptEdits
repos:
  app:
    agent:
      permission_mode: bypassPermissions
    autonomy:
      push: agent
      merge: agent
`))
	if err != nil {
		t.Fatal(err)
	}
	if app := c.Profile("app"); app.Agent.PermissionMode != PermBypass || app.Autonomy.Push != Agent || app.Autonomy.Merge != Agent {
		t.Errorf("app = %+v", app)
	}
	if other := c.Profile("other"); other.Agent.PermissionMode != PermAcceptEdits || other.Autonomy.Push != Human {
		t.Errorf("other = %+v", other)
	}
}

func TestEmptyFileIsDefault(t *testing.T) {
	if _, err := Parse(nil); err != nil {
		t.Errorf("empty file: %v", err)
	}
}

func TestLogLevel(t *testing.T) {
	if Default().Daemon.LogLevel != "info" {
		t.Errorf("default log level = %q", Default().Daemon.LogLevel)
	}
	for _, l := range LogLevels {
		c, err := Parse([]byte("daemon:\n  log_level: " + l + "\n"))
		if err != nil || c.Daemon.LogLevel != l {
			t.Errorf("log_level %s: %q %v", l, c.Daemon.LogLevel, err)
		}
	}
}

func TestModels(t *testing.T) {
	c, err := Parse([]byte("desk:\n  model: opus\ndefaults:\n  agent:\n    model: {claude: sonnet, cursor: auto}\n" +
		"repos:\n  x:\n    agent:\n      model: {claude: opus}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Desk.Model != "opus" {
		t.Errorf("desk.model = %q", c.Desk.Model)
	}
	if m := c.Profile("x").Agent.Model; m["claude"] != "opus" || m["cursor"] != "auto" {
		t.Errorf("repo models = %v", m)
	}
	if m := c.Profile("y").Agent.Model; m["claude"] != "sonnet" {
		t.Errorf("default models = %v", m)
	}
	if c.Defaults.Agent.Model["claude"] != "sonnet" {
		t.Error("a repo's models changed the defaults'")
	}
}

func TestDeskWake(t *testing.T) {
	if m := Default().Desk.WakeMode(); m != WakeAttached {
		t.Errorf("default wake = %q", m)
	}
	for _, w := range WakeModes {
		if _, err := Parse([]byte("desk:\n  wake: " + w + "\n")); err != nil {
			t.Errorf("wake %s: %v", w, err)
		}
	}
	if _, err := Parse([]byte("desk:\n  wake: sometimes\n")); err == nil || !strings.Contains(err.Error(), "desk.wake") {
		t.Errorf("bad wake: %v", err)
	}
}

func TestChatHyperlinks(t *testing.T) {
	if m := Default().Chat.HyperlinksMode(); m != HyperlinksAuto {
		t.Errorf("default hyperlinks = %q", m)
	}
	for _, m := range HyperlinkModes {
		c, err := Parse([]byte("chat:\n  hyperlinks: " + m + "\n"))
		if err != nil {
			t.Errorf("hyperlinks %s: %v", m, err)
		} else if c.Chat.HyperlinksMode() != m {
			t.Errorf("hyperlinks %s read as %q", m, c.Chat.HyperlinksMode())
		}
	}
	if _, err := Parse([]byte("chat:\n  hyperlinks: sometimes\n")); err == nil || !strings.Contains(err.Error(), "chat.hyperlinks") {
		t.Errorf("bad hyperlinks: %v", err)
	}
}

func TestOpenCode(t *testing.T) {
	c, err := Parse([]byte("opencode:\n  bin: /opt/oc/bin/opencode\n  endpoint: http://localhost:11434/v1\n" +
		"defaults:\n  agent:\n    model: {opencode: \"local/qwen3-coder:30b\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenCode.Bin != "/opt/oc/bin/opencode" || c.OpenCode.Endpoint != "http://localhost:11434/v1" {
		t.Errorf("opencode = %+v", c.OpenCode)
	}
	if m := c.Profile("x").Agent.Model["opencode"]; m != "local/qwen3-coder:30b" {
		t.Errorf("model = %q", m)
	}
	for name, in := range map[string]string{
		"no scheme":   "opencode:\n  endpoint: localhost:11434\n",
		"credentials": "opencode:\n  endpoint: http://user:pw@localhost:11434/v1\n",
		"not http":    "opencode:\n  endpoint: ftp://localhost/v1\n",
		"blank bin":   "opencode:\n  bin: \" oc\"\n",
		"unknown key": "opencode:\n  port: 1\n",
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOpenCodeIdleTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"": DefaultOpenCodeIdle, "off": 0, "30m": 30 * time.Minute} {
		c, err := Parse([]byte("opencode:\n  idle_timeout: \"" + in + "\"\n"))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := c.OpenCode.Idle(); got != want {
			t.Errorf("idle_timeout %q = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"10s", "soon", "-5m"} {
		if _, err := Parse([]byte("opencode:\n  idle_timeout: " + in + "\n")); err == nil {
			t.Errorf("idle_timeout %q accepted", in)
		}
	}
}

func TestOpenCodeProviderTimeouts(t *testing.T) {
	c, err := Parse([]byte("opencode: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenCode.Request() != DefaultOpenCodeRequest || c.OpenCode.Chunk() != DefaultOpenCodeChunk {
		t.Errorf("defaults = %v / %v", c.OpenCode.Request(), c.OpenCode.Chunk())
	}
	for _, tc := range []struct {
		in         string
		req, chunk time.Duration
	}{
		{"request_timeout: \"off\"\n  chunk_timeout: 30s", 0, 30 * time.Second},
		{"request_timeout: 30m\n  chunk_timeout: 5m", 30 * time.Minute, 5 * time.Minute},
		{"request_timeout: 1m\n  chunk_timeout: 10s", time.Minute, 10 * time.Second},
	} {
		c, err := Parse([]byte("opencode:\n  " + tc.in + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if c.OpenCode.Request() != tc.req || c.OpenCode.Chunk() != tc.chunk {
			t.Errorf("%q = %v / %v, want %v / %v", tc.in, c.OpenCode.Request(), c.OpenCode.Chunk(), tc.req, tc.chunk)
		}
	}
	for _, in := range []string{
		"request_timeout: 10s",
		"request_timeout: soon",
		"request_timeout: -5m",
		"chunk_timeout: off",
		"chunk_timeout: 5s",
		"chunk_timeout: soon",
		"chunk_timeout: -1m",
	} {
		if _, err := Parse([]byte("opencode:\n  " + in + "\n")); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestDeskRotation(t *testing.T) {
	d := Default().Desk
	if d.RotateAt() != DefaultRotateTokens || d.RotateUSD() != 0 || d.SummaryCap() != DefaultSummaryChars ||
		d.SummaryTurnCount() != DefaultSummaryTurns || d.ToolOutputCap() != DefaultToolOutputChars || d.SummaryModel != "" {
		t.Errorf("defaults = %+v", d)
	}
	c, err := Parse([]byte("desk:\n  rotate_tokens: 0\n  rotate_cost: 2.5\n  summary_model: haiku\n  summary_chars: 3000\n  summary_turns: 0\n  tool_output_chars: 800\n"))
	if err != nil {
		t.Fatal(err)
	}
	d = c.Desk
	if d.RotateAt() != 0 || d.RotateUSD() != 2.5 || d.SummaryModel != "haiku" || d.SummaryCap() != 3000 ||
		d.SummaryTurnCount() != 0 || d.ToolOutputCap() != 800 {
		t.Errorf("set = %+v", d)
	}
	for in, want := range map[string]string{
		"rotate_tokens: 500":     "desk.rotate_tokens: 500; at least 50000",
		"rotate_tokens: -1":      "desk.rotate_tokens",
		"rotate_cost: 0":         "desk.rotate_cost",
		"rotate_cost: -3":        "desk.rotate_cost",
		"summary_model: \"a b\"": "desk.summary_model",
		"summary_chars: 10":      "desk.summary_chars: 10; at least 1000",
		"summary_turns: 99":      "desk.summary_turns: 99; 0 to 50",
		"summary_turns: -1":      "desk.summary_turns",
		"tool_output_chars: 100": "desk.tool_output_chars: 100; at least 500",
		"rotate_tokens: lots":    "cannot unmarshal",
		"rotate_after_turns: 3":  "rotate_after_turns",
	} {
		if _, err := Parse([]byte("desk:\n  " + in + "\n")); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", in, err, want)
		}
	}
}

func TestOpenCodeLimits(t *testing.T) {
	c, err := Parse([]byte("opencode:\n  endpoint: http://localhost:11434/v1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ctx, out := c.OpenCode.Limits(); ctx != 32768 || out != 8192 {
		t.Errorf("defaults = %d, %d", ctx, out)
	}
	c, err = Parse([]byte("opencode:\n  context: 65536\n  output: 4096\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ctx, out := c.OpenCode.Limits(); ctx != 65536 || out != 4096 {
		t.Errorf("limits = %d, %d", ctx, out)
	}
	// Setting only one is checked against the other's default.
	for name, in := range map[string]string{
		"negative context":   "opencode:\n  context: -1\n",
		"negative output":    "opencode:\n  output: -1\n",
		"output ge context":  "opencode:\n  context: 4096\n  output: 4096\n",
		"context below dflt": "opencode:\n  context: 8000\n",
		"not a number":       "opencode:\n  context: lots\n",
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStandardsFields(t *testing.T) {
	c, err := Parse([]byte(`
defaults:
  standards: [house]
repos:
  a: {}
  b:
    standards: [other, house]
    standards_docs: [docs/rules]
    standards_pack_dir: .rules
  c:
    standards: []
`))
	if err != nil {
		t.Fatal(err)
	}
	a, b, d := c.Profile("a"), c.Profile("b"), c.Profile("c")
	if len(a.Standards) != 1 || a.Standards[0] != "house" || a.StandardsPackDir != ".shepherd/standards" || len(a.StandardsDocs) != 4 {
		t.Errorf("inherited: %+v", a)
	}
	if len(b.Standards) != 2 || len(b.StandardsDocs) != 1 || b.StandardsPackDir != ".rules" {
		t.Errorf("overridden: %+v", b)
	}
	if len(d.Standards) != 0 {
		t.Errorf("an empty list does not clear the defaults': %v", d.Standards)
	}
}

func TestStandardsValidation(t *testing.T) {
	for name, y := range map[string]string{
		"pack path":   "defaults:\n  standards: [../x]\n",
		"abs docs":    "defaults:\n  standards_docs: [/etc]\n",
		"escape docs": "defaults:\n  standards_docs: [docs/../../x]\n",
		"pack dir":    "defaults:\n  standards_pack_dir: ../elsewhere\n",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// withRetired sets the retired-key table for one test.
func withRetired(t *testing.T, m map[string]string) {
	t.Helper()
	old := RetiredKeys
	RetiredKeys = m
	t.Cleanup(func() { RetiredKeys = old })
}

func TestRetiredKeysWarnInsteadOfFailing(t *testing.T) {
	withRetired(t, map[string]string{
		"daemon.old_poll":  "use daemon.poll",
		"repos.*.old_gate": "use gate",
	})
	src := "daemon:\n  old_poll: 30s\n  max_runs: 3\nrepos:\n  a:\n    old_gate: make\n    base_branch: dev\n  b:\n    old_gate: make\n"
	c, warns, err := ParseWarn([]byte(src))
	if err != nil {
		t.Fatalf("retired keys must not fail: %v", err)
	}
	if c.Daemon.MaxRuns != 3 || c.Repos["a"].BaseBranch != "dev" {
		t.Errorf("the rest of the file was not loaded: %+v", c.Daemon)
	}
	want := []string{
		"daemon.old_poll: retired key, ignored: use daemon.poll",
		"repos.a.old_gate: retired key, ignored: use gate",
		"repos.b.old_gate: retired key, ignored: use gate",
	}
	if strings.Join(warns, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings = %q, want %q", warns, want)
	}
	// Parse and Load take it the same way.
	if _, err := Parse([]byte(src)); err != nil {
		t.Errorf("Parse: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(src), 0o600)
	if _, w, err := LoadWarn(path); err != nil || len(w) != 3 {
		t.Errorf("LoadWarn = %q, %v", w, err)
	}
}

func TestTyposStillFailWithRetiredKeys(t *testing.T) {
	withRetired(t, map[string]string{"daemon.old_poll": "use daemon.poll"})
	_, _, err := ParseWarn([]byte("daemon:\n  old_poll: 30s\n  pol: 30s\n"))
	if err == nil || !strings.Contains(err.Error(), "pol") {
		t.Errorf("a typo next to a retired key = %v", err)
	}
	// No retired key present: no warnings, and the file is untouched.
	if _, w, err := ParseWarn([]byte("daemon:\n  max_runs: 2\n")); err != nil || len(w) != 0 {
		t.Errorf("clean file: %q, %v", w, err)
	}
	if _, w, err := ParseWarn(nil); err != nil || len(w) != 0 {
		t.Errorf("empty file: %q, %v", w, err)
	}
}

func TestNoRetiredKeysShipped(t *testing.T) {
	// Tests set the table for themselves; the shipped one starts empty.
	if len(RetiredKeys) != 0 {
		t.Errorf("RetiredKeys = %v", RetiredKeys)
	}
}

func TestWebEditorURL(t *testing.T) {
	if got := Default().Web.EditorTemplate(); got != "vscode://file/{path}" {
		t.Errorf("default = %q", got)
	}
	if c, err := Parse([]byte("")); err != nil || c.Web.EditorURL == nil || *c.Web.EditorURL != DefaultEditorURL {
		t.Errorf("unset: %+v, %v", c.Web, err)
	}
	for _, tpl := range []string{"cursor://file/{path}", "idea://open?file={path}&line={line}", "vscode://file/{path}:{line}", ""} {
		c, err := Parse([]byte("web:\n  editor_url: \"" + tpl + "\"\n"))
		if err != nil {
			t.Errorf("%q: %v", tpl, err)
		} else if got := c.Web.EditorTemplate(); got != tpl {
			t.Errorf("%q came back as %q", tpl, got)
		}
	}
	for _, tpl := range []string{"javascript:alert({path})", "data:text/html,{path}", "file://{path}", "/just/{path}",
		"vscode://file/", "vscode://file/{file}", "vscode://file/{path", "vscode://file/{path}}", "://{path}", "my app://{path}"} {
		_, err := Parse([]byte("web:\n  editor_url: \"" + tpl + "\"\n"))
		if err == nil || !strings.Contains(err.Error(), "web.editor_url") {
			t.Errorf("%q: %v", tpl, err)
		}
	}
}

func TestUpdateSettings(t *testing.T) {
	c, err := Parse([]byte(""))
	if err != nil || !c.Update.Enabled() || c.Update.ChannelName() != UpdateBeta {
		t.Fatalf("defaults: %+v %v", c.Update, err)
	}
	c, err = Parse([]byte("update:\n  check: false\n  channel: stable\n"))
	if err != nil || c.Update.Enabled() || c.Update.ChannelName() != UpdateStable {
		t.Fatalf("set: %+v %v", c.Update, err)
	}
	if _, err := Parse([]byte("update:\n  channel: nightly\n")); err == nil || !strings.Contains(err.Error(), "update.channel") {
		t.Errorf("a channel outside the set was accepted: %v", err)
	}
}
