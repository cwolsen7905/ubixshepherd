package dispatch

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// A diagnosis says why a run, or the push of its work, went wrong, from what Shepherd
// saw: exit codes, its own timeouts, the adapters' limit words, the gate's output. No
// model is asked. See docs/run-failures.md for the kinds.

const (
	// evidenceLines and evidenceBytes cap the output kept with a diagnosis.
	evidenceLines = 40
	evidenceBytes = 4096
	// ringLines is how much of a run's output the watcher keeps to diagnose from.
	ringLines = 80
)

var (
	// pushRefusedWords matches what git prints when a push does not go through, which
	// includes what Shepherd's pre-push hook prints.
	pushRefusedWords = regexp.MustCompile(`(?i)push refused for lane|\[remote rejected\]|failed to push some refs|pre-push hook|pre-receive hook declined|shepherd-run-blocks-push`)
	// capWords matches an agent running out of context, turns or its own budget.
	capWords = regexp.MustCompile(`(?i)the run ended with (error_max\w+)|prompt is too long|context (window|length|limit)[^.\n]*(exceeded|reached|full)|exceeds? the (maximum )?context|maximum context length`)
	// failingWords picks the line that says what failed.
	failingWords = regexp.MustCompile(`(?i)\b(fail(ed|ure|s)?|error|panic|fatal|denied|refused|cannot|not found)\b|^--- FAIL|^not ok`)
	// fileRefs picks file references (path/to/file.go:12) out of failing output.
	fileRefs = regexp.MustCompile(`(?:^|[\s(])((?:[\w.\-]+/)*[\w.\-]+\.[A-Za-z]{1,5}):\d+`)
	// standardsHead is how a failed standards check begins (see standardsChecks).
	standardsHead = regexp.MustCompile("(?m)^standard (\\S+) \\((\\w+)\\) failed: `(.*)`$")
)

// endInfo is what the watcher knows of a run that has ended.
type endInfo struct {
	Run     store.Run
	Agent   string
	Stopped bool
	Idled   bool
	Closing bool
	Idle    time.Duration
	// Err is the process error, if any, and Why Shepherd's own reason for failing a
	// run that exited 0 (it changed nothing).
	Err      string
	NoChange bool
	Limit    Limit
	// QuotaUntil is when the agent's hold lifts, when the run ended out of quota.
	QuotaUntil time.Time
	// Tail is the end of the agent's output as it was logged: scrubbed, not yet capped.
	Tail string
}

// classifyEnd names why a run that ended did not end well. ok is false for a run that
// succeeded with nothing to flag.
func classifyEnd(in endInfo) (d store.Diagnosis, ok bool) {
	run := in.Run
	d = store.Diagnosis{RunID: run.ID, Log: run.Log, ExitCode: run.ExitCode, Evidence: redact.Tail(in.Tail, evidenceLines, evidenceBytes)}
	switch {
	case in.Stopped:
		d.Kind, d.Summary = store.DiagStopped, "stopped before it finished, on request"
	case in.Idled:
		d.Kind = store.DiagHung
		d.Summary = fmt.Sprintf("hung: printed nothing for %s, so Shepherd stopped it", in.Idle)
	case run.Reason == store.ReasonRestart:
		d.Kind, d.Summary = store.DiagInterrupted, "Shepherd restarted while this run was going; it is continued after the restart"
	case in.Closing:
		d.Kind, d.Summary = store.DiagInterrupted, "the daemon stopped while this run was going"
	case run.State == store.RunFailed && !in.QuotaUntil.IsZero():
		d.Kind = store.DiagQuota
		d.ResetAt = &in.QuotaUntil
		d.Summary = fmt.Sprintf("%s is out of quota (%s); held until %s", in.Agent, clip(in.Limit.Text, 120), in.QuotaUntil.Local().Format("Jan 2 15:04"))
	case in.NoChange:
		d.Kind = store.DiagNoChange
		d.Summary = fmt.Sprintf("%s exited 0 but changed nothing: no commit, no file change and nothing asked or reported; see the run log", in.Agent)
	case run.State == store.RunFailed:
		d = classifyFailure(in, d)
	case len(run.Outside) > 0:
		d.Kind, d.Files = store.DiagOutsideScope, run.Outside
		d.Summary = fmt.Sprintf("the run changed files outside its lane's scope: %s", clip(strings.Join(run.Outside, ", "), 160))
	default:
		return d, false
	}
	return d, true
}

