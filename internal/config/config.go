// Package config loads Shepherd's machine configuration: how the daemon listens, and the
// repo profiles that say how each repo works (base branch, branch model, gate, shared
// paths, autonomy).
//
// A missing file is not an error: every field has a default, and the defaults are
// cautious (a human merges, tags and deploys; agents plan first).
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is the whole file.
type Config struct {
	Daemon Daemon `yaml:"daemon" json:"daemon"`
	Desk   Desk   `yaml:"desk" json:"desk"`
	Chat   Chat   `yaml:"chat" json:"chat"`
	// Update says whether the daemon checks for new releases and which it counts
	// (docs/update-check.md).
	Update Update `yaml:"update,omitempty" json:"update"`
	// KeepGoing bounds keep-going mode, the switch that lets the front desk act on what
	// is already approved (docs/keep-going.md).
	KeepGoing KeepGoing `yaml:"keep_going,omitempty" json:"keep_going"`
	// Next holds the next-up list's settings (docs/design/next-up-guards.md).
	Next Next `yaml:"next,omitempty" json:"next"`
	// Web holds the browser UI's settings; the page reads them from GET /v1/config.
	Web      Web                `yaml:"web" json:"web"`
	OpenCode OpenCode           `yaml:"opencode" json:"opencode,omitempty"`
	Defaults Profile            `yaml:"defaults" json:"defaults"`
	Repos    map[string]Profile `yaml:"repos" json:"repos,omitempty"`
	// Projects are named groups of repos with a brief, a budget and intake rules. A repo
	// in no project behaves as it did before projects.
	Projects map[string]Project `yaml:"projects,omitempty" json:"projects,omitempty"`
	// Models is the model policy by agent: the catalogue the front desk chooses from,
	// and the defaults. Projects and repos narrow it (see docs/models.md).
	Models map[string]AgentModels `yaml:"models,omitempty" json:"models,omitempty"`
}

// Daemon holds the daemon's own settings.
type Daemon struct {
	// Listen is a loopback host:port. Port 0 picks a free port; clients find it in the
	// daemon's runtime file.
	Listen string `yaml:"listen" json:"listen"`
	// MaxRuns is how many agents may run at once on this machine.
	MaxRuns int `yaml:"max_runs" json:"max_runs"`
	// Poll is how often open lanes' merge requests are checked: "60s", "2m"; "off" stops it.
	Poll string `yaml:"poll" json:"poll"`
	// Budget is the daily spend, in dollars, past which Shepherd holds the runs it would
	// start on its own (fixes, routed requests); 0 means no cap. It warns at 80%.
	Budget *float64 `yaml:"budget" json:"budget"`
	// BudgetCap is soft (warns, never holds) or hard (also holds the runs Shepherd starts
	// itself); "" is hard, which is what Budget always did.
	BudgetCap string `yaml:"budget_cap,omitempty" json:"budget_cap,omitempty"`
	// CreditUSD prices one Copilot credit, which Copilot reports instead of dollars.
	CreditUSD *float64 `yaml:"credit_usd" json:"credit_usd"`
	// LogLevel is what the daemon logs: debug, info, warn or error (one of LogLevels).
	LogLevel string `yaml:"log_level" json:"log_level"`
}

// OpenCode says where the OpenCode CLI is and which model host it talks to. It is a
// machine setting, not a repo's: the binary and the host are properties of this machine.
type OpenCode struct {
	// Bin is the opencode executable; "" looks for opencode on PATH, then in
	// ~/.opencode/bin (where its standalone installer puts it).
	Bin string `yaml:"bin" json:"bin,omitempty"`
	// Endpoint is the base URL of an OpenAI-compatible model host (an Ollama's
	// http://localhost:11434/v1). When set, Shepherd defines a provider for each run
	// from it and the run's model, named <provider>/<model> in agent.model.opencode;
	// a model without a provider is run under the provider "local". "" leaves the
	// provider to the person's own OpenCode configuration.
	Endpoint string `yaml:"endpoint" json:"endpoint,omitempty"`
	// IdleTimeout is how long a run may print nothing before Shepherd stops it and
	// records it failed as hung: a duration of at least a minute, or "off". "" is
	// DefaultOpenCodeIdle. Any output resets it.
	IdleTimeout string `yaml:"idle_timeout" json:"idle_timeout,omitempty"`
	// RequestTimeout is how long one provider request may take when Endpoint is set:
	// a duration of at least a minute, or "off" (no limit). "" is
	// DefaultOpenCodeRequest. It bounds the request, not a quiet run (that is
	// IdleTimeout).
	RequestTimeout string `yaml:"request_timeout" json:"request_timeout,omitempty"`
	// ChunkTimeout is how long a stream may go without a chunk before that request
	// ends: a duration of at least 10 seconds. "" is DefaultOpenCodeChunk. OpenCode
	// accepts only a positive number of milliseconds here, so there is no "off".
	ChunkTimeout string `yaml:"chunk_timeout" json:"chunk_timeout,omitempty"`
	// Context and Output are the token limits told to OpenCode for the model run under
	// Endpoint; they decide when OpenCode compacts, and do not size the model host's
	// own context. 0 means DefaultOpenCodeContext and DefaultOpenCodeOutput.
	Context int `yaml:"context" json:"context,omitempty"`
	Output  int `yaml:"output" json:"output,omitempty"`
}

// OpenCode's model limits when none are configured.
const (
	DefaultOpenCodeContext = 32768
	DefaultOpenCodeOutput  = 8192
)

// Limits is the context and output token limits, with the defaults filled in.
func (o OpenCode) Limits() (context, output int) {
	context, output = o.Context, o.Output
	if context == 0 {
		context = DefaultOpenCodeContext
	}
	if output == 0 {
		output = DefaultOpenCodeOutput
	}
	return context, output
}

// DefaultOpenCodeIdle is OpenCode's idle limit when none is configured: generous,
// since a local model can take minutes over one step.
const DefaultOpenCodeIdle = 10 * time.Minute

// Idle is IdleTimeout as a duration; 0 means no limit.
func (o OpenCode) Idle() time.Duration {
	switch o.IdleTimeout {
	case "":
		return DefaultOpenCodeIdle
	case "off":
		return 0
	}
	d, err := time.ParseDuration(o.IdleTimeout)
	if err != nil || d < time.Minute {
		return DefaultOpenCodeIdle
	}
	return d
}

