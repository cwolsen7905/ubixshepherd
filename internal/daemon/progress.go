package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/dispatch"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// progressTail is how much of the end of a run's log is read to find its latest step.
const progressTail = 16 << 10

// progressTTL is how long a run's progress is reused: a board and a lane page asking in
// the same moment cost one log read and one git call.
const progressTTL = 2 * time.Second

// progressCache remembers each running run's progress for progressTTL.
type progressCache struct {
	mu sync.Mutex
	by map[int64]cachedProgress
}

type cachedProgress struct {
	at time.Time
	p  api.Progress
}

// RunProgress says what a running run is doing now: the latest tool step in its log,
// its commits so far and how long it has run. It is derived from the log and the lane's
// worktree each time (briefly cached), so nothing is stored and a client such as the
// chat dock can ask for it the same way. A run that is not running has none.
func (s *Server) RunProgress(ctx context.Context, run store.Run, worktree string) *api.Progress {
	if run.State != store.RunRunning {
		return nil
	}
	now := time.Now()
	s.progress.mu.Lock()
	if c, ok := s.progress.by[run.ID]; ok && now.Sub(c.at) < progressTTL {
		s.progress.mu.Unlock()
		p := c.p
		p.Elapsed = int64(now.Sub(run.Started).Seconds())
		p.Line = ProgressLine(p.Step, p.Commits, now.Sub(run.Started))
		return &p
	}
	s.progress.mu.Unlock()

	p := api.Progress{Elapsed: int64(now.Sub(run.Started).Seconds())}
	if tail, err := dispatch.ReadLogEnd(run.Log, progressTail); err == nil {
		p.Step = StepFromLog(string(tail), worktree)
	}
	if worktree != "" && run.StartSHA != "" {
		if out, err := git.Run(ctx, worktree, "rev-list", "--count", run.StartSHA+"..HEAD"); err == nil {
			p.Commits, _ = strconv.Atoi(strings.TrimSpace(out))
		}
	}
	p.Line = ProgressLine(p.Step, p.Commits, now.Sub(run.Started))

	s.progress.mu.Lock()
	if s.progress.by == nil {
		s.progress.by = map[int64]cachedProgress{}
	}
	for id, c := range s.progress.by {
		if now.Sub(c.at) > time.Minute {
			delete(s.progress.by, id)
		}
	}
	s.progress.by[run.ID] = cachedProgress{at: now, p: p}
	s.progress.mu.Unlock()
	return &p
}

// ProgressLine is the one-line phrase: the step with its commits and time in brackets,
// or, when the log shows no step yet, the commits and time alone.
func ProgressLine(step string, commits int, elapsed time.Duration) string {
	stats := fmt.Sprintf("%s, %s", plural(commits, "commit"), elapsedText(elapsed))
	if step == "" {
		if commits == 0 {
			return "just started, " + elapsedText(elapsed)
		}
		return plural(commits, "commit") + " so far, " + elapsedText(elapsed)
	}
	return step + " (" + stats + ")"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func elapsedText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

var toolInputKey = func(key string) *regexp.Regexp {
	// A value may be cut short by the log's clipping, so the closing quote is optional.
	return regexp.MustCompile(`"` + key + `"\s*:\s*"((?:[^"\\]|\\.)*)`)
}

var (
	inputCommand = toolInputKey("command")
	inputPath    = regexp.MustCompile(`"(?:file_path|filePath|path|notebook_path)"\s*:\s*"((?:[^"\\]|\\.)*)`)
	inputPattern = regexp.MustCompile(`"(?:pattern|query|url)"\s*:\s*"((?:[^"\\]|\\.)*)`)
)

// StepFromLog reads the latest step a run's log shows: the last tool call the agent
// made ("running make check", "editing internal/x.go"), or the last line of plain
// output for an agent whose log has no tool lines. It is redacted and one line, and ""
// when the log is empty. worktree is stripped from paths.
func StepFromLog(tail, worktree string) string {
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		step := stepOf(l, worktree)
		if step == "" {
			continue
		}
		return clipRunes(redact.String(step), 90)
	}
	return ""
}

func stepOf(line, worktree string) string {
	rest, ok := strings.CutPrefix(line, "→ ")
	if !ok {
		if strings.HasPrefix(line, "(the run ended") {
			return ""
		}
		return strings.Join(strings.Fields(line), " ")
	}
	name, input, _ := strings.Cut(rest, " ")
	rel := func(p string) string {
		p = unescape(p)
		if root := strings.TrimRight(worktree, "/"); root != "" {
			if r, ok := strings.CutPrefix(p, root+"/"); ok {
				return r
			}
		}
		return p
	}
	first := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(input); m != nil {
			return unescape(m[1])
		}
		return ""
	}
	switch strings.ToLower(name) {
	case "bash", "shell", "run_shell_command":
		if c := first(inputCommand); c != "" {
			return "running " + firstLine(c)
		}
		return "running a command"
	case "edit", "multiedit", "write", "patch", "apply_patch", "notebookedit":
		if p := inputPath.FindStringSubmatch(input); p != nil {
			return "editing " + rel(p[1])
		}
		return "editing files"
	case "read":
		if p := inputPath.FindStringSubmatch(input); p != nil {
			return "reading " + rel(p[1])
		}
		return "reading files"
	case "grep", "glob", "list", "ls", "websearch":
		if p := first(inputPattern); p != "" {
			return "searching for " + p
		}
		return "searching the code"
	case "webfetch":
		if p := first(inputPattern); p != "" {
			return "fetching " + p
		}
		return "fetching a page"
	case "task", "agent":
		return "working with a subagent"
	case "todowrite":
		return "planning its steps"
	}
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.Split(name, "__")
		return "using " + parts[len(parts)-1]
	}
	return "using " + name
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, `\n`)
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}

func unescape(s string) string {
	if u, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return u
	}
	return strings.ReplaceAll(s, `\"`, `"`)
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
