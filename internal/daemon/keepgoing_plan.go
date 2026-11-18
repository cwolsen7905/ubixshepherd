package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Plan auto-approve (docs/keep-going.md, "Plans that proceed"). While keep-going is on, a
// plan an agent posts as a decision proceeds without the person when every rule holds:
// it is a plan, every file it names is inside the lane's scope, the lane serves an
// approved item and the plan states no departure from it, the agent itself recommends
// going ahead, and it touches no merge, push, tag, release, money or scope question.
// Anything else stays a decision for the person, as it always was.

// PlanRule is the rule's name, in the answer and the feed, so the person can find it.
const PlanRule = "plan-auto-approve"

var (
	planWord   = regexp.MustCompile(`(?i)\bplan\b`)
	fileTicks  = regexp.MustCompile("`([^`\n]+)`")
	pathLike   = regexp.MustCompile(`^[\w.@+\-/]+$`)
	bareSlash  = regexp.MustCompile(`(?:^|[\s(,])((?:\.\.?/|/)?[\w.@+\-]+(?:/[\w.@+\-*]+)+)`)
	proceedRec = regexp.MustCompile(`(?i)^\W*(proceed|approve|approved|go ahead|go-ahead|yes|ok|okay|continue|build it|go)\b`)
	notRec     = regexp.MustCompile(`(?i)\b(do not|don't|not proceed|wait|hold|reject|no\b|decline|stop)\b`)
	// sensitive are words that put a plan back in the person's hands: anything about
	// merging, pushing, tagging, releasing, deploying, money, widening scope or
	// destroying data. A plan that merely mentions one waits; it is the cautious side.
	sensitive = regexp.MustCompile(`(?i)\b(merge|merges|merging|push|pushes|pushing|tag|tags|tagging|release|releases|releasing|deploy|deploys|deploying|publish|publishing|price|pricing|payment|billing|budget|spend|spending|cost|costs|money|widen|widening|force|delete|deleting|production|credential|credentials|secret|secrets)\b|\$\s?\d|(outside|beyond|expand\w*|extend\w*)\s+(the\s+|my\s+|its\s+)?scope`)
	// departs marks a plan that says it does not follow the brief.
	departs = regexp.MustCompile(`(?i)\b(deviat\w*|differs? from|instead of the (brief|item|task)|not in the (brief|item|task)|beyond the (brief|item|task)|outside the (brief|item|task)|ambiguous|conflicts? with|unclear|contradict\w*)\b`)
)

// planFiles are the files a plan names: backticked paths and bare paths with a slash.
// It is cautious: any token that looks like a path counts, so one outside the scope
// holds the plan.
func planFiles(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimRight(strings.TrimSpace(p), ".,;:)")
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range fileTicks.FindAllStringSubmatch(text, -1) {
		p := strings.TrimSpace(m[1])
		if pathLike.MatchString(p) && (strings.Contains(p, "/") || (len(p) > 1 && strings.Contains(p[1:], "."))) {
			add(p)
		}
	}
	for _, m := range bareSlash.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	return out
}

// planVerdict says whether the decision is a plan that may proceed, and why not when it
// may not (the reason is for the log and tests; the person just sees the decision).
func planVerdict(d store.Decision, lane store.Lane, item *store.NextItem) (bool, string) {
	head, _, _ := strings.Cut(strings.TrimSpace(d.Question), "\n")
	if !planWord.MatchString(head) {
		return false, "not a plan"
	}
	all := strings.Join(append([]string{d.Question, d.Why, d.Recommendation}, d.Options...), "\n")
	if sensitive.MatchString(all) {
		return false, "it touches a merge, push, tag, release, money, scope or data question"
	}
	files := planFiles(all)
	if len(files) == 0 {
		return false, "it names no files, so its scope cannot be checked"
	}
	for _, f := range files {
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "..") || !scope.Any(lane.Scope, f) {
			return false, "it names " + f + ", outside the lane's scope"
		}
	}
	if item == nil {
		return false, "the lane serves no approved item to follow"
	}
	if departs.MatchString(all) {
		return false, "it says it departs from, or is unsure of, the brief"
	}
	rec := strings.TrimSpace(d.Recommendation)
	if rec == "" || !proceedRec.MatchString(rec) || notRec.MatchString(rec) {
		return false, "the agent does not recommend going ahead"
	}
	return true, ""
}

// approvedItemOf is the approved Next-up item the lane serves, nil when there is none.
func (k *KeepGoing) approvedItemOf(ctx context.Context, lane store.Lane) *store.NextItem {
	items, err := k.s.Store.NextItems(ctx)
	if err != nil {
		return nil
	}
	for _, it := range items {
		if it.LaneID == lane.ID && it.State == store.NextApproved {
			return &it
		}
	}
	return nil
}

// answerDecision answers a decision with the rule's text. Unset, the daemon's runner
// carries it into the agent's session.
func (k *KeepGoing) answerDecision(ctx context.Context, id int64, text string) error {
	if f := k.planAnswer; f != nil {
		return f(ctx, id, text)
	}
	if k.s.Runner == nil {
		return fmt.Errorf("no runner")
	}
	_, err := k.s.Runner.Answer(ctx, id, text)
	return err
}

// autoApprovePlans answers the workspace's open plan decisions that meet the rules. It is
// called with the mode on; it does nothing when the switch is off.
func (k *KeepGoing) autoApprovePlans(ctx context.Context, ws int64) {
	if !k.s.LiveConfig().KeepGoing.PlansAutoApproved() {
		return
	}
	open, err := k.s.Store.Decisions(ctx, store.DecisionOpen)
	if err != nil {
		k.s.Log.Warn("keep-going: list decisions", "err", err)
		return
	}
	for _, d := range open {
		run, err := k.s.Store.Run(ctx, d.RunID)
		if err != nil {
			continue
		}
		lane, err := k.s.Store.Lane(ctx, run.LaneID)
		if err != nil || lane.State != store.LaneOpen {
			continue
		}
		repo, err := k.s.Store.Repo(ctx, lane.RepoID)
		if err != nil || repo.WorkspaceID != ws {
			continue
		}
		ok, why := planVerdict(d, lane, k.approvedItemOf(ctx, lane))
		if !ok {
			if why != "not a plan" {
				k.s.Log.Debug("keep-going: plan left for the person", "decision", d.ID, "why", why)
			}
			continue
		}
		text := fmt.Sprintf("Auto-approved under keep-going (rule %s), not by the person: the plan names only files inside the lane's scope, follows its approved item, and you recommended going ahead. Proceed with your plan, and stay inside your scope.", PlanRule)
		if err := k.answerDecision(ctx, d.ID, text); err != nil {
			k.s.Log.Warn("keep-going: auto-approve a plan", "decision", d.ID, "err", err)
			continue
		}
		k.s.Store.AddFeed(ctx, FeedKeepGoing, fmt.Sprintf("keep-going auto-approved the plan in decision %d on lane %s (rule %s)", d.ID, lane.Name, PlanRule), d.ID)
		k.Touch(ctx, ws)
	}
}
