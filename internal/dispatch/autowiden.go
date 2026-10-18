package dispatch

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/autowiden"
	"github.com/ubixsys/ubixshepherd/internal/git"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// widenMark is in the feed item of every auto-widen; the lane's count is the number of
// its feed items that carry it.
const widenMark = " scope auto-widened: "

// autoWiden widens the run's lane by rule, for the files the run needs, if the repo
// opted in and autowiden.Decide allows it. The Fold does the widening (Runner.Rescope),
// so scope leases are judged as for the person's widen. It records the widening in the
// feed and returns the lane and the files added; ok is false when nothing changed, and
// then the request goes to the person as before.
func (r *Runner) autoWiden(ctx context.Context, run store.Run, lane store.Lane, files []string) (store.Lane, []string, bool) {
	if r.Rescope == nil || len(files) == 0 {
		return lane, nil, false
	}
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return lane, nil, false
	}
	prof := r.Conf().Profile(repo.Name)
	if prof.Widen != "auto" {
		return lane, nil, false
	}
	var others [][]string
	lanes, err := r.Store.Lanes(ctx, repo.ID)
	if err != nil {
		return lane, nil, false
	}
	for _, o := range lanes {
		if o.ID != lane.ID && o.State == store.LaneOpen {
			others = append(others, o.Scope)
		}
	}
	var tracked []string
	if out, err := git.Run(ctx, repo.Path, "ls-tree", "-r", "-z", "--name-only", "HEAD"); err == nil {
		tracked = strings.Split(out, "\x00")
	}
	v := autowiden.Decide(autowiden.Input{
		Files: files, Scope: lane.Scope, Changed: changedFiles(ctx, lane),
		Others: others, RepoFiles: tracked, Used: len(r.autoWidens(ctx, lane)),
		Mode: prof.Widen, MaxFiles: prof.WidenMaxFiles, Cap: prof.WidenCap, Protected: prof.WidenProtected,
	})
	if !v.OK {
		r.Log.Info("auto-widen declined", "lane", lane.Name, "run", run.ID, "why", v.Reason)
		return lane, nil, false
	}
	next, err := r.Rescope(ctx, lane.ID, v.Files, nil)
	if err != nil {
		// The Fold's own overlap check disagreed, or the lane closed: the person decides.
		r.Log.Info("auto-widen refused by the fold", "lane", lane.Name, "run", run.ID, "err", err)
		return lane, nil, false
	}
	r.feed(ctx, store.FeedLaneOpened, lane.ID, "lane %s%s+%s (run %d); checks passed: %s; scope is now %s",
		lane.Name, widenMark, strings.Join(v.Files, ", "), run.ID, strings.Join(v.Checks, ", "), strings.Join(next.Scope, ", "))
	return next, v.Files, true
}

// autoWidens are the feed items of the lane's auto-widens, oldest first.
func (r *Runner) autoWidens(ctx context.Context, lane store.Lane) []store.FeedItem {
	var out []store.FeedItem
	var after int64
	for {
		page, err := r.Store.Feed(ctx, after, 500)
		if err != nil || len(page) == 0 {
			return out
		}
		for _, it := range page {
			if it.Kind == store.FeedLaneOpened && it.Ref == lane.ID && strings.Contains(it.Text, widenMark) {
				out = append(out, it)
			}
		}
		after = page[len(page)-1].ID
	}
}

// widenNote is what the agent is told after an auto-widen.
func widenNote(files []string, lane store.Lane) string {
	return fmt.Sprintf("[Shepherd] Your lane's scope now includes %s: Shepherd widened it by rule (small, next to your work, not protected, free of other lanes). "+
		"Your scope is now %s. You can commit and push those files.", strings.Join(files, ", "), strings.Join(lane.Scope, ", "))
}

