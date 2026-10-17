package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/version"
)

// shepherd mcp serves Shepherd's operator tools over MCP on stdio (newline-delimited
// JSON-RPC 2.0). Each tool runs the CLI command of the same name, so the two never
// drift: the MCP server is one more client of the daemon, like the CLI.

// mcpVersions are the protocol revisions this server speaks; it answers with the
// client's when it is one of these, else the newest. The tools use only base features.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = `Shepherd keeps parallel work in a workspace of git repos apart. A lane is one stream of work in one repo: its own branch, cut from a fresh fetch of the repo's base branch, its own worktree, and a scope (globs relative to the repo) that its changes stay inside. Open a lane before changing a repo, and work only in the lane's worktree. Scopes are leases: an overlapping scope is refused, naming the lane that holds it. A pre-push hook refuses pushes outside the scope. Close a lane once its MR is merged. Never pass force to lane_close unless the person has confirmed the MR is merged (a squash merge looks unmerged to git).`

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// args turns the call's arguments into CLI arguments.
	args func(map[string]any) ([]string, error) `json:"-"`
	// more says how to read what a capped output left out (see capOutput).
	more string
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	propRepo = map[string]any{"type": "string", "description": "Repo, by its path in the workspace as lane_list and shepherd_status show it (for example \"ubixshepherd\")."}
	propPath = map[string]any{"type": "string", "description": "Absolute directory to resolve. Default: where the MCP server runs."}
)

