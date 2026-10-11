package daemon

import (
	"reflect"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

// The settings form is driven by a description served with the config (api.ConfigDoc).
// Its fields come from the yaml tags of internal/config's structs by reflection, so a
// field added to a profile or the daemon section shows up in the form as a plain input
// with no change here. fieldMeta adds what the structs do not say: a label, help, the
// closed set of choices, and whether raising the value gives agents more power.

type fieldMeta struct {
	label, help, group string
	choices            []string
	kind               string // overrides the kind reflection found
	risk, restart      bool
}

var (
	whoChoices  = []string{config.Human, config.Agent}
	pushChoices = []string{config.Human, config.Shepherd, config.Agent}
)

// profileMeta, by field path inside a profile.
var profileMeta = map[string]fieldMeta{
	"base_branch":           {label: "Base branch", help: "The branch work branches off and lands on (the first of the promotion branches under the promotion model).", group: "Branches"},
	"branch_model":          {label: "Branch model", help: "trunk: work lands on the base branch. promotion: work lands on the first branch and is promoted through the rest.", group: "Branches", choices: []string{config.Trunk, config.Promotion}},
	"promotion":             {label: "Promotion branches", help: "In order, for the promotion model; at least two.", group: "Branches"},
	"gate":                  {label: "Gate", help: "The command that must pass in a lane's worktree before work counts as green.", group: "Checks", risk: true},
	"setup":                 {label: "Setup", help: "Runs in each new lane's worktree before its gate can: an .env, vendor/.", group: "Checks", kind: api.KindText},
	"shared_paths":          {label: "Shared paths", help: "Globs more than one lane may want; touching one takes a lease.", group: "Checks"},
	"forbid":                {label: "Forbidden commit patterns", help: "Regular expressions a commit message must not match before Shepherd pushes.", group: "Checks", risk: true},
	"worktree_root":         {label: "Worktree root", help: "Where lane worktrees go; empty is <workspace>/<repo>-worktrees.", group: "Checks"},
	"coord_file":            {label: "Coordination file", help: "A coordination file Shepherd keeps a generated lane view in during a cutover.", group: "Checks"},
	"tag_prefix":            {label: "Tag prefix", help: "Comes before a release version in the repo's tags.", group: "Releases"},
	"tags":                  {label: "Tags", help: "free, or reserved: every release tag must be reserved first and contain its lane's merge.", group: "Releases", choices: []string{config.TagsFree, config.TagsReserved}},
	"brief":                 {label: "Brief", help: "Added to every agent's brief in the repo: its own rules.", group: "Agents", kind: api.KindText},
	"standards":             {label: "Standards packs", help: "Packs under ~/.shepherd/standards every agent in the repo is told about.", group: "Standards"},
	"standards_docs":        {label: "Standards docs", help: "Repo paths holding the repo's own standards; they win over a pack where they disagree.", group: "Standards"},
	"standards_pack_dir":    {label: "Standards pack directory", help: "The repo's own pack directory, relative to the repo.", group: "Standards"},
	"standards_when":        {label: "Standards tags", help: "Extra `when` tags active in every lane of the repo.", group: "Standards"},
	"autonomy.push":         {label: "Push", help: "Who pushes a lane's branch and opens its merge request: human, shepherd (after the gate passes) or agent.", group: "Autonomy", choices: pushChoices, risk: true},
	"autonomy.merge":        {label: "Merge", help: "agent lets an agent arm merge-when-pipeline-succeeds on its own lane's merge request; the forge still gates it.", group: "Autonomy", choices: whoChoices, risk: true},
	"autonomy.tag":          {label: "Tag", help: "Who may cut a release tag.", group: "Autonomy", choices: whoChoices, risk: true},
	"autonomy.deploy":       {label: "Deploy", help: "Who may deploy.", group: "Autonomy", choices: whoChoices, risk: true},
	"autonomy.plan_first":   {label: "Plan first", help: "An agent proposes a plan and waits before changing anything.", group: "Autonomy", risk: true},
	"agent.permission_mode": {label: "Permission mode", help: "auto: the person's own Claude Code settings decide. acceptEdits and default allow a short list of commands. bypassPermissions skips every check.", group: "Agents", choices: config.PermissionModes, risk: true},
	"agent.model":           {label: "Model per agent", help: "By agent: claude, copilot, cursor, opencode. An agent left out uses its CLI's own default.", group: "Agents"},
	"follows":               {label: "Follows", help: "Repos this one depends on: a release of one opens a lane here.", group: "Releases"},
}

var daemonMeta = map[string]fieldMeta{
	"listen":     {label: "Listen", help: "A loopback host:port; port 0 picks a free one.", restart: true},
	"max_runs":   {label: "Max runs", help: "How many agents may run at once on this machine."},
	"poll":       {label: "Poll", help: "How often open lanes' merge requests are checked: 60s, 2m, or off.", restart: true},
	"budget":     {label: "Daily budget (USD)", help: "Past it Shepherd holds the runs it would start on its own; 0 is no cap. It warns at 80%."},
	"budget_cap": {label: "Budget cap", help: "soft only warns; hard also holds the runs Shepherd starts itself.", choices: config.Caps},
	"credit_usd": {label: "Copilot credit (USD)", help: "What one Copilot credit costs."},
	"log_level":  {label: "Log level", choices: config.LogLevels},
}

var deskMeta = map[string]fieldMeta{
	"model":             {label: "Model", help: "The front desk's model; empty leaves it to Claude Code's default."},
	"wake":              {label: "Wake", help: "attached: only while a client follows the desk. always, or never.", choices: config.WakeModes},
	"rotate_tokens":     {label: "Rotate at tokens", help: "Start a new desk session past this context size; 0 never rotates on size."},
	"rotate_cost":       {label: "Rotate at cost (USD)", help: "Start a new session past this session cost; unset never rotates on cost."},
	"summary_model":     {label: "Summary model", help: "Writes the notes paragraph of the summary a new session is seeded with."},
	"summary_chars":     {label: "Summary size (chars)"},
	"summary_turns":     {label: "Summary turns"},
	"tool_output_chars": {label: "Tool output cap (chars)", help: "What each operator tool returns to the daemon's desk."},
	"budget":            {label: "Daily budget (USD)", help: "The desk's own line, against the workspace ceiling; unset or 0 is no limit of its own."},
	"budget_cap":        {label: "Budget cap", choices: config.Caps},
}

var chatMeta = map[string]fieldMeta{
	"hyperlinks": {label: "Hyperlinks", help: "auto draws clickable links where the terminal supports them; on or off forces it. Applies to the next shepherd chat.", choices: config.HyperlinkModes},
}

var openCodeMeta = map[string]fieldMeta{
	"bin":             {label: "Executable", help: "Empty looks on PATH, then in ~/.opencode/bin."},
	"endpoint":        {label: "Endpoint", help: "Base URL of an OpenAI-compatible model host (an Ollama's http://localhost:11434/v1)."},
	"idle_timeout":    {label: "Idle timeout", help: "How long a run may print nothing before it is stopped: at least 1m, or off."},
	"request_timeout": {label: "Request timeout", help: "How long one provider request may take when an endpoint is set: at least 1m, or off."},
	"chunk_timeout":   {label: "Chunk timeout", help: "How long a stream may go without a chunk before the request ends: at least 10s. OpenCode has no way to turn this off."},
	"context":         {label: "Context tokens"},
	"output":          {label: "Output tokens"},
}

var projectMeta = map[string]fieldMeta{
	"repos":         {label: "Repos", help: "Members, each a key of repos and in no other project."},
	"brief_max_age": {label: "Brief max age", help: "How old the approved brief may be before it shows as stale: 30d or 720h."},
	"budget.amount": {label: "Budget (USD)", help: "Dollars per period; unset takes the daemon's, 0 is no cap."},
	"budget.period": {label: "Budget period", choices: []string{config.PeriodDay, config.PeriodMonth}},
	"budget.cap":    {label: "Budget cap", choices: config.Caps},
	"caches":        {label: "Caches", help: "Local build caches that are not repos."},
	"intake":        {label: "Intake rules", help: "Checked in order for a request from another project; first match wins."},
}

// configSections describes the whole form.
func configSections() []api.ConfigSection {
	return []api.ConfigSection{
		{ID: "daemon", Label: "Daemon", Root: []string{"daemon"}, Fields: describe(reflect.TypeFor[config.Daemon](), daemonMeta)},
		{ID: "desk", Label: "Front desk", Root: []string{"desk"}, Fields: describe(reflect.TypeFor[config.Desk](), deskMeta)},
		{ID: "chat", Label: "Chat", Root: []string{"chat"}, Fields: describe(reflect.TypeFor[config.Chat](), chatMeta)},
		{ID: "opencode", Label: "OpenCode", Root: []string{"opencode"}, Fields: describe(reflect.TypeFor[config.OpenCode](), openCodeMeta)},
		{ID: "defaults", Label: "Defaults", Root: []string{"defaults"}, Fields: describe(reflect.TypeFor[config.Profile](), profileMeta)},
		{ID: "repo", Label: "Repo profile", Root: []string{"repos"}, Many: true, Inherits: "defaults", Fields: describe(reflect.TypeFor[config.Profile](), profileMeta)},
		{ID: "project", Label: "Project", Root: []string{"projects"}, Many: true, Fields: describe(reflect.TypeFor[config.Project](), projectMeta)},
	}
}

// describe lists a struct's settings: its yaml-tagged fields, with nested structs
// flattened to dotted keys.
func describe(t reflect.Type, meta map[string]fieldMeta) []api.ConfigField {
	var out []api.ConfigField
	var walk func(t reflect.Type, prefix []string)
	walk = func(t reflect.Type, prefix []string) {
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if !f.IsExported() || name == "-" || name == "" {
				continue
			}
			key := append(append([]string{}, prefix...), name)
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, key)
				continue
			}
			kind, nullable := kindOf(f.Type)
			m := meta[strings.Join(key, ".")]
			if m.kind != "" {
				kind = m.kind
			}
			if len(m.choices) > 0 && kind == api.KindString {
				kind = api.KindEnum
			}
			label := m.label
			if label == "" {
				label = strings.ReplaceAll(name, "_", " ")
				label = strings.ToUpper(label[:1]) + label[1:]
			}
			out = append(out, api.ConfigField{Key: key, Kind: kind, Label: label, Help: m.help, Group: m.group,
				Choices: m.choices, Nullable: nullable, Risk: m.risk, Restart: m.restart})
		}
	}
	walk(t, nil)
	return out
}

func kindOf(t reflect.Type) (kind string, nullable bool) {
	if t.Kind() == reflect.Pointer {
		t, nullable = t.Elem(), true
	}
	switch t.Kind() {
	case reflect.String:
		return api.KindString, false
	case reflect.Bool:
		return api.KindBool, nullable
	case reflect.Int, reflect.Int64, reflect.Float64:
		return api.KindNumber, nullable
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return api.KindList, false
		}
	case reflect.Map:
		if t.Elem().Kind() == reflect.String {
			return api.KindMap, false
		}
	}
	return api.KindJSON, false
}