// autoWidenOutside widens for the files the lane's commits changed outside its scope,
// and continues the run's session to say so. It reports whether it carried the lane on,
// in which case the run's end needs nothing more (the continued run ends and ships).
func (r *Runner) autoWidenOutside(ctx context.Context, run store.Run, lane store.Lane) bool {
	if run.State != store.RunSucceeded || run.Session == "" || len(run.Outside) == 0 {
		return false
	}
	// Every file of the lane that is outside, not only this run's: all of them must be
	// covered, or the push is still refused.
	outside := r.outsideScope(ctx, lane)
	if len(outside) == 0 {
		return false
	}
	next, files, ok := r.autoWiden(ctx, run, lane, outside)
	if !ok {
		return false
	}
	started, err := r.Start(ctx, StartRequest{Continue: run.ID, Prompt: widenNote(files, next), Auto: true})
	if err != nil {
		r.feed(ctx, store.FeedLaneOpened, lane.ID, "lane %s was auto-widened, but continuing run %d failed: %s", lane.Name, run.ID, clip(err.Error(), 200))
		return false
	}
	r.feed(ctx, store.FeedRunStarted, started.ID, "run %d continues after the auto-widen of lane %s", started.ID, lane.Name)
	return true
}

var (
	backticked = regexp.MustCompile("`([^`\n]+)`")
	// asksToWiden is an explicit request to widen; offersOther is a sign the question
	// offers another way out (a revert, a move), which the person must choose between.
	asksToWiden  = regexp.MustCompile(`(?i)\b(widen|widening|extend|expand)\b.*\bscope\b|\bscope\b.*\b(widen|widening|extend|expand)\b|\b(add|include)\b.*\bto\b.*\bscope\b`)
	offersOther  = regexp.MustCompile(`(?i)\b(revert|drop|undo|remove|instead|rather|alternatively|move|or should|or would|or do)\b`)
	looksLikePth = regexp.MustCompile(`^[\w.@+\-/]+$`)
)

// scopeFiles parses the files a scope question names: the backticked paths of a question
// that asks to widen the scope and offers no other way out (no options, no revert or
// move), so that saying yes to the widening is the only reading. Anything else in backticks (a command, a word) is not a
// file and is left out; no files means the question is not one the rule can read.
func scopeFiles(d store.Decision) []string {
	text := d.Question
	if !asksToWiden.MatchString(text) || offersOther.MatchString(text) || len(d.Options) > 0 {
		return nil
	}
	var out []string
	for _, m := range backticked.FindAllStringSubmatch(text, -1) {
		p := strings.TrimSpace(m[1])
		if looksLikePth.MatchString(p) && (strings.Contains(p, "/") || strings.Contains(p[1:], ".")) {
			out = append(out, p)
		}
	}
	return out
}

// autoWidenDecision widens for a scope question an agent raised, and answers it with a
// note that says Shepherd's rule did it, not the person. The decision stays on record.
func (r *Runner) autoWidenDecision(ctx context.Context, d store.Decision) {
	files := scopeFiles(d)
	if len(files) == 0 || r.Rescope == nil {
		return
	}
	run, err := r.Store.Run(ctx, d.RunID)
	if err != nil {
		return
	}
	lane, err := r.Store.Lane(ctx, run.LaneID)
	if err != nil || lane.State != store.LaneOpen {
		return
	}
	next, added, ok := r.autoWiden(ctx, run, lane, files)
	if !ok {
		return
	}
	if _, err := r.Answer(ctx, d.ID, widenNote(added, next)+" (Answered by Shepherd's auto-widen rule, not by the person.)"); err != nil {
		r.Log.Error("answer an auto-widened decision", "decision", d.ID, "err", err)
	}
}

// autoWidenMR is the merge request description's lines for the lane's auto-widens, or "".
func (r *Runner) autoWidenMR(ctx context.Context, lane store.Lane) string {
	items := r.autoWidens(ctx, lane)
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nScope widened by Shepherd's rule, without asking:\n")
	for _, it := range items {
		text := it.Text[strings.Index(it.Text, widenMark)+len(widenMark):]
		text, _, _ = strings.Cut(text, "; scope is now")
		fmt.Fprintf(&b, "- %s\n", text)
	}
	return b.String()
}