// classifyFailure reads a run that failed on its own: a cap, a refused push, or an
// agent error.
func classifyFailure(in endInfo, d store.Diagnosis) store.Diagnosis {
	code := "no exit code"
	if d.ExitCode != nil {
		code = fmt.Sprintf("exit %d", *d.ExitCode)
	}
	last := failingLine(in.Tail)
	switch m := capWords.FindStringSubmatch(in.Tail); {
	case m != nil:
		d.Kind = store.DiagBudgetCap
		what := "ran out of context"
		switch {
		case strings.Contains(m[1], "turns"):
			what = "reached its turn limit"
		case strings.Contains(m[1], "budget"):
			what = "reached its spend cap"
		}
		d.Summary = fmt.Sprintf("%s %s (%s)", in.Agent, what, code)
	case pushRefusedWords.MatchString(in.Tail) && len(in.Run.Outside) == 0 && !pushOnlyBlocked(in.Tail):
		d.Kind, d.Summary = pushKind(in.Tail, in.Run), "a push was refused: "+clip(last, 160)
		if d.Kind == store.DiagOutsideScope {
			d.Files = in.Run.Outside
		}
	case in.Err != "" && d.ExitCode != nil && *d.ExitCode == 0:
		d.Kind, d.Summary = store.DiagAgentError, in.Agent+" failed: "+clip(in.Err, 160)
	case in.Err != "" || (d.ExitCode != nil && *d.ExitCode != 0):
		d.Kind = store.DiagAgentError
		d.Summary = fmt.Sprintf("%s failed (%s)", in.Agent, code)
		if last != "" {
			d.Summary += ": " + clip(last, 160)
		}
	default:
		d.Kind, d.Summary = store.DiagUnknown, fmt.Sprintf("%s failed (%s) and Shepherd could not tell why; see the run log", in.Agent, code)
	}
	if d.Kind == store.DiagPushRefused || d.Kind == store.DiagOutsideScope || d.Kind == store.DiagCommitsForbidden {
		// the refusal's own words are the best summary
		d.Summary = "a push was refused: " + clip(last, 160)
	}
	return d
}

// pushOnlyBlocked is true when the only push word in the output is Shepherd's own block
// on an agent's push (a run's output often mentions it without the run failing on it).
func pushOnlyBlocked(tail string) bool {
	return strings.Contains(tail, "shepherd-run-blocks-push") && len(pushRefusedWords.FindAllString(tail, -1)) == strings.Count(tail, "shepherd-run-blocks-push")
}

// pushKind says which rule a refused push broke, from the hook's words.
func pushKind(tail string, run store.Run) string {
	switch {
	case strings.Contains(tail, "outside lane") || len(run.Outside) > 0:
		return store.DiagOutsideScope
	case strings.Contains(tail, "does not allow"):
		return store.DiagCommitsForbidden
	}
	return store.DiagPushRefused
}

// failingLine is the line of out that most likely says what went wrong: the last that
// mentions a failure, else the last non-empty one.
func failingLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "# shepherd:") && failingWords.MatchString(l) {
			return l
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "# shepherd:") {
			return l
		}
	}
	return ""
}

// filesIn lists the files a command's failing output points at, at most n.
func filesIn(out string, n int) []string {
	var files []string
	seen := map[string]bool{}
	for _, m := range fileRefs.FindAllStringSubmatch(out, -1) {
		if f := m[1]; !seen[f] && !strings.Contains(f, "://") {
			seen[f] = true
			files = append(files, f)
			if len(files) == n {
				break
			}
		}
	}
	return files
}