func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "shepherd_status",
			Description: "The daemon, its workspaces with repo and lane counts, and what the current directory resolves to.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"status"}, nil },
		},
		{
			Name:        "workspace_rename",
			Description: "Rename a registered workspace. Changes the name only: its path, repos, lanes and history stay. Refused for an empty name or one another workspace already uses. Anything that names the old one (--workspace, $SHEPHERD_WORKSPACE, hosts.yaml) needs the new name.",
			InputSchema: obj(map[string]any{
				"workspace": map[string]any{"type": "string", "description": "The workspace to rename: its current name or its path (shepherd_status lists them)."},
				"name":      map[string]any{"type": "string", "description": "The new name: non-empty, no slashes."},
			}, "workspace", "name"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"workspace", "rename", str(a, "workspace"), str(a, "name")}, nil
			},
		},
		{
			Name:        "shepherd_where",
			Description: "Which workspace, repo and lane a directory belongs to, with the repo's profile (base branch, branch model, gate, shared paths, autonomy).",
			InputSchema: obj(map[string]any{"path": propPath}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"where"}
				if p := str(a, "path"); p != "" {
					out = append(out, p)
				}
				return out, nil
			},
		},
		{
			Name:        "lane_list",
			Description: "Open lanes: repo, name, state, age, scope, and who opened each (the surface, with agent, run, session, pid and directory where known; unknown for lanes from before Shepherd recorded it). Without repo, every repo in the workspace.",
			InputSchema: obj(map[string]any{"repo": propRepo}),
			args: func(a map[string]any) ([]string, error) {
				if r := str(a, "repo"); r != "" {
					return []string{"lane", "list", "--repo", r}, nil
				}
				return []string{"lane", "list", "--all"}, nil
			},
			more: "Pass repo to list one repo's lanes.",
		},
		{
			Name:        "lane_open",
			Description: "Open a lane: fetch, cut the branch from origin/<base>, add the worktree, record the scope. Returns the worktree path to work in. Refused if the scope overlaps an open lane's.",
			InputSchema: obj(map[string]any{
				"repo":   propRepo,
				"name":   map[string]any{"type": "string", "description": "Lane and branch name: lowercase, at most one slash (feat/login, fix-crash)."},
				"scope":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Globs relative to the repo the work stays inside: \"src/auth/**\", \"docs/*.md\", \"README.md\"."},
				"branch": map[string]any{"type": "string", "description": "Branch name if it should differ from the lane name."},
				"item":   map[string]any{"type": "integer", "description": "The next-up item (next_list) this lane is for; it links the item to the lane."},
			}, "repo", "name", "scope"),
			args: func(a map[string]any) ([]string, error) {
				scope, err := strs(a, "scope")
				if err != nil {
					return nil, err
				}
				out := []string{"lane", "open", str(a, "name"), "--repo", str(a, "repo")}
				for _, s := range scope {
					out = append(out, "--scope", s)
				}
				if b := str(a, "branch"); b != "" {
					out = append(out, "--branch", b)
				}
				if n, ok := a["item"].(float64); ok && n > 0 {
					out = append(out, "--item", fmt.Sprint(int64(n)))
				}
				return out, nil
			},
		},
		{
			Name:        "lane_widen",
			Description: "Add globs to an open lane's scope, so an agent can touch files it asked about. Only after the person approved exactly those paths in a decision (decision_list shows it answered) and in a turn they started: Shepherd refuses it otherwise. Refused too if another open lane holds an overlapping glob (the reason names the lane), or the decision is not an answered one from this lane's runs. It only adds; the change is recorded in the feed.",
			InputSchema: obj(map[string]any{
				"repo":     propRepo,
				"name":     map[string]any{"type": "string", "description": "The lane's name."},
				"add":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Globs relative to the repo to add, only those the person approved."},
				"decision": map[string]any{"type": "integer", "description": "The answered decision, raised by one of this lane's runs, in which the person approved them."},
			}, "repo", "name", "add", "decision"),
			args: func(a map[string]any) ([]string, error) {
				add, err := strs(a, "add")
				if err != nil {
					return nil, err
				}
				d, ok := a["decision"].(float64)
				if !ok || d <= 0 {
					return nil, fmt.Errorf("decision must be a number")
				}
				out := []string{"lane", "widen", str(a, "name"), "--repo", str(a, "repo"), "--decision", fmt.Sprint(int64(d))}
				for _, g := range add {
					out = append(out, "--add", g)
				}
				return out, nil
			},
		},
		{
			Name:        "lane_close",
			Description: "Close a lane: remove its worktree and delete its branch if git sees it merged. Refuses uncommitted changes or an unmerged branch unless force is set.",
			InputSchema: obj(map[string]any{
				"repo":  propRepo,
				"name":  map[string]any{"type": "string", "description": "The lane's name."},
				"force": map[string]any{"type": "boolean", "description": "Close anyway, discarding uncommitted changes and keeping an unmerged branch. Only after the person confirms the MR is merged."},
			}, "repo", "name"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"lane", "close", str(a, "name"), "--repo", str(a, "repo")}
				if f, _ := a["force"].(bool); f {
					out = append(out, "--force")
				}
				return out, nil
			},
		},
		{
			Name: "lane_ship",
			Description: "Push a lane's committed, unpushed work and open or update its merge request, the way a run's work is shipped when it ends. Shepherd checks the scope and the repo's commit rules and runs the repo's gate itself first (this can take minutes), and refuses with the reason if any fails. " +
				"Only for repos whose profile has autonomy.push: shepherd. Never merges: the merge request is the person's to review.",
			InputSchema: obj(map[string]any{
				"repo": propRepo,
				"name": map[string]any{"type": "string", "description": "The lane's name."},
			}, "repo", "name"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"lane", "ship", str(a, "name"), "--repo", str(a, "repo")}, nil
			},
		},
		{
			Name: "lane_run",
			Description: "Start an agent (claude, copilot, cursor or opencode) headless in a lane's worktree on a task, and return at once with the run id. The lane keeps its conversation: by default this continues the lane's last session with the same agent. " +
				"The agent may edit and commit inside the lane's scope and run the repo's gate; it can never push. One agent per lane. Check on it with run_status.",
			InputSchema: obj(map[string]any{
				"repo":        propRepo,
				"lane":        map[string]any{"type": "string", "description": "The lane's name; open it first with lane_open."},
				"agent":       map[string]any{"type": "string", "enum": []string{"claude", "copilot", "cursor", "opencode"}},
				"task":        map[string]any{"type": "string", "description": "What the agent should do, as you would brief a colleague. Shepherd adds the lane, scope and rules."},
				"model":       map[string]any{"type": "string", "description": "Model, if not the repo's default for this agent. Choose it from models_list for the repo; a model the policy does not permit is refused."},
				"effort":      map[string]any{"type": "string", "description": "Effort, in the agent's own words (Claude Code: low, medium, high, xhigh, max), from models_list. Omit for the repo's default. Not every agent or model takes one; then it is not applied and the run's log says so."},
				"new_session": map[string]any{"type": "boolean", "description": "Start a new conversation. By default the lane keeps its conversation: the run continues the lane's last session with this agent."},
			}, "repo", "lane", "agent", "task"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"lane", "run", str(a, "lane"), str(a, "task"), "--repo", str(a, "repo"), "--agent", str(a, "agent"), "--detach"}
				if m := str(a, "model"); m != "" {
					out = append(out, "--model", m)
				}
				if e := str(a, "effort"); e != "" {
					out = append(out, "--effort", e)
				}
				if n, _ := a["new_session"].(bool); n {
					out = append(out, "--new")
				}
				return out, nil
			},
		},
		{
			Name:        "models_list",
			Description: "The models each agent may run in a repo, what each is for, and the default model and effort, under the person's policy in config.yaml. Call it before lane_run and choose model and effort per task from it; a model not listed is refused.",
			InputSchema: obj(map[string]any{
				"repo":  propRepo,
				"agent": map[string]any{"type": "string", "enum": []string{"claude", "copilot", "cursor", "opencode"}, "description": "Only this agent. Default: all."},
			}, "repo"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"models", "--repo", str(a, "repo")}
				if ag := str(a, "agent"); ag != "" {
					out = append(out, "--agent", ag)
				}
				return out, nil
			},
		},
		{
			Name:        "run_continue",
			Description: "Continue a finished run's conversation with a follow-up (same agent, same session, same lane): a correction, the next step, a pipeline failure to fix. Returns the new run's id at once.",
			InputSchema: obj(map[string]any{
				"id":      map[string]any{"type": "integer", "description": "The run whose conversation to continue."},
				"message": map[string]any{"type": "string", "description": "What to tell the agent next."},
				"model":   map[string]any{"type": "string", "description": "A different model for the follow-up, from models_list. Omit to keep the run's."},
				"effort":  map[string]any{"type": "string", "description": "Effort for the follow-up, from models_list. Omit for the repo's default."},
			}, "id", "message"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				out := []string{"run", "continue", fmt.Sprint(int64(id)), str(a, "message"), "--detach"}
				if m := str(a, "model"); m != "" {
					out = append(out, "--model", m)
				}
				if e := str(a, "effort"); e != "" {
					out = append(out, "--effort", e)
				}
				return out, nil
			},
		},
		{
			Name:        "run_list",
			Description: "Recent agent runs across the workspace: id, agent, lane, state, commits, age, task.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"run", "list", "--all"}, nil },
			more:        "run_status with a run's id shows that run.",
		},
		{
			Name:        "run_status",
			Description: "One run: state, time, exit code, commits made, any files changed outside the lane's scope, and its log (long logs keep their start and end).",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"run", "show", fmt.Sprint(int64(id)), "--with-log"}, nil
			},
			more: "The whole log, redacted, is the file on the log line near the top: Read it with an offset and a limit, for the part you need.",
		},
		{
			Name: "usage_stats",
			Description: "Tokens, context sizes and cost over a range of days: agent runs by agent and model, by lane, and the front desk's turns. " +
				"Peak context is the largest single request's prompt in a run or turn; the count over 200k shows whether a 1M-context model was ever needed. Default: the last 7 days.",
			InputSchema: obj(map[string]any{
				"days": map[string]any{"type": "integer", "description": "The last N days, today included. Default 7."},
				"from": map[string]any{"type": "string", "description": "First day, YYYY-MM-DD (instead of days)."},
				"to":   map[string]any{"type": "string", "description": "Last day, YYYY-MM-DD. Default today."},
			}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"stats"}
				if from := str(a, "from"); from != "" {
					out = append(out, "--from", from)
				} else if d, ok := a["days"].(float64); ok {
					out = append(out, "--days", fmt.Sprint(int(d)))
				}
				if to := str(a, "to"); to != "" {
					out = append(out, "--to", to)
				}
				return out, nil
			},
		},
		{
			Name:        "run_stop",
			Description: "Stop a running agent. Its commits so far stay in the lane.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"run", "stop", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name:        "run_dismiss",
			Description: "Dismiss a failed or stopped run from the board, when the person asks to clear it. Nothing is deleted: the run, its log and its record stay, and a later run in the lane still shows. Refused for a run that is running or succeeded.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"run", "dismiss", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name:        "run_undismiss",
			Description: "Bring a dismissed run back onto the board.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"run", "undismiss", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name:        "run_why",
			Description: "Say why a run failed, or why Shepherd could not push what it produced: the reason in one line, the output that shows it (already redacted) and the fixes on offer.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"run", "why", fmt.Sprint(int64(id))}, nil
			},
			more: "run_fix takes one of the fixes. Only those marked [the desk may] are yours; the rest are the person's call.",
		},
		{
			Name: "run_fix",
			Description: "Take one of a failed run's suggested fixes (an id from run_why): continue its session with a follow-up, or retry it. " +
				"You may take only fixes run_why marks as the desk's; a fix that widens scope, spends beyond a cap or changes rules is the person's, and is refused.",
			InputSchema: obj(map[string]any{
				"id":     map[string]any{"type": "integer"},
				"action": map[string]any{"type": "string", "description": "the fix's id from run_why, such as continue or retry"},
			}, "id", "action"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				if str(a, "action") == "" {
					return nil, fmt.Errorf("action is required")
				}
				return []string{"run", "fix", fmt.Sprint(int64(id)), str(a, "action")}, nil
			},
		},
		{
			Name: "lane_review",
			Description: "Judge each of a repo's worktrees, lanes or not: finished (everything in the base or a merged request), live (uncommitted work, an open request, recent activity) or unclear (unlanded work, nothing recent), with the evidence. Changes nothing. " +
				"Use it to tell the person which old worktrees are safe to retire; retiring is theirs to ask for.",
			InputSchema: obj(map[string]any{"repo": propRepo}, "repo"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"lane", "review", "--repo", str(a, "repo")}, nil
			},
			more: "Ask the person to run shepherd lane review in a terminal for the whole report.",
		},
		{
			Name: "lane_diff",
			Description: "What is in a lane against its base, read-only: the commits, the committed changes, the uncommitted changes in the worktree and the untracked files, each file marked inside or OUT of the lane's scope. " +
				"Default: the stat, a few hundred characters for a small lane (head and base, up to 30 commits, one line per file with +added -removed). files=true adds each file's hunk headers, no bodies (more characters, up to 200 files). " +
				"file=<path> returns that one file's patch as the lane would ship it, or an untracked file's start, up to 64 KiB. Start with the stat, then read the files that matter one at a time. Secrets are masked in what it returns.",
			InputSchema: obj(map[string]any{
				"repo":  propRepo,
				"name":  map[string]any{"type": "string", "description": "The lane's name."},
				"files": map[string]any{"type": "boolean", "description": "Add each file's hunk headers to the stat."},
				"file":  map[string]any{"type": "string", "description": "A path relative to the repo: return only that file's patch."},
				"run":   map[string]any{"type": "number", "description": "A run's id in this lane: read only that run's own commits (start to end)."},
			}, "repo", "name"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"lane", "diff", str(a, "name"), "--repo", str(a, "repo")}
				if run, ok := a["run"].(float64); ok && run != 0 {
					out = append(out, "--run", fmt.Sprint(int64(run)))
				}
				files, _ := a["files"].(bool)
				switch file := str(a, "file"); {
				case file != "" && files:
					return nil, fmt.Errorf("pass files or file, not both")
				case file != "":
					out = append(out, "--file", file)
				case files:
					out = append(out, "--files")
				}
				return out, nil
			},
			more: "Read one file's patch with file=<path>; the stat lists the paths.",
		},
		{
			Name: "next_list",
			Description: "The next-up list: work that is planned but not being acted on, in the person's order (approved, then planned, then deferred), each with its plan doc and, once a lane is open for it, the lane and MR. " +
				"The item marked next is the first approved one with no lane. Use it to say what is next up; items marked proposed are not approved. The matrix list is each repo's docs/WORK.md rows with status todo or blocked, read-only (no id), edited in the file.",
			InputSchema: obj(map[string]any{"all": map[string]any{"type": "boolean", "description": "Include done and dropped items."}}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"next", "list"}
				if all, _ := a["all"].(bool); all {
					out = append(out, "--all")
				}
				return out, nil
			},
		},
		{
			Name:        "next_add",
			Description: "Put an item on the next-up list: when the person asks (\"put X on the list\"), or when the person and you agreed in chat on concrete work not being started now (call next_list first to avoid a duplicate; the note quotes what the person said; tell them you added it). It starts planned. Never add work from your own ideas the person has not discussed. Approval is the person's: pass approved only when the person's own words in this turn approve it, never your guess.",
			InputSchema: obj(map[string]any{
				"title":      map[string]any{"type": "string", "description": "One line naming the work: one merge request's worth."},
				"repo":       map[string]any{"type": "string", "description": "The repo it is for, by its name in the workspace."},
				"plan":       map[string]any{"type": "string", "description": "The plan doc, a path under the repo (docs/lane-diff.md)."},
				"lane":       map[string]any{"type": "string", "description": "The name the lane for it is expected to have."},
				"note":       map[string]any{"type": "string"},
				"done_when":  map[string]any{"type": "string", "description": "One line, how to tell the item is done (the acceptance test)."},
				"depends_on": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Ids of the items that must be done first; this item then shows as blocked until they are."},
				"approved":   map[string]any{"type": "boolean", "description": "Only when the person approved it in their own words in this turn."},
				"propose":    map[string]any{"type": "boolean", "description": "Put it up for the person's approval, so it waits in Waiting on you. Leave unset for work agreed in chat: that shows as planned from chat and waits on no one."},
			}, "title"),
			args: func(a map[string]any) ([]string, error) {
				if str(a, "title") == "" {
					return nil, fmt.Errorf("title is required")
				}
				out := []string{"next", "add", str(a, "title")}
				for _, k := range []string{"repo", "plan", "lane", "note"} {
					if v := str(a, k); v != "" {
						out = append(out, "--"+k, v)
					}
				}
				if v := str(a, "done_when"); v != "" {
					out = append(out, "--done-when", v)
				}
				deps, err := dependsArg(a)
				if err != nil {
					return nil, err
				}
				if deps != "" {
					out = append(out, "--depends", deps)
				}
				if ok, _ := a["approved"].(bool); ok {
					out = append(out, "--approved")
				}
				if ok, _ := a["propose"].(bool); ok {
					out = append(out, "--propose")
				}
				return out, nil
			},
		},
		{
			Name: "next_update",
			Description: "Change a next-up item: its title, note, plan, repo or lane hint, its done_when line or depends_on ids (text, not state), and, only in a turn the person started and with their own words, its state or place. " +
				"Shepherd refuses a state or move outside such a turn: propose it to the person instead. started and done are derived from the item's lane, not set.",
			InputSchema: obj(map[string]any{
				"id":         map[string]any{"type": "integer"},
				"title":      map[string]any{"type": "string"},
				"note":       map[string]any{"type": "string"},
				"plan":       map[string]any{"type": "string"},
				"repo":       map[string]any{"type": "string"},
				"lane":       map[string]any{"type": "string", "description": "The name the lane for it is expected to have."},
				"done_when":  map[string]any{"type": "string", "description": "One line, how to tell the item is done; empty clears it."},
				"depends_on": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Ids of the items that must be done first, replacing the list; empty clears it."},
				"state":      map[string]any{"type": "string", "enum": []string{"planned", "approved", "deferred", "dropped", "done"}, "description": "The person's call."},
				"move":       map[string]any{"type": "string", "enum": []string{"top", "up", "down"}, "description": "The person's call: the order is the priority."},
			}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				out := []string{"next", "update", fmt.Sprint(int64(id))}
				for _, k := range []string{"title", "note", "plan", "repo", "lane", "state", "move"} {
					if _, ok := a[k]; ok {
						out = append(out, "--"+k, str(a, k))
					}
				}
				if _, ok := a["done_when"]; ok {
					out = append(out, "--done-when", str(a, "done_when"))
				}
				if _, ok := a["depends_on"]; ok {
					deps, err := dependsArg(a)
					if err != nil {
						return nil, err
					}
					out = append(out, "--depends", deps)
				}
				if len(out) == 3 {
					return nil, fmt.Errorf("say what to change")
				}
				return out, nil
			},
		},
		{
			Name: "topic_list",
			Description: "The conversation topics: the matters the thread is about, most attention first, each with what waits on the person and its linked lanes, items and decisions. " +
				"Prefer an existing topic over a new one.",
			InputSchema: obj(map[string]any{"all": map[string]any{"type": "boolean", "description": "Include cleared topics."}}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"topic", "list"}
				if all, _ := a["all"].(bool); all {
					out = append(out, "--all")
				}
				return out, nil
			},
		},
		{
			Name:        "topic_show",
			Description: "One topic with its links and what waits on the person.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"topic", "show", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name: "topic_set",
			Description: "Name the matter of the turn and tie work to it. Pass id alone to file this turn under an existing topic, or title to make a new one and file the turn under it (at most one new topic per turn; a title that repeats an open topic is refused with its id). " +
				"Turns Shepherd starts for a run, decision or request already in a topic arrive filed. " +
				"Pass one of lane, item, run, decision or request to tie that thing to the topic as well; a lane by name or id. You cannot move a thing that is already in another topic: that is the person's.",
			InputSchema: obj(map[string]any{
				"id":       map[string]any{"type": "integer", "description": "An existing topic."},
				"title":    map[string]any{"type": "string", "description": "A new topic's title: a few words, at most 80 characters."},
				"summary":  map[string]any{"type": "string", "description": "One line saying what it is about."},
				"lane":     map[string]any{"type": []string{"string", "integer"}, "description": "A lane's name (with repo when it is not obvious) or its id, as lane_list shows."},
				"repo":     propRepo,
				"item":     map[string]any{"type": "integer", "description": "A next-up item's id."},
				"run":      map[string]any{"type": "integer"},
				"decision": map[string]any{"type": "integer"},
				"request":  map[string]any{"type": "integer"},
				"turn":     map[string]any{"type": "integer", "description": "A desk turn's id."},
			}),
			args: func(a map[string]any) ([]string, error) {
				var out []string
				switch id, hasID := a["id"].(float64); {
				case hasID:
					out = []string{"topic", "set", fmt.Sprint(int64(id))}
				case str(a, "title") != "":
					out = []string{"topic", "new", str(a, "title")}
				default:
					return nil, fmt.Errorf("pass id or title")
				}
				if v := str(a, "summary"); v != "" {
					out = append(out, "--summary", v)
				}
				for _, k := range []string{"lane", "item", "run", "decision", "request", "turn"} {
					switch v := a[k].(type) {
					case float64:
						out = append(out, "--"+k, fmt.Sprint(int64(v)))
					case string:
						if v = strings.TrimSpace(v); v != "" {
							out = append(out, "--"+k, v)
						}
					}
				}
				if r := str(a, "repo"); r != "" {
					out = append(out, "--repo", r)
				}
				return out, nil
			},
		},
		{
			Name:        "topic_rename",
			Description: "Rename a topic. The person's call: only in a turn they started, with their own words.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}, "title": map[string]any{"type": "string"}}, "id", "title"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok || str(a, "title") == "" {
					return nil, fmt.Errorf("id and title are required")
				}
				return []string{"topic", "rename", fmt.Sprint(int64(id)), str(a, "title")}, nil
			},
		},
		{
			Name:        "topic_merge",
			Description: "Merge a topic into another; its links move and its id keeps resolving. The person's call: only in a turn they started, with their own words.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}, "into": map[string]any{"type": "integer"}}, "id", "into"),
			args: func(a map[string]any) ([]string, error) {
				id, ok1 := a["id"].(float64)
				into, ok2 := a["into"].(float64)
				if !ok1 || !ok2 {
					return nil, fmt.Errorf("id and into must be numbers")
				}
				return []string{"topic", "merge", fmt.Sprint(int64(id)), fmt.Sprint(int64(into))}, nil
			},
		},
		{
			Name: "topic_clear",
			Description: "Clear a settled topic out of the thread. It folds the rows away and keeps them in history; it closes, cancels and answers nothing, and anything that needs the person stays visible. " +
				"The person's call: only in a turn they started, with their own words. Never because a lane finished.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"topic", "clear", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name:        "topic_reopen",
			Description: "Bring a cleared topic back into the thread. The person's call: only in a turn they started.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"topic", "reopen", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name: "question_open",
			Description: "Mark a question you are asking the person in this reply, so it waits in their \"Waiting on you\" list until they answer it. Call it whenever your reply asks them something that needs their answer, with the question in one or two sentences as you asked it. " +
				"Do not call it for rhetorical questions or for an agent's decision (decision_list already holds those). One call per question.",
			InputSchema: obj(map[string]any{
				"text":  map[string]any{"type": "string", "description": "The question, as you asked it."},
				"topic": map[string]any{"type": "integer", "description": "The topic it belongs to; the turn's topic by default."},
			}, "text"),
			args: func(a map[string]any) ([]string, error) {
				t := str(a, "text")
				if strings.TrimSpace(t) == "" {
					return nil, fmt.Errorf("text is required")
				}
				out := []string{"question", "open", t}
				if v, ok := a["topic"].(float64); ok && v > 0 {
					out = append(out, "--topic", fmt.Sprint(int64(v)))
				}
				return out, nil
			},
		},
		{
			Name: "question_answer",
			Description: "Mark an open question answered by the person's message in this turn, when that message plainly answers it (question_list shows the open ones). " +
				"Only in a turn the person started. Never mark one answered because the matter moved on or you guessed; the person can skip a question themselves.",
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "integer"}}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"question", "answer", fmt.Sprint(int64(id))}, nil
			},
		},
		{
			Name:        "question_list",
			Description: "The questions you asked the person that are still open.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"question", "list"}, nil },
		},
		{
			Name:        "session_list",
			Description: "Conversations the person had with agents outside Shepherd (adopted Claude Code sessions): id, repo, title, dates, branches. Ask one with session_ask.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"session", "list"}, nil },
		},
		{
			Name: "session_ask",
			Description: "Ask an adopted conversation a question; it answers with its own history and can read files but not change anything. Use it to recover what was decided or done in work the person did by hand. " +
				"Refused if the conversation looks open in a terminal right now.",
			InputSchema: obj(map[string]any{
				"id":       map[string]any{"type": "string", "description": "The conversation's id, or its first 8 characters."},
				"question": map[string]any{"type": "string"},
			}, "id", "question"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"session", "ask", str(a, "id"), str(a, "question")}, nil
			},
		},
		{
			Name:        "tag_reserve",
			Description: "Reserve the next release version in a repo (major, minor or patch) for a lane, atomically against the remote's tags and other reservations. In repos whose tags are reserved, a release tag must be reserved before it is pushed.",
			InputSchema: obj(map[string]any{
				"repo": propRepo,
				"bump": map[string]any{"type": "string", "enum": []string{"major", "minor", "patch"}},
				"lane": map[string]any{"type": "string", "description": "The lane it is for; leave out for a release cut outside any lane."},
			}, "repo", "bump"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"tag", "reserve", str(a, "bump"), "--repo", str(a, "repo")}
				if l := str(a, "lane"); l != "" {
					out = append(out, "--lane", l)
				} else {
					out = append(out, "--no-lane")
				}
				return out, nil
			},
		},
		{
			Name:        "tag_list",
			Description: "A repo's live tag reservations: version, state (reserved, pushed, verified) and lane.",
			InputSchema: obj(map[string]any{"repo": propRepo}, "repo"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"tag", "list", "--repo", str(a, "repo")}, nil
			},
		},
		{
			Name: "artifact_list",
			Description: "Files agents wrote for the person to read (plans, design notes, reports), newest first, with id, lane, run, state, intent and size. Read-only. " +
				"Filter by repo, lane (name, with repo), run, topic or state; with no filter, the most recent across the workspace. Read one with artifact_read.",
			InputSchema: obj(map[string]any{
				"repo":  propRepo,
				"lane":  map[string]any{"type": "string", "description": "A lane's name (give repo too) or its id."},
				"run":   map[string]any{"type": "integer", "description": "Only what this run produced."},
				"topic": map[string]any{"type": "integer", "description": "Only this conversation topic's artifacts (topic_list)."},
				"state": map[string]any{"type": "string", "enum": []string{"live", "kept", "committed", "discarded", "gone"}},
			}),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"artifact", "list"}
				if r := str(a, "repo"); r != "" {
					out = append(out, "--repo", r)
				}
				switch l := a["lane"].(type) {
				case string:
					if l != "" {
						out = append(out, "--lane", l)
					}
				case float64:
					out = append(out, "--lane", fmt.Sprint(int64(l)))
				}
				for _, k := range []string{"run", "topic"} {
					if n, ok := a[k].(float64); ok && n > 0 {
						out = append(out, "--"+k, fmt.Sprint(int64(n)))
					}
				}
				if s := str(a, "state"); s != "" {
					out = append(out, "--state", s)
				}
				return out, nil
			},
			more: "Narrow the list with repo, lane, run, topic or state.",
		},
		{
			Name: "artifact_read",
			Description: "The text of one artifact (from artifact_list): its latest content, or a version's. Read-only and redacted; a long file keeps its start and end. " +
				"Use it to summarise a plan or answer a question about a file the person has not committed. Images and binary files are not returned.",
			InputSchema: obj(map[string]any{
				"id":      map[string]any{"type": "integer", "description": "The artifact's id from artifact_list."},
				"version": map[string]any{"type": "string", "description": "A version's sha-256 (or a prefix of it) from artifact_list's show; default the latest."},
			}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				out := []string{"artifact", "cat", fmt.Sprint(int64(id))}
				if v := str(a, "version"); v != "" {
					out = append(out, "--version", v)
				}
				return out, nil
			},
			more: "Ask the person to open the artifact in the web for the whole file.",
		},
		{
			Name:        "decision_list",
			Description: "Questions agents are holding for the person, with their options and recommendations. Bring these to the person; do not answer them yourself.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"decision", "list"}, nil },
		},
		{
			Name:        "decision_answer",
			Description: "Record the person's answer to a decision; Shepherd continues the asking agent's conversation with it. Only with the person's own answer, never your guess at it.",
			InputSchema: obj(map[string]any{
				"id":     map[string]any{"type": "integer"},
				"answer": map[string]any{"type": "string", "description": "The person's answer, in their words, or an option number."},
			}, "id", "answer"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				return []string{"decision", "answer", fmt.Sprint(int64(id)), str(a, "answer")}, nil
			},
		},
		{
			Name:        "keep_going",
			Description: "Keep-going mode: on, off or status. While on, in a [Shepherd] event turn you may start the next approved Next-up item and continue approved next steps, within limits Shepherd enforces (spend cap, concurrent runs, idle time, end of day, starts per lane); a refusal comes back with its reason, relay it. Use on only when the person asks you to keep going, in a turn they started: Shepherd refuses it otherwise. off is always allowed. status says the numbers and what it last did.",
			InputSchema: obj(map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{"on", "off", "status"}},
				"cap":    map[string]any{"type": "number", "description": "on: dollars it may spend this session. Default keep_going.cap."},
				"runs":   map[string]any{"type": "integer", "description": "on: runs that may run at once. Default keep_going.max_runs."},
				"idle":   map[string]any{"type": "number", "description": "on: hours with nothing started or answered before it turns off. Default keep_going.idle_hours."},
			}, "action"),
			args: func(a map[string]any) ([]string, error) {
				act := str(a, "action")
				if act != "on" && act != "off" && act != "status" {
					return nil, fmt.Errorf("action must be on, off or status")
				}
				out := []string{"keep-going", act}
				if act == "on" {
					if v, ok := a["cap"].(float64); ok && v > 0 {
						out = append(out, "--cap", fmt.Sprint(v))
					}
					if v, ok := a["runs"].(float64); ok && v > 0 {
						out = append(out, "--runs", fmt.Sprint(int(v)))
					}
					if v, ok := a["idle"].(float64); ok && v > 0 {
						out = append(out, "--idle", fmt.Sprint(v))
					}
				}
				return out, nil
			},
		},
		{
			Name:        "request_list",
			Description: "Requests between lanes: agents asking each other, through Shepherd, for answers, hand-offs and reviews. needs_routing ones wait for you to say which lane (and agent).",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"request", "list"}, nil },
		},
		{
			Name:        "request_route",
			Description: "Route a request Shepherd could not route by rule: name the target lane (open it first with lane_open if needed) and optionally the agent. Shepherd starts or continues that agent and carries the reply back to the asker.",
			InputSchema: obj(map[string]any{
				"id":    map[string]any{"type": "integer"},
				"lane":  map[string]any{"type": "string"},
				"agent": map[string]any{"type": "string", "enum": []string{"claude", "copilot", "cursor", "opencode"}},
			}, "id", "lane"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				out := []string{"request", "route", fmt.Sprint(int64(id)), "--lane", str(a, "lane")}
				if ag := str(a, "agent"); ag != "" {
					out = append(out, "--agent", ag)
				}
				return out, nil
			},
		},
		{
			Name:        "request_close",
			Description: "Close a request between lanes without a reply: one gone stale or no longer needed. A target agent already working on it is left to finish, but its reply is not carried back. Say why; the reason is kept on the request.",
			InputSchema: obj(map[string]any{
				"id":  map[string]any{"type": "integer"},
				"why": map[string]any{"type": "string", "description": "Why it is closed."},
			}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				out := []string{"request", "close", fmt.Sprint(int64(id))}
				if w := str(a, "why"); w != "" {
					out = append(out, "--why", w)
				}
				return out, nil
			},
		},
		{
			Name:        "links_thread",
			Description: "The clickable URL of a GitLab review thread's first note (<mr url>#note_<id>), from the merge request's iid and the thread's discussion id. Use it, with the thread's title as link text, whenever a report names a thread; a discussion id alone is not linkable. Also gives the URL for kind mr, pipeline or job.",
			InputSchema: obj(map[string]any{
				"id":         map[string]any{"type": "integer", "description": "The merge request's iid (kind thread or mr), or the pipeline or job id."},
				"discussion": map[string]any{"type": "string", "description": "The thread's discussion id (kind thread)."},
				"kind":       map[string]any{"type": "string", "enum": []string{"thread", "mr", "pipeline", "job"}, "description": "Default thread."},
				"repo":       map[string]any{"type": "string", "description": "Repo name; default the current one."},
			}, "id"),
			args: func(a map[string]any) ([]string, error) {
				id, ok := a["id"].(float64)
				if !ok {
					return nil, fmt.Errorf("id must be a number")
				}
				kind := str(a, "kind")
				if kind == "" {
					kind = "thread"
				}
				out := []string{"links", kind, fmt.Sprint(int64(id))}
				if kind == "thread" {
					if str(a, "discussion") == "" {
						return nil, fmt.Errorf("discussion is required for a thread")
					}
					out = append(out, str(a, "discussion"))
				}
				if r := str(a, "repo"); r != "" {
					out = append(out, "--repo", r)
				}
				return out, nil
			},
		},
		{
			Name:        "fold_gc",
			Description: "List worktrees across the workspace that look finished (merged, branch gone, missing) and lanes whose worktree is gone. Changes nothing.",
			InputSchema: obj(map[string]any{}),
			args:        func(map[string]any) ([]string, error) { return []string{"fold", "gc"}, nil },
		},
	}
}

