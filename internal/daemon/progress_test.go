package daemon

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

func TestStepFromLog(t *testing.T) {
	wt := "/w/app-wt/x"
	for _, c := range []struct{ name, log, want string }{
		{"empty", "", ""},
		{"bash", "→ Bash {\"command\":\"make check\",\"description\":\"gate\"}\n", "running make check"},
		{"first line of a script", "→ Bash {\"command\":\"cd src \\u0026\\u0026 go test\\nrm x\"}\n", "running cd src && go test"},
		{"edit is relative", "→ Edit {\"file_path\":\"/w/app-wt/x/internal/x.go\",\"old_string\":\"a\"}\n", "editing internal/x.go"},
		{"write", "→ Write {\"file_path\":\"/elsewhere/y.go\"}\n", "editing /elsewhere/y.go"},
		{"read", "→ Read {\"file_path\":\"/w/app-wt/x/README.md\"}\n", "reading README.md"},
		{"grep", "→ Grep {\"pattern\":\"func Run\"}\n", "searching for func Run"},
		{"clipped input", "→ Bash {\"command\":\"go test ./internal/very/long/pa...\n", "running go test ./internal/very/long/pa..."},
		{"opencode lowercase", "→ bash {\"command\":\"ls\"}\n", "running ls"},
		{"mcp tool", "→ mcp__shepherd__report {\"status\":\"progress\"}\n", "using report"},
		{"other tool", "→ Weird {}\n", "using Weird"},
		{"plain text agents", "thinking about it\nrunning the tests now\n", "running the tests now"},
		{"timed tool line", "21:27:03 → Bash {\"command\":\"make check\"}\n", "running make check"},
		{"timed plain line", "21:27:03 running the tests now\n", "running the tests now"},
		{"old and timed mixed", "→ Edit {\"file_path\":\"/w/app-wt/x/a.go\"}\n21:27:09 → Bash {\"command\":\"go vet\"}\n", "running go vet"},
		{"timed end note is skipped", "21:27:03 → Bash {\"command\":\"ls\"}\n21:27:04 (the run ended)\n", "running ls"},
		{"the latest line wins", "→ Edit {\"file_path\":\"/w/app-wt/x/a.go\"}\n→ Bash {\"command\":\"go vet\"}\n", "running go vet"},
		{"secrets are redacted", "→ Bash {\"command\":\"curl -H 'Authorization: Bearer sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789'\"}\n", ""},
	} {
		got := StepFromLog(c.log, wt)
		if c.want == "" && c.name == "secrets are redacted" {
			if strings.Contains(got, "abcdefghijklmnop") {
				t.Errorf("%s: %q leaks a secret", c.name, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestProgressLine(t *testing.T) {
	for _, c := range []struct {
		step    string
		commits int
		d       time.Duration
		want    string
	}{
		{"running make check", 3, 12 * time.Minute, "running make check (3 commits, 12m)"},
		{"editing a.go", 1, 40 * time.Second, "editing a.go (1 commit, 40s)"},
		{"", 3, 12 * time.Minute, "3 commits so far, 12m"},
		{"", 0, 5 * time.Second, "just started, 5s"},
		{"", 1, 75 * time.Minute, "1 commit so far, 1h15m"},
	} {
		if got := ProgressLine(c.step, c.commits, c.d); got != c.want {
			t.Errorf("ProgressLine(%q, %d, %v) = %q, want %q", c.step, c.commits, c.d, got, c.want)
		}
	}
}

// A running run's view carries what its log last showed; an ended run's carries none.
func TestRunViewsCarryProgress(t *testing.T) {
	s, ts := newServer(t)
	a, b, la, _ := twoRuns(t, s)
	if err := os.WriteFile(a.Log, []byte("→ Bash {\"command\":\"make check\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.State = "succeeded"
	if err := s.Store.UpdateRun(t.Context(), b); err != nil {
		t.Fatal(err)
	}

	var one api.RunView
	if code := call(t, ts, s.Token, "GET", api.PathRun(a.ID), nil, &one); code != http.StatusOK || one.Progress == nil || one.Progress.Step != "running make check" {
		t.Fatalf("run view: %d %+v", code, one.Progress)
	}
	if !strings.HasPrefix(one.Progress.Line, "running make check (0 commits, ") {
		t.Errorf("line = %q", one.Progress.Line)
	}
	var list []api.RunView
	call(t, ts, s.Token, "GET", api.PathRuns, nil, &list)
	for _, r := range list {
		if (r.ID == a.ID) != (r.Progress != nil) {
			t.Errorf("listed run %d progress = %+v", r.ID, r.Progress)
		}
	}
	var lanes []api.LaneView
	call(t, ts, s.Token, "GET", api.PathLanes+"?workspace_id=1", nil, &lanes)
	for _, l := range lanes {
		if (l.ID == la.ID) != (l.Progress != nil) {
			t.Errorf("lane %s progress = %+v", l.Name, l.Progress)
		}
	}
}