// DefaultOpenCodeRequest is the per-request provider timeout when none is configured.
const DefaultOpenCodeRequest = 60 * time.Minute

// DefaultOpenCodeChunk is the gap allowed between streamed chunks when none is configured.
const DefaultOpenCodeChunk = 10 * time.Minute

// Request is RequestTimeout as a duration; 0 means no limit ("off").
func (o OpenCode) Request() time.Duration {
	switch o.RequestTimeout {
	case "":
		return DefaultOpenCodeRequest
	case "off":
		return 0
	}
	d, err := time.ParseDuration(o.RequestTimeout)
	if err != nil || d < time.Minute {
		return DefaultOpenCodeRequest
	}
	return d
}

// Chunk is ChunkTimeout as a duration. It is always at least 10 seconds: OpenCode
// has no way to disable the chunk limit.
func (o OpenCode) Chunk() time.Duration {
	if o.ChunkTimeout == "" {
		return DefaultOpenCodeChunk
	}
	d, err := time.ParseDuration(o.ChunkTimeout)
	if err != nil || d < 10*time.Second {
		return DefaultOpenCodeChunk
	}
	return d
}

// Chat holds shepherd chat's own display settings. They are read when the chat starts, so
// a change applies to the next chat, not to one already running.
type Chat struct {
	// Hyperlinks is how links are drawn: one of HyperlinkModes; "" is HyperlinksAuto.
	Hyperlinks string `yaml:"hyperlinks" json:"hyperlinks,omitempty"`
}

// chat.hyperlinks' choices: auto follows the terminal, on and off force it.
const (
	HyperlinksAuto = "auto"
	HyperlinksOn   = "on"
	HyperlinksOff  = "off"
)

// HyperlinkModes are chat.hyperlinks' choices.
var HyperlinkModes = []string{HyperlinksAuto, HyperlinksOn, HyperlinksOff}

// HyperlinksMode is chat.hyperlinks, with its default filled in.
func (c Chat) HyperlinksMode() string {
	if c.Hyperlinks == "" {
		return HyperlinksAuto
	}
	return c.Hyperlinks
}

// Update holds the release check's settings.
type Update struct {
	// Check turns the daily check on or off; unset is on.
	Check *bool `yaml:"check,omitempty" json:"check,omitempty"`
	// Channel is which releases count: beta (every release, pre-releases included) or
	// stable (full releases only); "" is beta, as every release so far is a beta.
	Channel string `yaml:"channel,omitempty" json:"channel,omitempty"`
	// Auto lets the daemon install a newer release by itself and restart onto it when no
	// agent is running; unset is off. It needs the check on, and only replaces a release
	// binary it may write (docs/update-check.md).
	Auto *bool `yaml:"auto,omitempty" json:"auto,omitempty"`
}

// update.channel's choices.
const (
	UpdateBeta   = "beta"
	UpdateStable = "stable"
)

// UpdateChannels are update.channel's choices.
var UpdateChannels = []string{UpdateBeta, UpdateStable}

// Enabled is update.check, with its default (on) filled in.
func (u Update) Enabled() bool { return u.Check == nil || *u.Check }

// AutoUpgrade is update.auto, with its default (off) filled in.
func (u Update) AutoUpgrade() bool { return u.Auto != nil && *u.Auto }

// ChannelName is update.channel, with its default filled in.
func (u Update) ChannelName() string {
	if u.Channel == "" {
		return UpdateBeta
	}
	return u.Channel
}

// DefaultEditorURL is Web.EditorURL when the file does not set it.
const DefaultEditorURL = "vscode://file/{path}"

// Web holds the browser UI's settings.
type Web struct {
	// EditorURL is the template of the "open in editor" links on the lane page: {path} is
	// the absolute path, URL-escaped, and {line} an optional line number. nil (a hand-built
	// Config) is DefaultEditorURL; Default() sets it so the config API shows the value in
	// force; "" hides the links.
	EditorURL *string `yaml:"editor_url,omitempty" json:"editor_url,omitempty"`
}

// EditorTemplate is the template in force: the default when unset, "" when links are off.
func (w Web) EditorTemplate() string {
	if w.EditorURL == nil {
		return DefaultEditorURL
	}
	return *w.EditorURL
}

var editorPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// validate checks the editor template: only {path} and {line}, {path} present, and a
// scheme that opens an application rather than running script or showing content.
func (w Web) validate() []error {
	t := w.EditorTemplate()
	if t == "" {
		return nil
	}
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("web.editor_url: "+format, a...))
	}
	for _, ph := range editorPlaceholder.FindAllString(t, -1) {
		if ph != "{path}" && ph != "{line}" {
			bad("%s is not a placeholder; use {path} or {line}", ph)
		}
	}
	if strings.Count(t, "{") != strings.Count(t, "}") || strings.Count(t, "{") != len(editorPlaceholder.FindAllString(t, -1)) {
		bad("%q has an unmatched brace", t)
	}
	if !strings.Contains(t, "{path}") {
		bad("%q has no {path}", t)
	}
	i := strings.Index(t, ":")
	if i <= 0 {
		bad("%q has no scheme (vscode://file/{path})", t)
		return errs
	}
	scheme := strings.ToLower(t[:i])
	for _, r := range scheme {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			bad("%q has no valid scheme", t)
			return errs
		}
	}
	if slices.Contains([]string{"javascript", "data", "vbscript", "file", "blob", "about"}, scheme) {
		bad("the scheme %q is not allowed", scheme)
	}
	return errs
}

