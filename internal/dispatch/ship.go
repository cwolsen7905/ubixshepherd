package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/crook"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// MaxGateTries is how many gate failures Shepherd hands back to the agent before a push,
// before leaving the lane to the person.
const MaxGateTries = 2

// GateTimeout bounds one gate run.
const GateTimeout = 20 * time.Minute

// ship pushes a lane and opens its merge request after an agent's run, for repos whose
// profile says Shepherd pushes. Shepherd runs the gate itself first, in the lane; the
// agent's word that it passed is not enough. It never merges.
//
// It ships the lane's unpushed commits, not only the run's: a run that continues one
// held back (by a question, a gate fix) may add none of its own.
func (r *Runner) ship(ctx context.Context, run store.Run, lane store.Lane) {
	if run.State != store.RunSucceeded {
		return
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return
	}
	prof := r.Conf().Profile(repo.Name)
	if !shipVerdict(ctx, repo, lane, prof).OK() {
		return
	}
	r.pushTags(ctx, lane, repo, prof)
	if n, err := git.Run(ctx, lane.Worktree, "rev-list", "--count", pushedRef(ctx, lane)+"..HEAD"); err != nil || n == "0" {
		return
	}
	if len(run.Outside) > 0 {
		r.feed(ctx, store.FeedGate, lane.ID, "not pushing lane %s: run %d changed files outside its scope (%s)", lane.Name, run.ID, strings.Join(run.Outside, ", "))
		r.diagnose(ctx, run, outsideDiagnosis(run.Outside))
		return
	}
	if outside := r.outsideScope(ctx, lane); len(outside) > 0 {
		r.feed(ctx, store.FeedGate, lane.ID, "not pushing lane %s: its commits change files outside its scope (%s)", lane.Name, strings.Join(outside, ", "))
		r.diagnose(ctx, run, outsideDiagnosis(outside))
		return
	}
	if problems := r.checkProblems(ctx, lane); len(problems) > 0 {
		r.feed(ctx, store.FeedGate, lane.ID, "not pushing lane %s: %s", lane.Name, problems[0])
		r.diagnose(ctx, run, checksDiagnosis(problems))
		return
	}
	if why, busy := r.unfinished(ctx, run, lane); why != "" {
		r.Log.Info("not shipping yet", "lane", lane.Name, "run", run.ID, "why", why)
		if !busy {
			r.feed(ctx, store.FeedGate, lane.ID, "not pushing lane %s yet: run %d %s; it ships when that is settled", lane.Name, run.ID, why)
		}
		return
	}
	lf, err := r.Store.LaneForge(ctx, lane.ID)
	if err != nil {
		return
	}

	if bad := forbidden(ctx, lane, prof.Forbid); bad != "" {
		r.gateFailed(ctx, run, lane, "the repo's commit rules", bad+"\nAmend your unpushed commits (git commit --amend, or git rebase) so no commit message matches.", "", &lf, forbiddenDiagnosis(bad))
		return
	}
	ok, tail, logPath, code := r.gate(ctx, lane, prof.Gate, fmt.Sprintf("%d.gate.log", run.ID))
	if !ok {
		r.gateFailed(ctx, run, lane, "the repo's gate, `"+prof.Gate+"`,", tail, logPath, &lf, commandFailure(store.DiagGateFailed, "the gate", prof.Gate, &code, tail, logPath))
		return
	}
	if blocked, tail := r.standardsChecks(ctx, lane, prof); blocked {
		r.gateFailed(ctx, run, lane, "the repo's standards checks", tail, "", &lf, diagnoseStandards(tail))
		return
	}
	lf.GateTries = 0
	r.Store.PutLaneForge(ctx, lf)
	r.publish(ctx, lane, repo, prof.Gate, fmt.Sprintf("run %d", run.ID))
}

// Shipped is what an explicit ship did.
type Shipped struct {
	Lane    string `json:"lane"`
	Commits int    `json:"commits"`
	Pushed  bool   `json:"pushed"`
	MR      int    `json:"mr,omitempty"`
	URL     string `json:"url,omitempty"`
	// Message says what happened, as the feed says it.
	Message string `json:"message"`
}

