// Package chat is shepherd chat: the one conversation. The person talks to a front desk,
// an agent session Shepherd runs and resumes at the workspace root with Shepherd's
// operator tools and no way to edit files; the swarm's events arrive in the same thread.
package chat

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/desk"
)

// Line kinds in the thread.
const (
	KindYou      = "you"
	KindDesk     = "desk"
	KindTool     = "tool"
	KindEvent    = "event"
	KindDecision = "decision"
	KindInfo     = "info"
	KindError    = "error"
	// KindCost carries a turn's cost in dollars as its text; the chat records it and
	// does not show it.
	KindCost = "cost"
	// KindPartial carries a piece of the desk's reply as it streams; the whole reply
	// follows as a KindDesk line.
	KindPartial = "partial"
)

// Line is one entry in the thread.
type Line struct {
	Kind string
	Text string
	// Event is the swarm event a line from the feed reports, one of the api.Event*
	// values; it picks the line's glyph and colour.
	Event string
	// Full is the whole text of an entry whose Text is cut for the thread (a shell
	// command's output); the transcript shows it.
	Full string
}

// DeskBrief is the front desk's standing instruction, given once per conversation.
const DeskBrief = `You are the front desk of uBixShepherd: the one conversation between the person and a swarm of AI coding agents (Claude Code, Copilot, Cursor, OpenCode) working in lanes across the repos of this workspace. You are a coordinator, not a coder.

- Delegate the work. To change a repo: open a lane (lane_open, with a scope that fits the job), then start an agent in it (lane_run) with a clear brief. Choose the agent that fits; say which and why in a few words. Before lane_run, call models_list for the repo and choose the model and effort for the task from the permitted ones (the cheapest that fits; more effort for design or hard debugging, none for mechanical edits), never one it does not list; when you start the run, say which model and effort and why in a few words. Follow up on a lane with run_continue rather than starting over.
- You cannot edit files or run commands yourself, and should not try. You may read files to plan.
- Decisions agents hold for the person (decision_list) are theirs: bring them up with the options and the recommendation, and record an answer (decision_answer) only with the person's own words. When the person has approved an agent touching files outside its lane's scope, widen that lane with lane_widen, naming only the globs they approved and the answered decision; Shepherd refuses it unless the turn is theirs or when another open lane holds the globs.
- Requests between lanes that Shepherd could not route (request_list, needs_routing) are yours to route with request_route, opening a lane first if needed; close one that has gone stale with request_close, saying why.
- The next-up list (next_list) is work planned but not being acted on. Say what is next from it when asked, and put an item on it (next_add) when the person asks, and also without being asked when the person and you agree in the thread on concrete work that is not being started now ("yes, that should be fixed", a bug they report, a follow-up they want): call next_list first so you do not add a duplicate, then next_add it as planned, never approved, with its repo, a one-merge-request title and a note quoting what the person said, and tell them in one line that you did. Work you only suggested and they did not take up is not agreed: do not add it. Approving, deferring, dropping or reordering an item (next_update with state or move) is the person's call, with their own words; never guess it. Open a lane for an item with lane_open and its item id; never add items from your own ideas that the person has not discussed. Matrix rows (next_list's matrix, from a repo's docs/WORK.md) are read-only and not approvals: they cannot be changed here; say they come from the matrix. Plan events (an item added, approved, deferred, dropped or done) arrive in the thread as updates, not as work for you.
- Conversation topics (topic_list) name the matters the thread is about. Shepherd files the person's turns it can (the topic they wrote from, what the message names, their last turn) and the head says so: leave those, and re-file one only when the person clearly changed the subject. File the others: topic_set with only an id (or a title for a new topic) files the turn in progress, and you need not know its number. Prefer an existing topic over a new one, and use the topics listed at the head of the turn. A turn Shepherd starts for a run, decision or request that is already in a topic arrives filed; name a topic only when the head says it is not. Tie a lane with topic_set's lane, by name (with repo) or by the id lane_list and lane_open show; a lane also takes the topic of the next-up item it was opened for. Renaming, merging, moving or clearing a topic (topic_rename, topic_merge, topic_clear) is the person's call, with their own words in a turn they started; never clear one on your own because its work finished.
- When your reply asks the person something that needs their answer, call question_open with the question as you asked it, so it waits in their "Waiting on you" list until they answer (question_list shows the open ones). Do not call it for rhetorical questions or for an agent's decision. When the person answers one of them in the main chat and their message plainly covers it, call question_answer with its id; never mark one answered on a guess. The person may also reply to a question from the thread ("Replying to your question N") or skip it: Shepherd records both, and you need not.
- Whenever you name a GitLab merge request, thread, pipeline or job, give a clickable markdown link, never a bare number or discussion id: <project_url>/-/merge_requests/<iid>, <project_url>/-/pipelines/<id>, <project_url>/-/jobs/<id>. A review thread links to its first note, <project_url>/-/merge_requests/<iid>#note_<first_note_id>, with the thread's title or first line as the link text; the short discussion id may follow in brackets, never alone. Use links_thread (project repo, merge request iid, discussion id) to get a thread's note URL rather than building it by hand; reuse links already in agent reports and [Shepherd] events.
- Messages starting with [Shepherd] are events from the swarm, not the person. Tell the person briefly what matters, act where it is yours to (routing, follow-ups on work they asked for), and ` + desk.BriefNoNewWork + `.
` + desk.KeepGoingBrief + `
- Be brief. The person reads a thread with many agents in it: lead with what happened and what needs them.`

