package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

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
	fileName   = regexp.MustCompile(`^[\w@+\-]*[A-Za-z_][\w@+\-]*(\.[\w@+\-]+)*\.[A-Za-z][A-Za-z0-9]{0,5}$`)
	proceedRec = regexp.MustCompile(`(?i)^\W*(proceed|approve|approved|go ahead|go-ahead|yes|ok|okay|continue|build it|go)\b`)
	notRec     = regexp.MustCompile(`(?i)\b(do not|don't|not proceed|wait|hold|reject|no\b|decline|stop)\b`)
	// sensitive are words that put a plan back in the person's hands: anything about
	// merging, pushing, tagging, releasing, deploying, money, widening scope or
	// destroying data. A plan that merely mentions one waits; it is the cautious side.
	rootFiles = map[string]bool{"Makefile": true, "Dockerfile": true, "Jenkinsfile": true, "LICENSE": true, "README": true, "CHANGELOG": true, "CODEOWNERS": true}
	sensitive = regexp.MustCompile(`(?i)\b(` +
		`merg(e|es|ed|ing)|push(es|ed|ing)?|tag(s|ged|ging)?|releas(e|es|ed|ing)|deploy(s|ed|ing|ment|ments)?|publish(es|ed|ing)?|` +
		`ship(s|ped|ping)?|promot(e|es|ed|ing|ion)|reverts?|reverted|reverting|rollback|rollout|roll\s+(back|out)|go\s+live|cut\s+a\s+(tag|release)|` +
		`pric(e|es|ed|ing)|payment|payments|billing|budget|spend|spending|spent|cost|costs|money|` +
		`widen|widening|force|delet(e|es|ed|ing)|production|credentials?|secrets?` +
		`)\b|\$\s?\d|(outside|beyond|expand\w*|extend\w*)\s+(the\s+|my\s+|its\s+)?scope|approv\w*\s+(the\s+|this\s+|its\s+|my\s+)?(mr|merge|pull|request)\b|\barm(s|ed|ing)?\s+(the\s+)?(merge|mr)\b`)
	// departs marks a plan that says it does not follow the brief.
	departs = regexp.MustCompile(`(?i)\b(deviat\w*|differs? from|instead of the (brief|item|task)|not in the (brief|item|task)|beyond the (brief|item|task)|outside the (brief|item|task)|ambiguous|conflicts? with|unclear|contradict\w*)\b`)
)

// planFiles are the files a plan names. It is cautious and reads every word: any token
// with a path separator, a backslash, "~" or "$" counts, and so does a token that looks
// like a file name (a dot and a short extension, or a well-known root file such as
// Makefile), ticked or not. A false hit only holds the plan for the person.
func planFiles(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("`,;()[]{}<>\"'", r)
	}) {
		tok = strings.TrimRight(tok, ".,:;!?")
		tok = strings.TrimLeft(tok, ":")
		if tok == "" || seen[tok] || !fileLike(tok) {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// fileLike reports whether a token reads as a file or path.
func fileLike(tok string) bool {
	if strings.ContainsAny(tok, "/\\~$") {
		return true
	}
	if rootFiles[tok] {
		return true
	}
	if fileName.MatchString(tok) {
		switch strings.ToLower(tok) {
		case "e.g", "i.e", "etc", "vs":
			return false
		}
		return true
	}
	return false
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
		if strings.ContainsAny(f, "\\~$:") || strings.HasPrefix(f, "/") || strings.HasPrefix(f, "..") || !scope.Any(lane.Scope, f) {
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
		// Answering resumes the agent, so it is an autonomous continuation: it passes the
		// same gate as any other, and counts against the lane's starts for the day.
		if a := k.Allow(ctx, ws, KeepContinue, lane.ID); !a.OK {
			k.s.Log.Debug("keep-going: plan left for the person", "decision", d.ID, "why", a.Reason)
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