// Ship pushes a lane's existing unpushed commits and opens or updates its merge request:
// the path a run takes when it ends, for work that is already committed. It checks the
// same things first (the scope, the repo's commit rules, Shepherd's own gate run) and
// refuses, saying why, rather than hand anything to an agent. Only for repos whose
// profile says Shepherd pushes; it never merges.
func (r *Runner) Ship(ctx context.Context, laneID int64) (Shipped, error) {
	lane, err := r.Store.Lane(ctx, laneID)
	if err != nil {
		return Shipped{}, err
	}
	out := Shipped{Lane: lane.Name}
	if lane.State != store.LaneOpen {
		return out, refuse("lane %s is %s, not open", lane.Name, lane.State)
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return out, err
	}
	prof := r.Conf().Profile(repo.Name)
	if v := shipVerdict(ctx, repo, lane, prof); !v.OK() {
		return out, refusal{msg: v.Reason, rule: v.Rule}
	}
	tagged := r.pushTags(ctx, lane, repo, prof)
	if prof.Gate == "" {
		return out, refuse("%s has no gate: Shepherd pushes only after running the repo's gate itself", repo.Name)
	}
	if busy, _ := r.Store.Runs(ctx, lane.ID, store.RunRunning, 1); len(busy) > 0 {
		return out, refuse("run %d is going in lane %s; ship when it ends", busy[0].ID, lane.Name)
	}
	if dirty, err := git.Dirty(ctx, lane.Worktree); err != nil {
		return out, err
	} else if dirty != "" {
		return out, refuse("lane %s has uncommitted changes; commit or discard them first, so the gate checks what is pushed", lane.Name)
	}
	pushed := pushedRef(ctx, lane)
	n, err := git.Run(ctx, lane.Worktree, "rev-list", "--count", pushed+"..HEAD")
	if err != nil {
		return out, err
	}
	fmt.Sscan(n, &out.Commits)
	if out.Commits == 0 {
		if len(tagged) > 0 {
			out.Message = strings.Join(tagged, "; ")
			return out, nil
		}
		return out, refuse("lane %s has no commits beyond %s: nothing to ship", lane.Name, pushed)
	}
	if outside := r.outsideScope(ctx, lane); len(outside) > 0 {
		r.diagnoseLane(ctx, lane, outsideDiagnosis(outside))
		return out, refuse("not shipping lane %s: its commits change files outside its scope (%s)", lane.Name, strings.Join(outside, ", "))
	}
	if problems := r.checkProblems(ctx, lane); len(problems) > 0 {
		r.diagnoseLane(ctx, lane, checksDiagnosis(problems))
		return out, refuse("not shipping lane %s: %s", lane.Name, strings.Join(problems, "\n"))
	}
	if bad := forbidden(ctx, lane, prof.Forbid); bad != "" {
		r.diagnoseLane(ctx, lane, forbiddenDiagnosis(bad))
		return out, refuse("not shipping lane %s: %s", lane.Name, bad)
	}
	if ok, tail, logPath, code := r.gate(ctx, lane, prof.Gate, fmt.Sprintf("lane-%d.gate.log", lane.ID)); !ok {
		r.diagnoseLane(ctx, lane, commandFailure(store.DiagGateFailed, "the gate", prof.Gate, &code, tail, logPath))
		r.feed(ctx, store.FeedGate, lane.ID, "the repo's gate, `%s`, failed in lane %s; not pushing (%s)", prof.Gate, lane.Name, logPath)
		return out, refuse("the repo's gate, `%s`, failed in lane %s; not pushing. The whole output is in %s; it ended:\n%s", prof.Gate, lane.Name, logPath, tail)
	}
	if blocked, tail := r.standardsChecks(ctx, lane, prof); blocked {
		r.diagnoseLane(ctx, lane, diagnoseStandards(tail))
		return out, refuse("not shipping lane %s: %s", lane.Name, tail)
	}
	res := r.publish(ctx, lane, repo, prof.Gate, "shepherd lane ship")
	res.Lane, res.Commits = out.Lane, out.Commits
	return res, nil
}

// pushedRef is what the lane's unpushed commits are counted from: its branch on origin,
// or else its base there, or else the local base.
func pushedRef(ctx context.Context, lane store.Lane) string {
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Branch) {
		return "origin/" + lane.Branch
	}
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Base) {
		return "origin/" + lane.Base
	}
	return lane.Base
}

// outsideScope lists the files the lane's branch changes, since it left its base, that
// its scope does not cover.
func (r *Runner) outsideScope(ctx context.Context, lane store.Lane) []string {
	var outside []string
	for _, f := range changedFiles(ctx, lane) {
		if !scope.Any(lane.Scope, f) {
			outside = append(outside, f)
		}
	}
	return outside
}