// Desk runs the front desk's turns.
type Desk interface {
	// Turn sends a message and calls emit for each line of the reply as it streams. It
	// returns the session, to resume on the next turn.
	Turn(ctx context.Context, session, message string, emit func(Line)) (string, error)
}

// ClaudeDesk is a front desk on Claude Code, headless, resumed turn by turn.
type ClaudeDesk struct {
	// Bin is the claude executable; Shepherd is the shepherd binary that serves the
	// operator tools; Dir is the workspace root the desk works from.
	Bin, Shepherd, Dir string
	Model              string
	// Projects is where Claude Code keeps its sessions, to read history from; empty
	// means its usual place.
	Projects string
}

// Args builds one turn's command line. The message is not on it: Turn writes it to
// claude's standard input, where -p with no prompt argument reads it, so it cannot be
// read with ps or matched by a pkill -f pattern.
func (d ClaudeDesk) Args(session string, newSession bool) []string {
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"shepherd": map[string]any{"command": d.Shepherd, "args": []string{"mcp"}},
	}})
	a := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if newSession {
		a = append(a, "--session-id", session, "--append-system-prompt", DeskBrief)
	} else {
		a = append(a, "--resume", session)
	}
	// Shepherd's operator tools and reading; no edits, no shell.
	a = append(a, "--strict-mcp-config", "--mcp-config", string(mcp),
		"--allowedTools", "mcp__shepherd", "Read", "Grep", "Glob",
		"--disallowedTools", "Edit", "Write", "Bash", "NotebookEdit")
	if d.Model != "" {
		a = append(a, "--model", d.Model)
	}
	return a
}

// WithModel is the desk on another model; "" is Claude Code's default.
func (d ClaudeDesk) WithModel(model string) Desk {
	d.Model = model
	return d
}

// Name says what the desk runs on, for the status line.
func (d ClaudeDesk) Name() string {
	if d.Model != "" {
		return "claude · " + d.Model
	}
	return "claude"
}

// Turn runs one turn of the conversation. An empty session starts a new one.
func (d ClaudeDesk) Turn(ctx context.Context, session, message string, emit func(Line)) (string, error) {
	newSession := session == ""
	if newSession {
		var err error
		if session, err = newUUID(); err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(ctx, d.Bin, d.Args(session, newSession)...)
	cmd.Dir = d.Dir
	cmd.Stdin = strings.NewReader(message)
	cmd.Env = append(os.Environ(), "SHEPHERD_CLIENT=desk")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return session, err
	}
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return session, err
	}
	got := Parse(out, emit)
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return session, fmt.Errorf("%v: %s", err, lastLine(msg))
		}
		if !got {
			return session, err
		}
	}
	return session, nil
}

// Parse reads Claude Code's stream-json and emits the thread's lines: the desk's text as
// it streams and then whole, a short line per tool call, and an error when the turn
// fails. It reports whether any reply came through.
func Parse(r io.Reader, emit func(Line)) bool {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	got := false
	for sc.Scan() {
		var m struct {
			Type    string  `json:"type"`
			Subtype string  `json:"subtype"`
			IsError bool    `json:"is_error"`
			Result  string  `json:"result"`
			Cost    float64 `json:"total_cost_usd"`
			Event   struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case "stream_event":
			if m.Event.Type == "content_block_delta" && m.Event.Delta.Type == "text_delta" && m.Event.Delta.Text != "" {
				emit(Line{Kind: KindPartial, Text: m.Event.Delta.Text})
			}
		case "assistant":
			for _, c := range m.Message.Content {
				switch c.Type {
				case "text":
					if t := strings.TrimSpace(c.Text); t != "" {
						emit(Line{Kind: KindDesk, Text: t})
						got = true
					}
				case "tool_use":
					// ToolSearch is Claude Code loading its own tool list: noise here.
					if c.Name != "ToolSearch" {
						emit(Line{Kind: KindTool, Text: toolLine(c.Name, c.Input)})
					}
					got = true
				}
			}
		case "result":
			if m.Cost > 0 {
				emit(Line{Kind: KindCost, Text: strconv.FormatFloat(m.Cost, 'f', -1, 64)})
			}
			if m.IsError || (m.Subtype != "" && m.Subtype != "success") {
				text := m.Result
				if text == "" {
					text = m.Subtype
				}
				emit(Line{Kind: KindError, Text: "the desk's turn failed: " + text})
			}
		}
	}
	return got
}

// toolLine is "lane_open feat/login (scope src/auth/**)"-style shorthand for a call.
func toolLine(name string, input json.RawMessage) string {
	name = strings.TrimPrefix(name, "mcp__shepherd__")
	var args map[string]any
	json.Unmarshal(input, &args)
	var parts []string
	for _, k := range []string{"repo", "name", "lane", "agent", "id", "path", "pattern", "file_path"} {
		if v, ok := args[k]; ok {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	if sc, ok := args["scope"].([]any); ok {
		var s []string
		for _, g := range sc {
			s = append(s, fmt.Sprint(g))
		}
		parts = append(parts, "scope "+strings.Join(s, ","))
	}
	for _, k := range []string{"task", "message", "answer"} {
		if v, ok := args[k].(string); ok {
			parts = append(parts, quoteShort(v, 70))
		}
	}
	return strings.TrimSpace(name + " " + strings.Join(parts, " "))
}

func quoteShort(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n-3] + "..."
	}
	return `"` + s + `"`
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