// commandFailure is the diagnosis of a command Shepherd ran itself (the gate, a
// standards check) that failed.
func commandFailure(kind, what, command string, code *int, tail, log string) store.Diagnosis {
	d := store.Diagnosis{Kind: kind, Command: command, ExitCode: code, Log: log,
		Evidence: redact.Tail(tail, evidenceLines, evidenceBytes), Files: filesIn(tail, 10)}
	exit := ""
	if code != nil {
		exit = fmt.Sprintf(" (exit %d)", *code)
	}
	d.Summary = what + " failed" + exit
	if l := failingLine(tail); l != "" {
		d.Summary += ": " + clip(l, 160)
	}
	return d
}

// diagnoseStandards reads the first blocking standards check out of the text
// standardsChecks returns.
func diagnoseStandards(tail string) store.Diagnosis {
	d := commandFailure(store.DiagStandardsFailed, "a standards check", "", nil, tail, "")
	if m := standardsHead.FindStringSubmatch(tail); m != nil {
		d.Command = m[3]
		d.Summary = fmt.Sprintf("standards rule %s (%s) failed: `%s`", m[1], m[2], clip(m[3], 100))
		if rest := strings.TrimSpace(tail[strings.Index(tail, m[0])+len(m[0]):]); rest != "" {
			// the check's own first line of output, not a later one
			if l, _, _ := strings.Cut(rest, "\n"); strings.TrimSpace(l) != "" {
				d.Summary += ": " + clip(l, 120)
			}
		}
	}
	return d
}

// fixesFor offers what can be done about a diagnosis. Each fix is an existing operation
// (see ApplyFix). NeedsPerson marks what only the person chooses; DeskOK what the front
// desk may take on work the person asked for.
func fixesFor(d store.Diagnosis, run store.Run) []store.Fix {
	canContinue := run.Session != "" && run.ID != 0
	var fixes []store.Fix
	cont := func(label, lead string) {
		if !canContinue {
			return
		}
		fixes = append(fixes, store.Fix{ID: "continue", Label: label, Op: store.FixContinue, DeskOK: true,
			Args: map[string]string{"prompt": followUp(lead, d)}})
	}
	retry := func(desk bool) {
		if run.Prompt == "" {
			return
		}
		fixes = append(fixes, store.Fix{ID: "retry", Label: "Retry in a new session", Op: store.FixRetry, DeskOK: desk, NeedsPerson: !desk})
	}
	other := func() {
		fixes = append(fixes, store.Fix{ID: "retry_other", Label: "Retry with another agent or model", Op: store.FixRetryOther,
			Args: map[string]string{"agent": "", "model": ""}})
	}
	person := func(id, label, op string, args map[string]string) {
		fixes = append(fixes, store.Fix{ID: id, Label: label, Op: op, Args: args, NeedsPerson: true})
	}
	switch d.Kind {
	case store.DiagStartFailed:
		retry(true)
		other()
	case store.DiagAgentError:
		cont("Continue the run and fix the error", "Your last run failed.")
		retry(true)
		other()
	case store.DiagHung:
		cont("Continue the run", "Your last run stopped printing and was stopped by Shepherd.")
		retry(true)
		other()
	case store.DiagQuota:
		fixes = append(fixes, store.Fix{ID: "wait_quota", Label: "Wait for the quota to reset", Op: store.FixWaitQuota, DeskOK: true})
		other()
	case store.DiagNoChange:
		cont("Continue and ask it to do the work or say why not", "Your last run exited without changing anything.")
		retry(true)
		other()
	case store.DiagGateFailed:
		cont("Hand the failure back to the agent", "Shepherd ran the repo's gate and it failed.")
		person("ship", "Run the gate and push again", store.FixShip, nil)
	case store.DiagStandardsFailed:
		cont("Hand the failed check back to the agent", "Shepherd ran the repo's standards checks and one failed.")
		person("ship", "Run the checks and push again", store.FixShip, nil)
	case store.DiagCommitsForbidden:
		cont("Ask the agent to amend the commit messages", "A commit message in your lane is not allowed by the repo's rules.")
		person("ship", "Push again after amending", store.FixShip, nil)
	case store.DiagOutsideScope:
		cont("Ask the agent to move or drop the out-of-scope changes", "Your commits change files outside the lane's scope.")
		person("widen_scope", "Widen the lane's scope to include these files", store.FixWidenScope, map[string]string{"paths": strings.Join(d.Files, "\n")})
	case store.DiagPushRefused:
		cont("Hand the refusal back to the agent", "A push of your lane was refused.")
		person("ship", "Push again", store.FixShip, nil)
	case store.DiagMergeRefused:
		person("ship", "Push and open the merge request again", store.FixShip, nil)
		person("open_mr", "Open the merge request yourself", store.FixOpenMR, nil)
	case store.DiagStopped:
		fixes = append(fixes, store.Fix{ID: "continue", Label: "Continue the run", Op: store.FixContinue, NeedsPerson: true,
			Args: map[string]string{"prompt": followUp("Your last run was stopped before it finished; carry on from where it stopped.", d)}})
		retry(false)
	case store.DiagInterrupted:
		cont("Continue the run", "Your last run was cut short by the daemon stopping.")
		retry(true)
	case store.DiagBudgetCap:
		person("raise_budget", "Raise or lift the cap", store.FixRaiseBudget, nil)
		retry(false)
		other()
	default:
		cont("Continue the run", "Your last run failed and Shepherd could not tell why.")
		retry(true)
		other()
	}
	if d.Log != "" {
		fixes = append(fixes, store.Fix{ID: "open_log", Label: "Open the log", Op: store.FixOpenLog, DeskOK: true, Args: map[string]string{"path": d.Log}})
	}
	return fixes
}