// baseRef is what the lane's changes are counted from: its base on origin if there.
func baseRef(ctx context.Context, lane store.Lane) string {
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Base) {
		return "origin/" + lane.Base
	}
	return lane.Base
}

// changedFiles lists the files the lane's branch changes since it left its base.
func changedFiles(ctx context.Context, lane store.Lane) []string {
	files, err := git.Run(ctx, lane.Worktree, "diff", "--name-only", "--no-renames", baseRef(ctx, lane)+"...HEAD")
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range strings.Split(files, "\n") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// publish pushes a lane whose gate has passed and opens its merge request, or says the
// open one is updated, telling the feed either way. from says what shipped it, for the
// request's description ("run 12", "shepherd lane ship").
func (r *Runner) publish(ctx context.Context, lane store.Lane, repo store.Repo, gate, from string) Shipped {
	out := Shipped{Lane: lane.Name}
	say := func(kind, format string, a ...any) Shipped {
		out.Message = fmt.Sprintf(format, a...)
		r.feed(ctx, kind, lane.ID, "%s", out.Message)
		return out
	}
	if _, err := git.Run(ctx, lane.Worktree, "push", "--quiet", "-u", "origin", lane.Branch); err != nil {
		r.diagnoseLane(ctx, lane, store.Diagnosis{Kind: store.DiagPushRefused, Evidence: err.Error(),
			Summary: "the gate passed, but the push failed: " + clip(err.Error(), 160)})
		return say(store.FeedGate, "gate passed in lane %s, but the push failed: %s", lane.Name, clip(err.Error(), 300))
	}
	out.Pushed = true
	f, err := r.forgeFor(repo.Remote)
	if err != nil {
		return say(store.FeedGate, "pushed lane %s (gate passed); open the merge request yourself: %v", lane.Name, err)
	}
	mr, err := f.MRForBranch(ctx, lane.Branch)
	if err != nil {
		return say(store.FeedGate, "pushed lane %s (gate passed); could not look up its merge request: %s", lane.Name, clip(err.Error(), 200))
	}
	if mr != nil && mr.State == "opened" {
		out.MR, out.URL = mr.IID, mr.URL
		return say(store.FeedMR, "pushed lane %s (gate `%s` passed); !%d updated: %s", lane.Name, gate, mr.IID, mr.URL)
	}
	title, body := r.mrText(ctx, lane, gate, from)
	mr, err = f.CreateMR(ctx, lane.Branch, lane.Base, title, body)
	if err != nil {
		r.diagnoseLane(ctx, lane, store.Diagnosis{Kind: store.DiagMergeRefused, Evidence: err.Error(),
			Summary: "pushed, but the forge refused the merge request: " + clip(err.Error(), 160)})
		return say(store.FeedGate, "pushed lane %s (gate passed), but opening the merge request failed: %s", lane.Name, clip(err.Error(), 300))
	}
	r.Log.Info("lane shipped", "lane", lane.Name, "mr", mr.IID)
	out.MR, out.URL = mr.IID, mr.URL
	return say(store.FeedMR, "pushed lane %s (gate `%s` passed) and opened !%d for you to review: %s", lane.Name, gate, mr.IID, mr.URL)
}

// forbidden checks the lane's unpushed commit messages against the repo's patterns and
// describes the first match, or returns "".
func forbidden(ctx context.Context, lane store.Lane, patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	base := lane.Base
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+lane.Branch) {
		base = "origin/" + lane.Branch
	} else if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	}
	out, err := git.Run(ctx, lane.Worktree, "log", "--format=%h%x00%B%x01", base+"..HEAD")
	if err != nil {
		return ""
	}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		for _, c := range strings.Split(out, "\x01") {
			sha, msg, _ := strings.Cut(strings.TrimSpace(c), "\x00")
			if m := re.FindString(msg); m != "" {
				return fmt.Sprintf("Commit %s's message contains %q, which this repo does not allow (pattern %s).", sha, m, p)
			}
		}
	}
	return ""
}