func str(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}

// dependsArg reads depends_on, an array of item ids, as the comma list --depends takes.
func dependsArg(a map[string]any) (string, error) {
	raw, ok := a["depends_on"]
	if !ok || raw == nil {
		return "", nil
	}
	list, ok := raw.([]any)
	if !ok {
		return "", fmt.Errorf("depends_on must be an array of item ids")
	}
	ids := make([]string, 0, len(list))
	for _, v := range list {
		n, ok := v.(float64)
		if !ok || n < 1 || n != float64(int64(n)) {
			return "", fmt.Errorf("depends_on must be an array of item ids")
		}
		ids = append(ids, fmt.Sprint(int64(n)))
	}
	return strings.Join(ids, ","), nil
}

func strs(a map[string]any, k string) ([]string, error) {
	raw, ok := a[k].([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", k)
	}
	var out []string
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", k)
		}
		out = append(out, s)
	}
	return out, nil
}

const workerInstructions = `You are an agent Shepherd started in a lane. These tools are how you reach Shepherd and the person. ask_human: anything that is the person's call (money or pricing, published or user-facing text, deleting or overwriting data, production, a scope or design change with no clear default); give options and your recommendation, then end your turn, and you will be continued with the answer. ask_shepherd: you need another lane (an answer from it, a change outside your scope, a review); then end your turn, and you will be continued with the reply. report: progress, done (what you did and how you checked it) or blocked (why). artifact: a plan, design note or report the person should read; write it in your worktree and declare it with intent commit, keep or scratch. Everything else, keep working without asking.`

func workerTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "report",
			Description: "Tell Shepherd how the work stands: progress, done (what you did and how you checked it) or blocked (why). Shepherd checks a claim of done against the lane's commits and gate.",
			InputSchema: obj(map[string]any{
				"status":  map[string]any{"type": "string", "enum": []string{"progress", "done", "blocked"}},
				"summary": map[string]any{"type": "string"},
			}, "status", "summary"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"worker", "report", "--status", str(a, "status"), str(a, "summary")}, nil
			},
		},
		{
			Name: "ask_human",
			Description: "Hold a decision for the person: money or pricing, published or user-facing text, deleting or overwriting data, production, or a scope or design change with no clear default. " +
				"Give the options and your recommendation. Then end your turn: Shepherd continues this conversation with the answer. " +
				"The person reads it as markdown: open the question with a one-line summary, then short sections or bullets (what you propose, the files, the risk); keep each option to a short phrase.",
			InputSchema: obj(map[string]any{
				"question":       map[string]any{"type": "string", "description": "Markdown: a one-line summary first, then short sections or bullets. Not a wall of text."},
				"options":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Short phrases the person can pick; one line each."},
				"recommendation": map[string]any{"type": "string", "description": "Which option you recommend, and why, in a sentence."},
				"why":            map[string]any{"type": "string", "description": "Why this is the person's call rather than yours."},
				"artifacts":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Files in your worktree (a plan, a design note) the person should read to decide; they are shown beside the question."},
			}, "question", "recommendation"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"worker", "ask-human", str(a, "question"), "--recommendation", str(a, "recommendation"), "--why", str(a, "why")}
				if raw, ok := a["options"]; ok && raw != nil {
					opts, err := strs(a, "options")
					if err != nil {
						return nil, err
					}
					for _, o := range opts {
						out = append(out, "--option", o)
					}
				}
				if raw, ok := a["artifacts"]; ok && raw != nil {
					paths, err := strs(a, "artifacts")
					if err != nil {
						return nil, err
					}
					for _, p := range paths {
						out = append(out, "--artifact", p)
					}
				}
				return out, nil
			},
		},
		{
			Name: "artifact",
			Description: "Tell Shepherd about a file in your worktree the person may want to read: a plan, a design note, a report, a diagram. " +
				"Shepherd shows it beside your lane and keeps its versions. Intent: commit (it belongs in the repo), keep (for the person, not the repo) or scratch (working notes). Declare it again after you change it.",
			InputSchema: obj(map[string]any{
				"path":   map[string]any{"type": "string", "description": "The file, relative to your worktree."},
				"title":  map[string]any{"type": "string", "description": "A short title."},
				"intent": map[string]any{"type": "string", "enum": []string{"commit", "keep", "scratch"}},
			}, "path"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"worker", "artifact", str(a, "path")}
				if t := str(a, "title"); t != "" {
					out = append(out, "--title", t)
				}
				if i := str(a, "intent"); i != "" {
					out = append(out, "--intent", i)
				}
				return out, nil
			},
		},
		{
			Name:        "tag_reserve",
			Description: "Reserve the next release version for your lane (major, minor or patch) before you tag. Tag exactly the version you are given, on a commit that includes your merged work.",
			InputSchema: obj(map[string]any{"bump": map[string]any{"type": "string", "enum": []string{"major", "minor", "patch"}}}, "bump"),
			args: func(a map[string]any) ([]string, error) {
				return []string{"worker", "tag-reserve", str(a, "bump")}, nil
			},
		},
		{
			Name: "ask_shepherd",
			Description: "Ask for something from another lane: a question to the agent working there, a hand-off of work outside your scope, or a review. " +
				"Kind person is for the person instead, and becomes a decision for them, as ask_human does. " +
				"Then end your turn: Shepherd continues this conversation with the reply.",
			InputSchema: obj(map[string]any{
				"kind":    map[string]any{"type": "string", "enum": []string{"question", "handoff", "review", "person"}},
				"message": map[string]any{"type": "string", "description": "What you need, as you would ask a colleague."},
				"lane":    map[string]any{"type": "string", "description": "The lane it is for, if you know it."},
			}, "kind", "message"),
			args: func(a map[string]any) ([]string, error) {
				out := []string{"worker", "ask-shepherd", str(a, "message"), "--kind", str(a, "kind")}
				if l := str(a, "lane"); l != "" {
					out = append(out, "--lane", l)
				}
				return out, nil
			},
		},
	}
}