// Desk holds the front desk's settings: the daemon's, and shepherd chat's own.
type Desk struct {
	// Model is the desk's model when the chat is given no --model and none was set
	// with /model; "" leaves it to Claude Code's default.
	Model string `yaml:"model" json:"model,omitempty"`
	// Wake is when the daemon's front desk takes a turn on its own for swarm events
	// (a run ended, a decision, a request needing routing): one of WakeModes; ""
	// is WakeAttached.
	Wake string `yaml:"wake" json:"wake,omitempty"`
	// RotateTokens rotates the daemon's desk session once a turn's context (its input
	// and cache tokens) passes it; nil is DefaultRotateTokens, 0 never rotates on size.
	RotateTokens *int `yaml:"rotate_tokens,omitempty" json:"rotate_tokens,omitempty"`
	// RotateCost rotates it once the session's total cost passes this many dollars;
	// nil never rotates on cost.
	RotateCost *float64 `yaml:"rotate_cost,omitempty" json:"rotate_cost,omitempty"`
	// SummaryModel writes a short notes paragraph into the summary a new session is
	// seeded with; "" leaves it out.
	SummaryModel string `yaml:"summary_model,omitempty" json:"summary_model,omitempty"`
	// SummaryChars caps the summary; 0 is DefaultSummaryChars.
	SummaryChars int `yaml:"summary_chars,omitempty" json:"summary_chars,omitempty"`
	// SummaryTurns is how many of the thread's last turns the summary quotes; nil is
	// DefaultSummaryTurns.
	SummaryTurns *int `yaml:"summary_turns,omitempty" json:"summary_turns,omitempty"`
	// ToolOutputChars caps what each operator tool returns to the daemon's desk; 0 is
	// DefaultToolOutputChars.
	ToolOutputChars int `yaml:"tool_output_chars,omitempty" json:"tool_output_chars,omitempty"`
	// Budget is the desk's own daily line in dollars, against the workspace ceiling and
	// charged to no project; nil gives it no limit of its own, and 0 likewise.
	Budget *float64 `yaml:"budget,omitempty" json:"budget,omitempty"`
	// BudgetCap is soft or hard for that line; "" is hard.
	BudgetCap string `yaml:"budget_cap,omitempty" json:"budget_cap,omitempty"`
}

// The desk's defaults and bounds.
const (
	DefaultRotateTokens    = 150000
	DefaultSummaryChars    = 6000
	DefaultSummaryTurns    = 6
	DefaultToolOutputChars = 4000
	// MinSummaryChars and MinToolOutputChars keep a cap from leaving nothing useful.
	MinSummaryChars    = 1000
	MinToolOutputChars = 500
	// MinRotateTokens keeps a session from rotating on every turn: Claude Code's own
	// prompt, the brief and the tools come to over 20000 tokens before anything is said.
	MinRotateTokens = 50000
	MaxSummaryTurns = 50
)

// RotateAt is desk.rotate_tokens with its default filled in; 0 is off.
func (d Desk) RotateAt() int {
	if d.RotateTokens == nil {
		return DefaultRotateTokens
	}
	return *d.RotateTokens
}

// RotateUSD is desk.rotate_cost; 0 is off.
func (d Desk) RotateUSD() float64 {
	if d.RotateCost == nil {
		return 0
	}
	return *d.RotateCost
}

// SummaryCap is desk.summary_chars with its default filled in.
func (d Desk) SummaryCap() int {
	if d.SummaryChars == 0 {
		return DefaultSummaryChars
	}
	return d.SummaryChars
}

// SummaryTurnCount is desk.summary_turns with its default filled in.
func (d Desk) SummaryTurnCount() int {
	if d.SummaryTurns == nil {
		return DefaultSummaryTurns
	}
	return *d.SummaryTurns
}

// ToolOutputCap is desk.tool_output_chars with its default filled in.
func (d Desk) ToolOutputCap() int {
	if d.ToolOutputChars == 0 {
		return DefaultToolOutputChars
	}
	return d.ToolOutputChars
}

// desk.wake's choices.
const (
	// WakeAttached: only while a client follows the desk's stream, and for a grace
	// period after the last leaves; otherwise events wait as one digest for the next.
	WakeAttached = "attached"
	WakeAlways   = "always"
	WakeNever    = "never"
)

// WakeModes are desk.wake's choices.
var WakeModes = []string{WakeAttached, WakeAlways, WakeNever}

// WakeMode is desk.wake, with its default filled in.
func (d Desk) WakeMode() string {
	if d.Wake == "" {
		return WakeAttached
	}
	return d.Wake
}

// Agents are the agent CLIs Shepherd can start.
var Agents = []string{"claude", "copilot", "cursor", "opencode"}

// LogLevels are daemon.log_level's choices, quietest last.
var LogLevels = []string{"debug", "info", "warn", "error"}

// Branch models.
const (
	// Trunk: work branches off the base branch and lands on it; releases are tags.
	Trunk = "trunk"
	// Promotion: work lands on the first branch and is promoted through the rest
	// (for example dev, then staging, then main).
	Promotion = "promotion"
)

// Tag modes.
const (
	TagsFree     = "free"
	TagsReserved = "reserved"
)

// Who may take an action.
const (
	Human = "human"
	Agent = "agent"
	// Shepherd: Shepherd itself, deterministically (for push: after the gate passes).
	Shepherd = "shepherd"
)

// Claude Code permission modes for the agents Shepherd starts.
const (
	// PermAuto runs as the person's own sessions do: their Claude Code auto mode and
	// settings decide what needs no prompt.
	PermAuto = "auto"
	// PermAcceptEdits and PermDefault allow edits (or not) and Shepherd's own short list
	// of commands; anything else is refused, since nobody can approve it headless.
	PermAcceptEdits = "acceptEdits"
	PermDefault     = "default"
	// PermBypass skips permission checks altogether.
	PermBypass = "bypassPermissions"
)

// PermissionModes are the values agent.permission_mode takes.
var PermissionModes = []string{PermAuto, PermAcceptEdits, PermDefault, PermBypass}

