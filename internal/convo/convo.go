// Package convo adopts agent conversations a person already had outside Shepherd, so
// they can be listed, reopened, and asked questions from the one conversation.
//
// Today that is Claude Code's sessions, which it keeps as one JSON-lines file each under
// ~/.claude/projects. Shepherd reads only their metadata (session id, working directory,
// branches, times) and a short title, never changes them, and continues one only through
// Claude Code itself.
package convo

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/paths"
	"github.com/ubixsys/ubixshepherd/internal/redact"
)

// InUseWindow is how recently a session's file may have changed for Shepherd to treat
// it as open in someone's terminal, and refuse to continue it headless.
const InUseWindow = 5 * time.Minute

// Found is a conversation found on disk.
type Found struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent"`
	Dir      string    `json:"dir"`
	Title    string    `json:"title"`
	Branches []string  `json:"branches"`
	Started  time.Time `json:"started"`
	Last     time.Time `json:"last"`
	File     string    `json:"file"`
}

// ClaudeHome is where Claude Code keeps its sessions.
func ClaudeHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// Scan finds the Claude Code sessions under projects whose working directory is inside
// one of roots (a repo and its worktree directory, say).
func Scan(projects string, roots []string) ([]Found, error) {
	files, err := filepath.Glob(filepath.Join(projects, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	canon := make([]string, 0, len(roots))
	for _, r := range roots {
		if c, err := paths.Canonical(r); err == nil {
			canon = append(canon, c)
		}
	}
	var out []Found
	for _, f := range files {
		c, ok := read(f)
		if !ok || c.Dir == "" {
			continue
		}
		dir, _ := paths.Canonical(c.Dir)
		for _, r := range canon {
			if paths.Within(r, dir) {
				c.Dir = dir
				out = append(out, c)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out, nil
}

type record struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	GitBranch string `json:"gitBranch"`
	Timestamp string `json:"timestamp"`
	Summary   string `json:"summary"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// read takes one session file's metadata: the first working directory (where the session
// was started, and so where it resumes), its branches by use, its time span, and a title
// (Claude Code's summary, or the person's first message).
func read(file string) (Found, bool) {
	fh, err := os.Open(file)
	if err != nil {
		return Found{}, false
	}
	defer fh.Close()
	c := Found{Agent: "claude", File: file}
	branches := map[string]int{}
	firstSay := ""
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if c.ID == "" && r.SessionID != "" {
			c.ID = r.SessionID
		}
		if c.Dir == "" && r.Cwd != "" {
			c.Dir = r.Cwd
		}
		if r.GitBranch != "" {
			branches[r.GitBranch]++
		}
		if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
			if c.Started.IsZero() || t.Before(c.Started) {
				c.Started = t
			}
			if t.After(c.Last) {
				c.Last = t
			}
		}
		if r.Type == "summary" && r.Summary != "" {
			c.Title = r.Summary
		}
		if firstSay == "" && r.Type == "user" && r.Message.Role == "user" {
			firstSay = userText(r.Message.Content)
		}
	}
	if c.ID == "" {
		c.ID = strings.TrimSuffix(filepath.Base(file), ".jsonl")
	}
	if c.Title == "" {
		c.Title = firstSay
	}
	c.Title = redact.String(clip(c.Title, 100))
	type kv struct {
		b string
		n int
	}
	var bs []kv
	for b, n := range branches {
		bs = append(bs, kv{b, n})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].n > bs[j].n })
	for _, x := range bs {
		c.Branches = append(c.Branches, x.b)
	}
	return c, true
}

// userText is the text of a person's message: a plain string, or the text parts of a
// list; tool results and command wrappers are not what they said.
func userText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "<") {
			return ""
		}
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" && !strings.HasPrefix(strings.TrimSpace(p.Text), "<") {
				return strings.TrimSpace(p.Text)
			}
		}
	}
	return ""
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > n {
		return string([]rune(s)[:n-1]) + "…"
	}
	return s
}

// InUse reports whether a session file changed recently enough that it may be open.
func InUse(file string) bool {
	fi, err := os.Stat(file)
	return err == nil && time.Since(fi.ModTime()) < InUseWindow
}