func runMCP(ctx context.Context, env Env, args []string) error {
	fs := flags("mcp", env)
	worker := fs.Bool("worker", false, "serve the worker tools, for an agent Shepherd started")
	scoped := fs.Bool("scoped", false, "refuse to start without SHEPHERD_TOKEN (for the daemon's front desk)")
	maxOut := fs.Int("max-output", 0, "cap what each tool returns at this many characters, keeping its start and end (the daemon's front desk passes desk.tool_output_chars)")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	// Started by Shepherd (an agent's worker tools, or the daemon's front desk): call
	// the daemon with the token it was given, never daemon.json's operator token.
	if *scoped && os.Getenv(dispatch.EnvToken) == "" {
		return fmt.Errorf("--scoped needs %s: this server is for the daemon's front desk, and will not fall back to daemon.json", dispatch.EnvToken)
	}
	if os.Getenv(dispatch.EnvToken) != "" {
		scoped, done, err := scopedEnv(env)
		if err != nil {
			return err
		}
		defer done()
		env = scoped
	}
	if *maxOut < 0 {
		return fmt.Errorf("--max-output %d: a number of characters, or 0 for no cap of its own", *maxOut)
	}
	if *worker {
		return serveMCP(ctx, env, env.Stdin, env.Stdout, workerTools(), workerInstructions, *maxOut)
	}
	return serveMCP(ctx, env, env.Stdin, env.Stdout, mcpTools(), mcpInstructions, *maxOut)
}