// Profile says how a repo works. In Repos, an empty field inherits from Defaults.
type Profile struct {
	BaseBranch  string   `yaml:"base_branch,omitempty" json:"base_branch,omitempty"`
	BranchModel string   `yaml:"branch_model,omitempty" json:"branch_model,omitempty"`
	Promotion   []string `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	// Gate is the command that must pass in a worktree before work counts as green.
	Gate string `yaml:"gate,omitempty" json:"gate,omitempty"`
	// SharedPaths are globs that more than one lane may want; touching one takes a lease.
	SharedPaths []string `yaml:"shared_paths,omitempty" json:"shared_paths,omitempty"`
	// WorktreeRoot is where lane worktrees go. Empty means <workspace>/<repo>-worktrees.
	WorktreeRoot string `yaml:"worktree_root,omitempty" json:"worktree_root,omitempty"`
	// Setup runs in each new lane's worktree, with SHEPHERD_REPO set to the repo's main
	// checkout: what a fresh worktree needs before its gate can run (an .env, vendor/).
	Setup string `yaml:"setup,omitempty" json:"setup,omitempty"`
	// CoordFile is a coordination file (AGENTS-COORD.md) Shepherd keeps a generated view
	// of its lanes in, between markers, during a cutover; "" for none.
	CoordFile string `yaml:"coord_file,omitempty" json:"coord_file,omitempty"`
	// TagPrefix comes before a release version in the repo's tags; "v" by default.
	TagPrefix string `yaml:"tag_prefix,omitempty" json:"tag_prefix,omitempty"`
	// Tags is "free" (the default) or "reserved": every release tag pushed from the repo
	// must be reserved first (shepherd tag reserve), and contain its lane's merge.
	Tags string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// MaxRuns is how many agents may run at once in the repo; 0 leaves only the
	// machine's daemon.max_runs.
	MaxRuns int `yaml:"max_runs,omitempty" json:"max_runs,omitempty"`
	// Brief is added to every agent's brief in the repo: its own rules.
	Brief string `yaml:"brief,omitempty" json:"brief,omitempty"`
	// Forbid are regular expressions commit messages must not match before Shepherd
	// pushes ("(?i)co-authored-by"); a match goes back to the agent to amend.
	Forbid []string `yaml:"forbid,omitempty" json:"forbid,omitempty"`
	// Standards names the Shepherd standards packs (~/.shepherd/standards/<pack>/) every
	// agent in the repo is told about. See docs/standards-packs.md.
	Standards []string `yaml:"standards,omitempty" json:"standards,omitempty"`
	// StandardsDocs are repo paths holding the repo's own standards. They win over a
	// Shepherd pack where they disagree; only those that exist are named to the agent.
	StandardsDocs []string `yaml:"standards_docs,omitempty" json:"standards_docs,omitempty"`
	// StandardsPackDir is the repo's own pack directory, relative to the repo. Its rules
	// replace a Shepherd rule of the same id, and `disabled: true` switches one off.
	StandardsPackDir string `yaml:"standards_pack_dir,omitempty" json:"standards_pack_dir,omitempty"`
	// StandardsWhen are extra `when` tags active in every lane of the repo.
	StandardsWhen []string `yaml:"standards_when,omitempty" json:"standards_when,omitempty"`
	Autonomy      Autonomy `yaml:"autonomy,omitempty" json:"autonomy"`
	// Agent says how the agents Shepherd starts in the repo run.
	Agent AgentOpts `yaml:"agent,omitempty" json:"agent"`
	// Follows are the repos this one depends on: a release of one opens a lane here and
	// hands it to an agent (a framework's tag, the host's version bump).
	Follows []Follow `yaml:"follows,omitempty" json:"follows,omitempty"`
	// Watch says what Shepherd's forge watcher hands back to a lane's agent unasked.
	Watch Watch `yaml:"watch,omitempty" json:"watch"`
	// Artifacts says which files a lane's agents wrote that the person may want to read.
	Artifacts Artifacts `yaml:"artifacts,omitempty" json:"artifacts"`
}

// Artifacts is the artifact discovery setting of a repo.
type Artifacts struct {
	// Discover are the globs (repo-relative, as a lane's scope) that pick a lane's
	// artifacts from its changed files. Unset is DefaultArtifactGlobs; a repo's list
	// replaces them.
	Discover []string `yaml:"discover,omitempty" json:"discover,omitempty"`
	// KeepDays is how long the snapshots of a closed lane's artifacts are kept after the
	// close, pinned ones excepted. 0 is unset, which is DefaultArtifactKeepDays.
	KeepDays int `yaml:"keep_days,omitempty" json:"keep_days,omitempty"`
	// CapMB is the total size of every snapshot, in megabytes; past it the oldest
	// unpinned ones of closed lanes go first. It is a machine total, so the value that
	// counts is the defaults' (see Config.ArtifactRetention). 0 is DefaultArtifactCapMB.
	CapMB int `yaml:"cap_mb,omitempty" json:"cap_mb,omitempty"`
}

// The retention defaults for artifact snapshots (docs/design/artifacts.md §4).
const (
	DefaultArtifactKeepDays = 30
	DefaultArtifactCapMB    = 500
)

// ArtifactRetention is how long the daemon keeps a closed lane's snapshots and the most
// it keeps in total. The cap is the defaults' setting. The days are the longest any
// profile asks for, so no repo's snapshots expire sooner than its own setting says.
func (c Config) ArtifactRetention() (keepDays int, capBytes int64) {
	keepDays = c.Defaults.Artifacts.KeepDays
	for _, p := range c.Repos {
		if p.Artifacts.KeepDays > keepDays {
			keepDays = p.Artifacts.KeepDays
		}
	}
	if keepDays <= 0 {
		keepDays = DefaultArtifactKeepDays
	}
	mb := c.Defaults.Artifacts.CapMB
	if mb <= 0 {
		mb = DefaultArtifactCapMB
	}
	return keepDays, int64(mb) << 20
}

// DefaultArtifactGlobs are the discovery globs when a repo sets none: markdown and
// images anywhere, and the directory Shepherd keeps files not meant for the repo in.
var DefaultArtifactGlobs = []string{"**/*.md", "**/*.markdown", "**/*.png", "**/*.jpg",
	"**/*.gif", "**/*.webp", "**/*.svg", ".shepherd/artifacts/**"}

// Globs is the effective discovery list.
func (a Artifacts) Globs() []string {
	if len(a.Discover) == 0 {
		return DefaultArtifactGlobs
	}
	return a.Discover
}

// DefaultFixTries is how many times Shepherd hands a merge request's failures (a failed
// pipeline, unresolved review threads) back to the lane's agent before leaving it to
// the person.
const DefaultFixTries = 2

// Watch is what the forge watcher sends back to the lane's agent on its own. The
// require-approval job is never handed back: it needs a human reviewer. Set under
// defaults for every repo, or under a repo to override.
type Watch struct {
	// FixTries is the tries per merge request; 0 turns handing back off. Unset is
	// DefaultFixTries.
	FixTries *int `yaml:"fix_tries,omitempty" json:"fix_tries,omitempty"`
	// ReviewThreads hands unresolved review threads on the merge request back to the
	// agent. Unset is on.
	ReviewThreads *bool `yaml:"review_threads,omitempty" json:"review_threads,omitempty"`
}

// Tries is the effective try cap.
func (w Watch) Tries() int {
	if w.FixTries == nil {
		return DefaultFixTries
	}
	return *w.FixTries
}

// Threads says whether review threads go back to the agent.
func (w Watch) Threads() bool { return w.ReviewThreads == nil || *w.ReviewThreads }

// When a follow acts on a release.
const (
	// Published: the release tag's pipeline passed, so what it publishes is there.
	Published = "published"
	// Tagged: the tag alone, for a repo with no pipeline on its tags.
	Tagged = "tagged"
)

// Follow is one repo a repo follows, and what to do here when it releases. Lane and
// Task may use {repo}, {tag}, {version}, {major}, {minor}, {patch} and {previous}.
type Follow struct {
	// Repo is the followed repo's name in the workspace.
	Repo string `yaml:"repo" json:"repo"`
	// MinBump is the smallest release worth a lane: patch, minor (the default) or major.
	MinBump string `yaml:"min_bump,omitempty" json:"min_bump,omitempty"`
	// After is published (the default) or tagged.
	After string `yaml:"after,omitempty" json:"after,omitempty"`
	// Lane names the lane; "chore/{repo}-{version}" by default.
	Lane  string   `yaml:"lane,omitempty" json:"lane,omitempty"`
	Scope []string `yaml:"scope" json:"scope"`
	// Agent does the work: claude (the default), copilot, cursor or opencode.
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Task is what the agent is asked to do.
	Task string `yaml:"task" json:"task"`
}

// Effective fills a follow's defaults.
func (f Follow) Effective() Follow {
	if f.MinBump == "" {
		f.MinBump = "minor"
	}
	if f.After == "" {
		f.After = Published
	}
	if f.Lane == "" {
		f.Lane = "chore/{repo}-{version}"
	}
	if f.Agent == "" {
		f.Agent = "claude"
	}
	return f
}

// AgentOpts says how the agents Shepherd starts run.
type AgentOpts struct {
	// PermissionMode is the Claude Code permission mode of a run: auto (the default),
	// acceptEdits, default or bypassPermissions. Copilot takes auto and bypassPermissions
	// as allowing every tool; Cursor always runs with --force.
	PermissionMode string `yaml:"permission_mode,omitempty" json:"permission_mode,omitempty"`
	// Model is the model each agent runs on when lane run names none, by agent
	// (claude, copilot, cursor, opencode); an agent left out uses its CLI's own default. A repo's
	// entries replace the defaults' for the same agent.
	Model map[string]string `yaml:"model,omitempty" json:"model,omitempty"`
	// Effort is the effort each agent runs at when lane run names none, by agent, in
	// that agent's own words. An agent left out uses models.<agent>.effort, else its
	// CLI's own default. A repo's entries replace the defaults' for the same agent.
	Effort map[string]string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// Models narrows the models the repo's agents may run (see ModelRules). The
	// defaults' rules and the repo's both apply.
	Models ModelRules `yaml:"models,omitempty" json:"models,omitempty"`
}

// badModel says a model name is not one: blank around it, or with spaces inside.
func badModel(m string) bool {
	return m != strings.TrimSpace(m) || strings.ContainsAny(m, " \t\n")
}

// Autonomy records what agents may do unasked in a repo.
type Autonomy struct {
	// Merge: "agent" lets an agent arm merge-when-pipeline-succeeds on its own lane's
	// merge request; the forge's approvals, pipelines and threads still gate it.
	Merge  string `yaml:"merge,omitempty" json:"merge,omitempty"`
	Tag    string `yaml:"tag,omitempty" json:"tag,omitempty"`
	Deploy string `yaml:"deploy,omitempty" json:"deploy,omitempty"`
	// Push: who pushes a lane's branch and opens its merge request. "shepherd" lets
	// Shepherd do it after an agent's run, once the repo's gate passes in the lane;
	// "agent" lets the agent push its own lane's branch during its run.
	Push string `yaml:"push,omitempty" json:"push,omitempty"`
	// PlanFirst means an agent proposes a plan and waits before changing anything.
	PlanFirst *bool `yaml:"plan_first,omitempty" json:"plan_first,omitempty"`
}

// Default is the configuration with no file.
func Default() Config {
	yes := true
	editor := DefaultEditorURL
	return Config{
		Web:    Web{EditorURL: &editor},
		Daemon: Daemon{Listen: "127.0.0.1:0", MaxRuns: 4, Poll: "60s", Budget: ptr(20.0), CreditUSD: ptr(0.04), LogLevel: "info"},
		Defaults: Profile{
			BaseBranch:       "main",
			TagPrefix:        "v",
			Tags:             TagsFree,
			BranchModel:      Trunk,
			Autonomy:         Autonomy{Merge: Human, Tag: Human, Deploy: Human, Push: Human, PlanFirst: &yes},
			Agent:            AgentOpts{PermissionMode: PermAuto},
			StandardsDocs:    []string{"docs/standards", "docs/architecture", "AGENTS.md", "CLAUDE.md"},
			StandardsPackDir: ".shepherd/standards",
		},
	}
}

func ptr(f float64) *float64 { return &f }

// Load reads path over the defaults. A missing file gives Default().
func Load(path string) (Config, error) {
	c, _, err := LoadWarn(path)
	return c, err
}

// LoadWarn is Load, and also returns a warning for each retired key the file still
// has (see RetiredKeys). The keys are ignored; the file loads.
func LoadWarn(path string) (Config, []string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil, nil
	}
	if err != nil {
		return Config{}, nil, err
	}
	c, warns, err := ParseWarn(b)
	if err != nil {
		return Config{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, warns, nil
}

// Parse reads YAML over the defaults and validates the result. Unknown keys are errors,
// so a typo cannot silently fall back to a default; a retired key (RetiredKeys) is
// dropped, with a warning from ParseWarn.
func Parse(b []byte) (Config, error) {
	c, _, err := ParseWarn(b)
	return c, err
}

// ParseWarn is Parse, and also returns a warning for each retired key found.
func ParseWarn(b []byte) (Config, []string, error) {
	b, warns, err := stripRetired(b)
	if err != nil {
		return Config{}, nil, err
	}
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var file Config
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, nil, err
	}
	if file.Daemon.Listen != "" {
		c.Daemon.Listen = file.Daemon.Listen
	}
	if file.Daemon.MaxRuns != 0 {
		c.Daemon.MaxRuns = file.Daemon.MaxRuns
	}
	if file.Daemon.Poll != "" {
		c.Daemon.Poll = file.Daemon.Poll
	}
	if file.Daemon.Budget != nil {
		c.Daemon.Budget = file.Daemon.Budget
	}
	c.Daemon.BudgetCap = file.Daemon.BudgetCap
	if file.Daemon.CreditUSD != nil {
		c.Daemon.CreditUSD = file.Daemon.CreditUSD
	}
	if file.Daemon.LogLevel != "" {
		c.Daemon.LogLevel = file.Daemon.LogLevel
	}
	c.Desk = file.Desk
	c.Chat = file.Chat
	c.Update = file.Update
	c.KeepGoing = file.KeepGoing
	c.Next = file.Next
	if file.Web.EditorURL != nil {
		c.Web = file.Web
	}
	c.OpenCode = file.OpenCode
	c.Defaults = merge(c.Defaults, file.Defaults)
	c.Repos = file.Repos
	c.Projects = file.Projects
	c.Models = file.Models
	if err := c.Validate(); err != nil {
		return c, warns, err
	}
	return c, append(warns, c.ModelWarnings()...), nil
}

// Validate checks every value against its closed set.
func (c Config) Validate() error {
	var errs []error
	host, _, err := net.SplitHostPort(c.Daemon.Listen)
	if err != nil {
		errs = append(errs, fmt.Errorf("daemon.listen: %w", err))
	} else if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		// Authentication is a local token today; logins come with the hosted service.
		errs = append(errs, fmt.Errorf("daemon.listen: %q is not a loopback address", c.Daemon.Listen))
	}
	if c.Daemon.Poll != "off" {
		if d, err := time.ParseDuration(c.Daemon.Poll); err != nil || d < 10*time.Second {
			errs = append(errs, fmt.Errorf("daemon.poll: %q; a duration of at least 10s, or off", c.Daemon.Poll))
		}
	}
	if *c.Daemon.Budget < 0 || *c.Daemon.CreditUSD < 0 {
		errs = append(errs, fmt.Errorf("daemon.budget and daemon.credit_usd cannot be negative"))
	}
	if !slices.Contains(LogLevels, c.Daemon.LogLevel) {
		errs = append(errs, fmt.Errorf("daemon.log_level: %q is not %s", c.Daemon.LogLevel, strings.Join(LogLevels, ", ")))
	}
	if c.Desk.Wake != "" && !slices.Contains(WakeModes, c.Desk.Wake) {
		errs = append(errs, fmt.Errorf("desk.wake: %q is not %s", c.Desk.Wake, strings.Join(WakeModes, ", ")))
	}
	if !slices.Contains(HyperlinkModes, c.Chat.HyperlinksMode()) {
		errs = append(errs, fmt.Errorf("chat.hyperlinks: %q is not %s", c.Chat.Hyperlinks, strings.Join(HyperlinkModes, ", ")))
	}
	if !slices.Contains(UpdateChannels, c.Update.ChannelName()) {
		errs = append(errs, fmt.Errorf("update.channel: %q is not %s", c.Update.Channel, strings.Join(UpdateChannels, " or ")))
	}
	if badModel(c.Desk.Model) {
		errs = append(errs, fmt.Errorf("desk.model: %q is not a model name", c.Desk.Model))
	}
	errs = append(errs, c.Desk.validate()...)
	errs = append(errs, c.KeepGoing.validate()...)
	errs = append(errs, c.Next.validate()...)
	errs = append(errs, c.Web.validate()...)
	if !slices.Contains(Caps, c.Daemon.BudgetCapMode()) {
		errs = append(errs, fmt.Errorf("daemon.budget_cap: %q is not %s", c.Daemon.BudgetCap, strings.Join(Caps, " or ")))
	}
	if b := c.OpenCode.Bin; b != strings.TrimSpace(b) {
		errs = append(errs, fmt.Errorf("opencode.bin: %q has blanks around it", b))
	}
	if t := c.OpenCode.IdleTimeout; t != "" && t != "off" {
		if d, err := time.ParseDuration(t); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("opencode.idle_timeout: %q; a duration of at least 1m, or off", t))
		}
	}
	if t := c.OpenCode.RequestTimeout; t != "" && t != "off" {
		if d, err := time.ParseDuration(t); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("opencode.request_timeout: %q; a duration of at least 1m, or off", t))
		}
	}
	if t := c.OpenCode.ChunkTimeout; t != "" {
		if d, err := time.ParseDuration(t); err != nil || d < 10*time.Second {
			errs = append(errs, fmt.Errorf("opencode.chunk_timeout: %q; a duration of at least 10s", t))
		}
	}
	if c.OpenCode.Context < 0 {
		errs = append(errs, fmt.Errorf("opencode.context: %d is not a positive number of tokens", c.OpenCode.Context))
	}
	if c.OpenCode.Output < 0 {
		errs = append(errs, fmt.Errorf("opencode.output: %d is not a positive number of tokens", c.OpenCode.Output))
	}
	if c.OpenCode.Context >= 0 && c.OpenCode.Output >= 0 {
		if ctx, out := c.OpenCode.Limits(); out >= ctx {
			errs = append(errs, fmt.Errorf("opencode.output: %d must be less than the context, %d", out, ctx))
		}
	}
	if e := c.OpenCode.Endpoint; e != "" {
		if u, err := url.Parse(e); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			errs = append(errs, fmt.Errorf("opencode.endpoint: %q is not an http or https URL without credentials", e))
		}
	}
	if c.Daemon.MaxRuns < 1 {
		errs = append(errs, fmt.Errorf("daemon.max_runs: %d; at least 1", c.Daemon.MaxRuns))
	}
	errs = append(errs, c.Defaults.validate("defaults")...)
	for name := range c.Repos {
		errs = append(errs, c.Profile(name).validate("repos."+name)...)
	}
	errs = append(errs, c.validateProjects()...)
	errs = append(errs, c.validateModels()...)
	return errors.Join(errs...)
}

func (d Desk) validate() []error {
	var errs []error
	if n := d.RotateAt(); n != 0 && n < MinRotateTokens {
		errs = append(errs, fmt.Errorf("desk.rotate_tokens: %d; at least %d, or 0 to never rotate on size", n, MinRotateTokens))
	}
	if d.RotateCost != nil && *d.RotateCost <= 0 {
		errs = append(errs, fmt.Errorf("desk.rotate_cost: %v; dollars above 0, or leave it out to never rotate on cost", *d.RotateCost))
	}
	if d.SummaryModel != "" && badModel(d.SummaryModel) {
		errs = append(errs, fmt.Errorf("desk.summary_model: %q is not a model name", d.SummaryModel))
	}
	if n := d.SummaryChars; n != 0 && n < MinSummaryChars {
		errs = append(errs, fmt.Errorf("desk.summary_chars: %d; at least %d", n, MinSummaryChars))
	}
	if n := d.SummaryTurnCount(); n < 0 || n > MaxSummaryTurns {
		errs = append(errs, fmt.Errorf("desk.summary_turns: %d; 0 to %d", n, MaxSummaryTurns))
	}
	if d.Budget != nil && *d.Budget < 0 {
		errs = append(errs, fmt.Errorf("desk.budget: %v cannot be negative", *d.Budget))
	}
	if !slices.Contains(Caps, d.BudgetCapMode()) {
		errs = append(errs, fmt.Errorf("desk.budget_cap: %q is not %s", d.BudgetCap, strings.Join(Caps, " or ")))
	}
	if n := d.ToolOutputChars; n != 0 && n < MinToolOutputChars {
		errs = append(errs, fmt.Errorf("desk.tool_output_chars: %d; at least %d", n, MinToolOutputChars))
	}
	return errs
}

// Profile returns the effective profile for a repo: its entry in Repos over Defaults.
func (c Config) Profile(repo string) Profile {
	return merge(c.Defaults, c.Repos[repo])
}

func (p Profile) validate(at string) []error {
	var errs []error
	if p.BaseBranch == "" {
		errs = append(errs, fmt.Errorf("%s.base_branch: empty", at))
	}
	switch p.BranchModel {
	case Trunk:
	case Promotion:
		if len(p.Promotion) < 2 {
			errs = append(errs, fmt.Errorf("%s.promotion: the promotion model needs at least two branches", at))
		}
	default:
		errs = append(errs, fmt.Errorf("%s.branch_model: %q is not %s or %s", at, p.BranchModel, Trunk, Promotion))
	}
	for field, v := range map[string]string{"merge": p.Autonomy.Merge, "tag": p.Autonomy.Tag, "deploy": p.Autonomy.Deploy} {
		if v != Human && v != Agent {
			errs = append(errs, fmt.Errorf("%s.autonomy.%s: %q is not %s or %s", at, field, v, Human, Agent))
		}
	}
	for _, f := range p.Forbid {
		if _, err := regexp.Compile(f); err != nil {
			errs = append(errs, fmt.Errorf("%s.forbid: %q: %v", at, f, err))
		}
	}
	for _, g := range p.SharedPaths {
		if strings.TrimSpace(g) == "" {
			errs = append(errs, fmt.Errorf("%s.shared_paths: empty glob", at))
		}
	}
	if p.Tags != TagsFree && p.Tags != TagsReserved {
		errs = append(errs, fmt.Errorf("%s.tags: %q is not %s or %s", at, p.Tags, TagsFree, TagsReserved))
	}
	for agent, m := range p.Agent.Model {
		if !slices.Contains(Agents, agent) {
			errs = append(errs, fmt.Errorf("%s.agent.model: %q is not claude, copilot, cursor or opencode", at, agent))
		}
		if m == "" || badModel(m) {
			errs = append(errs, fmt.Errorf("%s.agent.model.%s: %q is not a model name", at, agent, m))
		}
	}
	if !slices.Contains([]string{Human, Shepherd, Agent}, p.Autonomy.Push) {
		errs = append(errs, fmt.Errorf("%s.autonomy.push: %q is not %s, %s or %s", at, p.Autonomy.Push, Human, Shepherd, Agent))
	}
	if !slices.Contains(PermissionModes, p.Agent.PermissionMode) {
		errs = append(errs, fmt.Errorf("%s.agent.permission_mode: %q is not %s", at, p.Agent.PermissionMode, strings.Join(PermissionModes, ", ")))
	}
	if p.MaxRuns < 0 {
		errs = append(errs, fmt.Errorf("%s.max_runs: %d cannot be negative; 0 leaves only daemon.max_runs", at, p.MaxRuns))
	}
	if p.Watch.FixTries != nil && *p.Watch.FixTries < 0 {
		errs = append(errs, fmt.Errorf("%s.watch.fix_tries: %d is negative; 0 turns handing back off", at, *p.Watch.FixTries))
	}
	if p.Autonomy.Push == Shepherd && strings.TrimSpace(p.Gate) == "" {
		errs = append(errs, fmt.Errorf("%s.autonomy.push: shepherd pushes only after the gate passes, and no gate is set", at))
	}
	for i, g := range p.Artifacts.Discover {
		if bad := badRepoPath(g); bad != "" {
			errs = append(errs, fmt.Errorf("%s.artifacts.discover[%d]: %q %s", at, i, g, bad))
		}
	}
	if p.Artifacts.KeepDays < 0 {
		errs = append(errs, fmt.Errorf("%s.artifacts.keep_days: %d is negative; 0 takes the default of %d", at, p.Artifacts.KeepDays, DefaultArtifactKeepDays))
	}
	if p.Artifacts.CapMB < 0 {
		errs = append(errs, fmt.Errorf("%s.artifacts.cap_mb: %d is negative; 0 takes the default of %d", at, p.Artifacts.CapMB, DefaultArtifactCapMB))
	}
	for i, n := range p.Standards {
		if n == "" || strings.ContainsAny(n, `/\`) || n == "." || n == ".." {
			errs = append(errs, fmt.Errorf("%s.standards[%d]: %q is not a pack name (a folder under ~/.shepherd/standards)", at, i, n))
		}
	}
	for i, d := range p.StandardsDocs {
		if bad := badRepoPath(d); bad != "" {
			errs = append(errs, fmt.Errorf("%s.standards_docs[%d]: %q %s", at, i, d, bad))
		}
	}
	if d := p.StandardsPackDir; d != "" {
		if bad := badRepoPath(d); bad != "" {
			errs = append(errs, fmt.Errorf("%s.standards_pack_dir: %q %s", at, d, bad))
		}
	}
	for i, f := range p.Follows {
		f, at := f.Effective(), fmt.Sprintf("%s.follows[%d]", at, i)
		if strings.TrimSpace(f.Repo) == "" {
			errs = append(errs, fmt.Errorf("%s.repo: empty", at))
		}
		if len(f.Scope) == 0 {
			errs = append(errs, fmt.Errorf("%s.scope: a follow's lane needs a scope", at))
		}
		if strings.TrimSpace(f.Task) == "" {
			errs = append(errs, fmt.Errorf("%s.task: empty", at))
		}
		if !slices.Contains([]string{"patch", "minor", "major"}, f.MinBump) {
			errs = append(errs, fmt.Errorf("%s.min_bump: %q is not patch, minor or major", at, f.MinBump))
		}
		if f.After != Published && f.After != Tagged {
			errs = append(errs, fmt.Errorf("%s.after: %q is not %s or %s", at, f.After, Published, Tagged))
		}
		if !slices.Contains(Agents, f.Agent) {
			errs = append(errs, fmt.Errorf("%s.agent: %q is not claude, copilot, cursor or opencode", at, f.Agent))
		}
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errs
}

// badRepoPath says why p is not a path inside a repo, or "" if it is.
func badRepoPath(p string) string {
	switch {
	case strings.TrimSpace(p) == "":
		return "is empty"
	case filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`):
		return "must be relative to the repo"
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "must stay inside the repo"
		}
	}
	return ""
}