// followUp is the prompt that continues a run with what went wrong.
func followUp(lead string, d store.Diagnosis) string {
	var b strings.Builder
	b.WriteString("[Shepherd] " + lead + "\n\n" + d.Summary + "\n")
	if d.Command != "" {
		fmt.Fprintf(&b, "\nCommand: %s\n", d.Command)
	}
	if d.Evidence != "" {
		b.WriteString("\nIts output ended:\n\n" + d.Evidence + "\n")
	}
	b.WriteString("\nFind the cause, fix it within your scope, check it yourself and commit.")
	return b.String()
}

// diagnose records a diagnosis of a run with its suggested fixes. A diagnosis that
// cannot be written is logged: it never fails the work it describes.
func (r *Runner) diagnose(ctx context.Context, run store.Run, d store.Diagnosis) store.Diagnosis {
	d.RunID = run.ID
	d.Summary = redact.String(d.Summary)
	d.Evidence = redact.Tail(d.Evidence, evidenceLines, evidenceBytes)
	d.Command = redact.String(d.Command)
	d.Fixes = fixesFor(d, run)
	saved, err := r.Store.AddDiagnosis(ctx, d)
	if err != nil {
		r.Log.Error("record run diagnosis", "run", run.ID, "kind", d.Kind, "err", err)
		return d
	}
	return saved
}

// diagnoseLane records a diagnosis against the lane's newest run, the one whose work
// Shepherd is checking or pushing. It does nothing for a lane that never ran.
func (r *Runner) diagnoseLane(ctx context.Context, lane store.Lane, d store.Diagnosis) {
	runs, err := r.Store.Runs(ctx, lane.ID, "", 1)
	if err != nil || len(runs) == 0 {
		return
	}
	r.diagnose(ctx, runs[0], d)
}

// tailFile is the end of a file, as lines, for evidence.
func tailFile(path string) string {
	b, err := ReadLogEnd(path, 16<<10)
	if err != nil {
		return ""
	}
	return redact.Tail(string(b), evidenceLines, evidenceBytes)
}

// ReadLogEnd returns up to max bytes from the end of a file.
func ReadLogEnd(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := max64(st.Size()-max, 0)
	buf := make([]byte, st.Size()-off)
	n, err := f.ReadAt(buf, off)
	if n == len(buf) {
		err = nil
	}
	return buf[:n], err
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