// scopedEnv is env with its commands pointed at the daemon in SHEPHERD_URL with the
// token in SHEPHERD_TOKEN, through a runtime file of its own in a private directory:
// commands find their daemon by the runtime file, so they all use that token. It never
// starts a daemon. done removes the directory.
func scopedEnv(env Env) (Env, func(), error) {
	u, err := url.Parse(os.Getenv(dispatch.EnvURL))
	if err != nil || u.Host == "" {
		return env, nil, fmt.Errorf("%s is set but %s is not a daemon URL", dispatch.EnvToken, dispatch.EnvURL)
	}
	dir, err := os.MkdirTemp("", "shepherd-mcp-")
	if err != nil {
		return env, nil, err
	}
	done := func() { os.RemoveAll(dir) }
	b, _ := json.Marshal(api.Runtime{Addr: u.Host, Token: os.Getenv(dispatch.EnvToken)})
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), b, 0o600); err != nil {
		done()
		return env, nil, err
	}
	env.Layout = paths.Layout{Home: dir}
	env.Autostart = false
	return env, done, nil
}

// serveMCP serves toolset; maxOut > 0 caps what each tool returns (capOutput).
func serveMCP(ctx context.Context, env Env, in io.Reader, out io.Writer, toolset []mcpTool, instructions string, maxOut int) error {
	tools := map[string]mcpTool{}
	var list []mcpTool
	for _, t := range toolset {
		tools[t.Name] = t
		list = append(list, t)
	}
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(msg.ID) == 0 {
			continue // a notification (initialized, cancelled): nothing to answer
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: msg.ID}
		switch msg.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(msg.Params, &p)
			v := mcpVersions[0]
			for _, s := range mcpVersions {
				if s == p.ProtocolVersion {
					v = s
				}
			}
			resp.Result = map[string]any{
				"protocolVersion": v,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "shepherd", "version": version.Version},
				"instructions":    instructions,
			}
		case "ping":
			resp.Result = map[string]any{}
		case "tools/list":
			resp.Result = map[string]any{"tools": list}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				resp.Error = &rpcError{-32602, err.Error()}
				break
			}
			t, ok := tools[p.Name]
			if !ok {
				resp.Error = &rpcError{-32602, "unknown tool " + p.Name}
				break
			}
			resp.Result = callTool(ctx, env, t, p.Arguments, maxOut)
		default:
			resp.Error = &rpcError{-32601, "method not found: " + msg.Method}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// callTool runs the tool's CLI command with output captured. A failing command is a