// merge returns base with every set field of over applied.
func merge(base, over Profile) Profile {
	out := base
	if over.BaseBranch != "" {
		out.BaseBranch = over.BaseBranch
	}
	if over.BranchModel != "" {
		out.BranchModel = over.BranchModel
	}
	if over.Promotion != nil {
		out.Promotion = over.Promotion
	}
	if over.Gate != "" {
		out.Gate = over.Gate
	}
	if over.SharedPaths != nil {
		out.SharedPaths = over.SharedPaths
	}
	if over.WorktreeRoot != "" {
		out.WorktreeRoot = over.WorktreeRoot
	}
	if over.Setup != "" {
		out.Setup = over.Setup
	}
	if over.CoordFile != "" {
		out.CoordFile = over.CoordFile
	}
	if over.Tags != "" {
		out.Tags = over.Tags
	}
	if over.TagPrefix != "" {
		out.TagPrefix = over.TagPrefix
	}
	if over.Brief != "" {
		out.Brief = over.Brief
	}
	if over.Forbid != nil {
		out.Forbid = over.Forbid
	}
	if over.Standards != nil {
		out.Standards = over.Standards
	}
	if over.StandardsDocs != nil {
		out.StandardsDocs = over.StandardsDocs
	}
	if over.StandardsPackDir != "" {
		out.StandardsPackDir = over.StandardsPackDir
	}
	if over.StandardsWhen != nil {
		out.StandardsWhen = over.StandardsWhen
	}
	if over.Artifacts.Discover != nil {
		out.Artifacts.Discover = over.Artifacts.Discover
	}
	if over.Artifacts.KeepDays != 0 {
		out.Artifacts.KeepDays = over.Artifacts.KeepDays
	}
	if over.Artifacts.CapMB != 0 {
		out.Artifacts.CapMB = over.Artifacts.CapMB
	}
	if over.Autonomy.Merge != "" {
		out.Autonomy.Merge = over.Autonomy.Merge
	}
	if over.Autonomy.Tag != "" {
		out.Autonomy.Tag = over.Autonomy.Tag
	}
	if over.Autonomy.Deploy != "" {
		out.Autonomy.Deploy = over.Autonomy.Deploy
	}
	if over.Autonomy.Push != "" {
		out.Autonomy.Push = over.Autonomy.Push
	}
	if over.Autonomy.PlanFirst != nil {
		out.Autonomy.PlanFirst = over.Autonomy.PlanFirst
	}
	if over.Watch.FixTries != nil {
		out.Watch.FixTries = over.Watch.FixTries
	}
	if over.Watch.ReviewThreads != nil {
		out.Watch.ReviewThreads = over.Watch.ReviewThreads
	}
	if over.MaxRuns != 0 {
		out.MaxRuns = over.MaxRuns
	}
	if over.Agent.PermissionMode != "" {
		out.Agent.PermissionMode = over.Agent.PermissionMode
	}
	if len(over.Agent.Model) > 0 {
		m := maps.Clone(base.Agent.Model)
		if m == nil {
			m = map[string]string{}
		}
		maps.Copy(m, over.Agent.Model)
		out.Agent.Model = m
	}
	if len(over.Agent.Effort) > 0 {
		m := maps.Clone(base.Agent.Effort)
		if m == nil {
			m = map[string]string{}
		}
		maps.Copy(m, over.Agent.Effort)
		out.Agent.Effort = m
	}
	out.Agent.Models = mergeRules(base.Agent.Models, over.Agent.Models)
	if over.Follows != nil {
		out.Follows = over.Follows
	}
	return out
}

//go:embed template.yaml
var template []byte

// Template is the commented config file the daemon writes on first start.
func Template() []byte { return template }

// WriteTemplate writes Template to path if nothing is there yet, and reports whether it
// did. An existing file is never touched.
func WriteTemplate(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(template); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