// unfinished says why a run's work is not ready to push, or "": it is waiting on the
// person or on the answer to a question, it said it was blocked, or the lane has moved
// on to another run (busy, which the feed need not hear about). Handoffs and reviews do
// not hold it: they are work for others, or about work already done.
func (r *Runner) unfinished(ctx context.Context, run store.Run, lane store.Lane) (why string, busy bool) {
	if going, _ := r.Store.Runs(ctx, lane.ID, store.RunRunning, 1); len(going) > 0 {
		return "another run is going in the lane", true
	}
	if ds, err := r.Store.Decisions(ctx, store.DecisionOpen); err == nil {
		for _, d := range ds {
			if d.RunID == run.ID {
				return fmt.Sprintf("waits on your decision %d", d.ID), false
			}
		}
	}
	if qs, err := r.Store.Requests(ctx, store.RequestPending, store.RequestNeedsRouting, store.RequestRouted, store.RequestReplyReady); err == nil {
		for _, q := range qs {
			if q.FromRun == run.ID && q.Kind == KindQuestion {
				return fmt.Sprintf("waits on the answer to its question, request %d (%s)", q.ID, q.State), false
			}
		}
	}
	if events, err := r.Store.Events(ctx, run.ID); err == nil {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Kind == EventReport && events[i].Status != "progress" {
				if events[i].Status == "blocked" {
					return "reported blocked", false
				}
				break
			}
		}
	}
	return "", false
}

func (r *Runner) forgeFor(remote string) (forge.Forge, error) {
	if r.ForgeFor != nil {
		return r.ForgeFor(remote)
	}
	return forge.For(remote)
}

// gate runs the repo's gate in the lane's worktree. It reports whether it passed, the
// end of its output, and where the whole output is (logName, in the runs directory).
func (r *Runner) gate(ctx context.Context, lane store.Lane, gate, logName string) (ok bool, tail, logPath string, code int) {
	ctx, cancel := context.WithTimeout(ctx, GateTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", gate)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", gate)
	}
	cmd.Dir = lane.Worktree
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	r.feed(ctx, store.FeedGate, lane.ID, "running the gate `%s` in lane %s before pushing", gate, lane.Name)
	head, clean := gateTarget(ctx, lane)
	err := cmd.Run()
	if err != nil {
		code = -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
	}
	text := redact.String(out.String())
	logPath = filepath.Join(r.Dir, logName)
	os.MkdirAll(r.Dir, 0o700)
	os.WriteFile(logPath, []byte(text), 0o600)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	tail = strings.Join(lines, "\n")
	timedOut := ctx.Err() != nil
	if clean {
		// The record outlives the run's own context: a cancelled run still ran the gate.
		r.Store.AddGateResult(context.WithoutCancel(ctx), store.GateResult{LaneID: lane.ID, SHA: head, Gate: redact.String(gate),
			Passed: err == nil && !timedOut, Source: store.GateSourceShip})
	}
	if timedOut {
		return false, tail + "\n(the gate timed out after " + GateTimeout.String() + ")", logPath, code
	}
	if err == nil {
		r.feed(ctx, store.FeedGate, lane.ID, "gate `%s` passed in lane %s", gate, lane.Name)
	}
	return err == nil, tail, logPath, code
}

// gateTarget is the commit a gate run checks, and whether the worktree is exactly that
// commit: a run over uncommitted changes says nothing about the head, so it is not recorded.
func gateTarget(ctx context.Context, lane store.Lane) (head string, clean bool) {
	head, err := git.Run(ctx, lane.Worktree, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return "", false
	}
	dirty, err := git.Dirty(ctx, lane.Worktree)
	return head, err == nil && dirty == ""
}

// gateFailed hands the gate's failure back to the agent while tries remain.
// what names the check: "the repo's gate, `make check`," or "the repo's commit rules".
func (r *Runner) gateFailed(ctx context.Context, run store.Run, lane store.Lane, what, tail, logPath string, lf *store.LaneForge, diag store.Diagnosis) {
	r.diagnose(ctx, run, diag)
	where := ""
	if logPath != "" {
		where = " (" + logPath + ")"
	}
	if lf.GateTries >= MaxGateTries {
		r.feed(ctx, store.FeedGate, lane.ID, "%s failed again in lane %s after %d fixes; not pushing, it is yours%s", strings.TrimSuffix(what, ","), lane.Name, lf.GateTries, where)
		return
	}
	if run.Session == "" {
		r.feed(ctx, store.FeedGate, lane.ID, "%s failed in lane %s; not pushing%s", strings.TrimSuffix(what, ","), lane.Name, where)
		return
	}
	prompt := fmt.Sprintf("[Shepherd] Before pushing your work, Shepherd checked %s in your lane, and it failed:\n\n%s\n\n"+
		"Fix it within your scope, check again yourself, and commit. Shepherd checks again when you finish.", what, tail)
	// Count the try before starting it: a fast fix run can end, and be checked again,
	// before a count saved afterwards would land.
	lf.GateTries++
	r.Store.PutLaneForge(ctx, *lf)
	next, err := r.Start(ctx, StartRequest{Continue: run.ID, Prompt: prompt, Auto: true})
	if err != nil {
		lf.GateTries--
		r.Store.PutLaneForge(ctx, *lf)
		r.feed(ctx, store.FeedGate, lane.ID, "the gate failed in lane %s, and handing it back failed: %s", lane.Name, clip(err.Error(), 200))
		return
	}
	r.feed(ctx, store.FeedGate, lane.ID, "%s failed in lane %s; asked %s to fix it (run %d, try %d of %d)", strings.TrimSuffix(what, ","), lane.Name, run.Agent, next.ID, lf.GateTries, MaxGateTries)
}