// tool error the model can read and act on, not a protocol error.
func callTool(ctx context.Context, env Env, t mcpTool, a map[string]any, maxOut int) map[string]any {
	if a == nil {
		a = map[string]any{}
	}
	text, isErr := "", false
	args, err := t.args(a)
	if err != nil {
		text, isErr = err.Error(), true
	} else {
		var stdout, stderr bytes.Buffer
		cenv := env
		cenv.Stdin, cenv.Stdout, cenv.Stderr = strings.NewReader(""), &stdout, &stderr
		cenv.Interactive = false
		cenv.Client = "mcp"
		if p := str(a, "path"); p != "" && filepath.IsAbs(p) {
			cenv.Cwd = p
		}
		code := Run(ctx, cenv, args)
		text = strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		isErr = code != 0
	}
	if text == "" {
		text = "done"
	}
	// Keep a long agent log from flooding the session: its start and its end matter most.
	if max := 12000; len(text) > max {
		text = text[:2000] + "\n\n[... " + fmt.Sprint(len(text)-max) + " bytes cut ...]\n\n" + text[len(text)-(max-2000):]
	}
	if maxOut > 0 {
		text = capOutput(text, maxOut, t.more)
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}

// capOutput keeps a tool's output within max characters for a caller that rereads it on
// every turn (the daemon's front desk): its start, where the summary lines are, and its
// end, where a log's outcome is, with a line saying what was cut and how to read more.
func capOutput(text string, max int, more string) string {
	r := []rune(text)
	if len(r) <= max {
		return text
	}
	if more == "" {
		more = "Narrow the call if it takes arguments, or ask the person to look in a terminal."
	}
	note := "\n\n[... %d characters cut: the front desk sees at most %d of a tool's output (desk.tool_output_chars). " + more + " ...]\n\n"
	room := max - len([]rune(fmt.Sprintf(note, len(r), max)))
	if room < 2 {
		return string(r[:max])
	}
	head := room * 2 / 5
	tail := room - head
	return string(r[:head]) + fmt.Sprintf(note, len(r)-head-tail, max) + string(r[len(r)-tail:])
}
