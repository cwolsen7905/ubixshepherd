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
// feed and returns the lane, the files added and, in words, the requested files the rule
// refused as protected (the rest was still widened); ok is false when nothing changed, and
// then the request goes to the person as before.
func (r *Runner) autoWiden(ctx context.Context, run store.Run, lane store.Lane, files []string) (store.Lane, []string, string, bool) {
	if r.Rescope == nil || len(files) == 0 {
		return lane, nil, "", false
	}
	r.widenStep.Lock()
	defer r.widenStep.Unlock()
	repo, err := r.Store.Repo(ctx, lane.RepoID)
	if err != nil {
		return lane, nil, "", false
	}
	prof := r.Conf().Profile(repo.Name)
	if prof.Widen != "auto" {
		return lane, nil, "", false
	}
	var others [][]string
	lanes, err := r.Store.Lanes(ctx, repo.ID)
	if err != nil {
		return lane, nil, "", false
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
		Others: others, RepoFiles: tracked, Used: r.widenCount(ctx, lane),
		Mode: prof.Widen, MaxFiles: prof.WidenMaxFiles, Cap: prof.WidenCap, Protected: prof.WidenProtected,
	})
	if !v.OK {
		r.Log.Info("auto-widen declined", "lane", lane.Name, "run", run.ID, "why", v.Reason, "refused", len(v.Refused))
		return lane, nil, "", false
	}
	next, err := r.Rescope(ctx, lane.ID, v.Files, nil)
	if err != nil {
		// The Fold's own overlap check disagreed, or the lane closed: the person decides.
		r.Log.Info("auto-widen refused by the fold", "lane", lane.Name, "run", run.ID, "err", err)
		return lane, nil, "", false
	}
	r.widenUsed(lane.ID, 1)
	refused := v.Describe()
	left := ""
	if refused != "" {
		// Named before the scope, which the merge request's lines cut at, so the person
		// sees it there too.
		left = "; left out, needs an explicit widen: " + refused
		r.Log.Info("auto-widen left files out", "lane", lane.Name, "run", run.ID, "refused", refused)
	}
	r.feed(ctx, store.FeedLaneOpened, lane.ID, "lane %s%s+%s (run %d); checks passed: %s%s; scope is now %s",
		lane.Name, widenMark, strings.Join(v.Files, ", "), run.ID, strings.Join(v.Checks, ", "), left, strings.Join(next.Scope, ", "))
	return next, v.Files, refused, true
}

// widenCount is how many auto-widens the lane has had. The feed is the durable record,
// so the first ask after a start reads it; after that the count is kept in memory.
func (r *Runner) widenCount(ctx context.Context, lane store.Lane) int {
	r.widenMu.Lock()
	defer r.widenMu.Unlock()
	n, ok := r.widens[lane.ID]
	if !ok {
		n = len(r.autoWidens(ctx, lane))
		if r.widens == nil {
			r.widens = map[int64]int{}
		}
		r.widens[lane.ID] = n
	}
	return n
}

// widenUsed adds n to the lane's cached count, if it has one.
func (r *Runner) widenUsed(laneID int64, n int) {
	r.widenMu.Lock()
	defer r.widenMu.Unlock()
	if _, ok := r.widens[laneID]; ok {
		r.widens[laneID] += n
	}
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
// refused, when not empty, names the files the rule left out and why.
func widenNote(files []string, lane store.Lane, refused string) string {
	note := fmt.Sprintf("[Shepherd] Your lane's scope now includes %s: Shepherd widened it by rule (small, next to your work, not protected, free of other lanes). "+
		"Your scope is now %s. You can commit and push those files.", strings.Join(files, ", "), strings.Join(lane.Scope, ", "))
	if refused != "" {
		note += " Not widened, still outside your scope: " + refused + ". Do not change them; ask for an explicit widen if you need them."
	}
	return note
}

// autoWidenOutside widens for the files the lane's commits changed outside its scope,
// and continues the run's session to say so. It reports whether it carried the lane on,
// in which case the run's end needs nothing more (the continued run ends and ships). When
// the rule refused some of the files it does not: they are still outside the scope.
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
	next, files, refused, ok := r.autoWiden(ctx, run, lane, outside)
	if !ok {
		return false
	}
	if refused != "" {
		// Protected files are still outside the scope, so the push would be refused:
		// continuing the run would only stall. The allowed files are widened and the feed
		// names the rest, for the person's explicit widen.
		return false
	}
	started, err := r.Start(ctx, StartRequest{Continue: run.ID, Prompt: widenNote(files, next, ""), Auto: true})
	if err != nil {
		// The agent cannot be told, so the widening is undone and the person decides as
		// before; the feed keeps the record of both.
		if _, rerr := r.Rescope(ctx, lane.ID, nil, files); rerr != nil {
			r.Log.Error("undo an auto-widen", "lane", lane.Name, "err", rerr)
		}
		r.feed(ctx, store.FeedLaneOpened, lane.ID, "lane %s: the auto-widen of %s was undone because continuing run %d failed: %s",
			lane.Name, strings.Join(files, ", "), run.ID, clip(err.Error(), 200))
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
	next, added, refused, ok := r.autoWiden(ctx, run, lane, files)
	if !ok {
		return
	}
	if _, err := r.Answer(ctx, d.ID, widenNote(added, next, refused)+" (Answered by Shepherd's auto-widen rule, not by the person.)"); err != nil {
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