// mrText is the merge request's title and description, from the lane's commits.
func (r *Runner) mrText(ctx context.Context, lane store.Lane, gate, from string) (string, string) {
	base := lane.Base
	if git.RefExists(ctx, lane.Worktree, "refs/remotes/origin/"+base) {
		base = "origin/" + base
	}
	log, _ := git.Run(ctx, lane.Worktree, "log", "--reverse", "--format=%s", base+"..HEAD")
	subjects := strings.Split(strings.TrimSpace(log), "\n")
	title := lane.Name
	if len(subjects) > 0 && subjects[0] != "" {
		title = subjects[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Opened by uBixShepherd from lane `%s` (%s). The gate, `%s`, passed in the lane before the push.\n\n", lane.Name, from, gate)
	b.WriteString("Commits:\n")
	for _, s := range subjects {
		if s != "" {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	b.WriteString(r.autoWidenMR(ctx, lane))
	fmt.Fprintf(&b, "\nScope: `%s`. Shepherd does not merge; that is yours.\n", strings.Join(lane.Scope, "`, `"))
	return title, b.String()
}

// outsideDiagnosis is the diagnosis of commits that change files outside the lane's scope.
func outsideDiagnosis(files []string) store.Diagnosis {
	return store.Diagnosis{Kind: store.DiagOutsideScope, Files: files,
		Summary: "not pushed: commits change files outside the lane's scope: " + clip(strings.Join(files, ", "), 160)}
}

// checkProblems judges the lane's branch against the repo's approved check files: the
// hook that would catch a change can be skipped, so Shepherd looks itself before it
// pushes. A branch it cannot judge counts as a problem.
func (r *Runner) checkProblems(ctx context.Context, lane store.Lane) []string {
	problems, err := fold.ShipProblems(ctx, r.Store, lane)
	if err != nil {
		return []string{"could not check the lane against the approved check files: " + err.Error()}
	}
	return problems
}

// checksDiagnosis is the diagnosis of a branch that changes approved check files.
func checksDiagnosis(problems []string) store.Diagnosis {
	return store.Diagnosis{Kind: store.DiagOutsideScope, Evidence: strings.Join(problems, "\n"),
		Summary: "not pushed: " + clip(strings.Join(strings.Fields(strings.Join(problems, " ")), " "), 200)}
}

// forbiddenDiagnosis is the diagnosis of a commit message the repo does not allow; bad is
// what forbidden said.
func forbiddenDiagnosis(bad string) store.Diagnosis {
	return store.Diagnosis{Kind: store.DiagCommitsForbidden, Evidence: bad, Summary: "not pushed: " + clip(bad, 200)}
}

// Unpushed counts the lane's commits that are not on its branch at origin (or, with no
// branch there, beyond its base): the work `lane ship` would push. It is 0 when git
// cannot say.
func Unpushed(ctx context.Context, lane store.Lane) int {
	out, err := git.Run(ctx, lane.Worktree, "rev-list", "--count", pushedRef(ctx, lane)+"..HEAD")
	if err != nil {
		return 0
	}
	var n int
	fmt.Sscan(strings.TrimSpace(out), &n)
	return n
}

// shipVerdict is Crook's answer to whether Shepherd may push the lane: only a repo that
// has opted in with autonomy.push: shepherd.
func shipVerdict(ctx context.Context, repo store.Repo, lane store.Lane, prof config.Profile) crook.Verdict {
	return crook.Check(ctx, crook.Request{Action: crook.ActionShip, Actor: crook.ActorDaemon,
		Facts: crook.Facts{Repo: repo.Name, Lane: lane.Name, Push: prof.Autonomy.Push}})
}
