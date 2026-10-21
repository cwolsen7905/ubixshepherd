package mergerule

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/crook"
)

const specRule = `---
kind: merge
mode: shadow
require:
  - linked_requirement
  - approved_checks_pass
  - no_test_edits
  - gate_green
  - pipeline_green
  - ai_review_not_blocking
  - up_to_date
max_changed_lines: 50
paths_not: [".shepherd/**", ".gitlab-ci.yml"]
then: proceed
---
Spec work merges on its own when the approved checks prove it.
`

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is a clone of a bare origin whose main holds the rule and one existing test.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T, rule string) *repo {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitT(t, root, "init", "--bare", "-q", origin)
	dir := filepath.Join(root, "work")
	gitT(t, root, "clone", "-q", origin, dir)
	gitT(t, dir, "checkout", "-q", "-b", "main")
	write(t, dir, "a_test.go", "package a\n")
	write(t, dir, "a.go", "package a\n")
	if rule != "" {
		write(t, dir, ".shepherd/rules/spec-verified.md", rule)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "base")
	gitT(t, dir, "push", "-q", "origin", "main")
	return &repo{t, dir}
}

// lane commits files on a new branch and returns its head.
func (r *repo) lane(name string, files map[string]string) string {
	r.t.Helper()
	gitT(r.t, r.dir, "checkout", "-q", "-b", name, "main")
	for p, b := range files {
		write(r.t, r.dir, p, b)
	}
	gitT(r.t, r.dir, "add", "-A")
	gitT(r.t, r.dir, "commit", "-q", "-m", "lane")
	head := gitT(r.t, r.dir, "rev-parse", "HEAD")
	gitT(r.t, r.dir, "checkout", "-q", "main")
	return head
}

// input is a lane that meets every condition but what a test changes.
func (r *repo) input(head string) Input {
	return Input{
		Repo: "r", Lane: "l", Dir: r.dir, Base: "main", Head: head, MR: 7, MRURL: "https://x/7",
		ItemLinked: true, Requirements: []string{"REQ-1"},
		ApprovedChecks: "pass", CheckFilesKnown: true, CheckFiles: []string{"checks_test.go"},
		GateSHA: head, GatePassed: true, Pipeline: "success", AIReview: "ok", OpenThreads: 0,
	}
}

type recorder struct {
	mu   sync.Mutex
	rows []crook.Entry
}

func (r *recorder) Record(_ context.Context, e crook.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, e)
	return nil
}

func observer(rec *recorder, feed *[]string) *Observer {
	return &Observer{Rec: rec, Feed: func(_ context.Context, lane, msg string) { *feed = append(*feed, lane+": "+msg) }}
}

func TestEveryConditionMeetsAndEachFailsAlone(t *testing.T) {
	r := newRepo(t, specRule)
	head := r.lane("ok", map[string]string{"b.go": "package a\n"})
	ctx := context.Background()
	cases := []struct {
		name   string
		change func(*Input)
		want   crook.Kind
		unmet  string
	}{
		{"all met", func(*Input) {}, crook.Proceed, ""},
		{"not linked", func(in *Input) { in.ItemLinked = false }, crook.Ask, "linked_requirement"},
		{"checks fail", func(in *Input) { in.ApprovedChecks = "fail" }, crook.Ask, "approved_checks_pass"},
		{"checks unknown", func(in *Input) { in.ApprovedChecks = ""; in.CheckFilesKnown = false }, crook.Ask, "approved_checks_pass"},
		{"gate on an older head", func(in *Input) { in.GateSHA = "0000000" }, crook.Ask, "gate_green"},
		{"gate failed", func(in *Input) { in.GatePassed = false }, crook.Ask, "gate_green"},
		{"pipeline red", func(in *Input) { in.Pipeline = "failed" }, crook.Ask, "pipeline_green"},
		{"pipeline running holds", func(in *Input) { in.Pipeline = "running" }, crook.Hold, "pipeline_green"},
		{"review unreviewed", func(in *Input) { in.AIReview = "unreviewed" }, crook.Ask, "ai_review_not_blocking"},
		{"review blocking", func(in *Input) { in.AIReview = "blocking" }, crook.Ask, "ai_review_not_blocking"},
		{"review unset", func(in *Input) { in.AIReview = "" }, crook.Ask, "ai_review_not_blocking"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := r.input(head)
			c.change(&in)
			var feed []string
			rec := &recorder{}
			res, err := observer(rec, &feed).Observe(ctx, in)
			if err != nil || res == nil {
				t.Fatalf("Observe = %v, %v", res, err)
			}
			if res.Verdict != c.want || res.Rule != "spec-verified" {
				t.Fatalf("verdict = %s by %s (%s), want %s", res.Verdict, res.Rule, res.Reason, c.want)
			}
			if c.unmet != "" && !strings.Contains(res.Reason, c.unmet) {
				t.Errorf("reason %q does not name %s", res.Reason, c.unmet)
			}
			if len(rec.rows) != 1 || rec.rows[0].Stage != StageShadow || rec.rows[0].Action != ActionMerge {
				t.Fatalf("rows = %+v", rec.rows)
			}
			if got := len(feed) == 1; got != (c.want == crook.Proceed) {
				t.Errorf("feed = %v", feed)
			}
		})
	}
}

func TestTestEditsAndCheckFiles(t *testing.T) {
	r := newRepo(t, specRule)
	ctx := context.Background()
	// An added test is new work; an edited existing test is not allowed.
	added := r.lane("added", map[string]string{"new_test.go": "package a\n"})
	edited := r.lane("edited", map[string]string{"a_test.go": "package a // weaker\n"})
	check := r.lane("check", map[string]string{"checks_test.go": "package a\n"})
	for _, c := range []struct {
		name string
		head string
		want crook.Kind
	}{{"added", added, crook.Proceed}, {"edited", edited, crook.Ask}, {"check file", check, crook.Proceed}} {
		in := r.input(c.head)
		in.CheckFiles = []string{"a.go"} // not touched by "check"
		res, err := observer(&recorder{}, new([]string)).Observe(ctx, in)
		if err != nil || res.Verdict != c.want {
			t.Errorf("%s: %v %v, want %s", c.name, res, err, c.want)
		}
	}
	// Marked a_test.go as an approved check: editing it is refused for that reason too.
	in := r.input(edited)
	in.CheckFiles = []string{"a_test.go"}
	f := Facts(ctx, in)
	if f["check_files_edited"] != true || f["tests_edited"] != true {
		t.Errorf("facts = %v", f)
	}
}

func TestSizeProtectedPathsAndBase(t *testing.T) {
	r := newRepo(t, specRule)
	ctx := context.Background()
	big := r.lane("big", map[string]string{"big.go": strings.Repeat("x\n", 51)})
	atLimit := r.lane("limit", map[string]string{"limit.go": strings.Repeat("x\n", 50)})
	ci := r.lane("ci", map[string]string{".gitlab-ci.yml": "a: b\n"})
	rules := r.lane("rules", map[string]string{".shepherd/rules/x.md": "x\n"})
	for _, c := range []struct {
		name string
		head string
		want crook.Kind
	}{{"over the limit", big, crook.Ask}, {"at the limit", atLimit, crook.Proceed},
		{"ci config", ci, crook.Ask}, {"rules folder", rules, crook.Ask}} {
		res, err := observer(&recorder{}, new([]string)).Observe(ctx, r.input(c.head))
		if err != nil || res == nil || res.Verdict != c.want {
			t.Errorf("%s: %+v %v, want %s", c.name, res, err, c.want)
		}
	}
	// The base moves on: the lane is no longer up to date, which holds until it is rebased.
	behind := r.lane("behind", map[string]string{"c.go": "package a\n"})
	write(t, r.dir, "later.go", "package a\n")
	gitT(t, r.dir, "add", "-A")
	gitT(t, r.dir, "commit", "-q", "-m", "later")
	res, _ := observer(&recorder{}, new([]string)).Observe(ctx, r.input(behind))
	if res.Verdict != crook.Hold || !strings.Contains(res.Reason, "up_to_date") {
		t.Errorf("behind = %+v", res)
	}
}

func TestRulesComeFromTheBaseBranchNotTheWorktree(t *testing.T) {
	r := newRepo(t, specRule)
	head := r.lane("l", map[string]string{"b.go": "package a\n"})
	// The lane's checkout rewrites the rule to need nothing; the base branch is unchanged.
	gitT(t, r.dir, "checkout", "-q", "l")
	write(t, r.dir, ".shepherd/rules/spec-verified.md", "---\nkind: merge\nmode: shadow\nthen: proceed\n---\nAnything goes.\n")
	in := r.input(head)
	in.ItemLinked = false
	res, err := observer(&recorder{}, new([]string)).Observe(context.Background(), in)
	if err != nil || res == nil || res.Verdict != crook.Ask {
		t.Fatalf("got %+v %v: the loosened worktree rule was used", res, err)
	}
}

func TestNoRuleNoRowsAndLiveRulesAreNotObserved(t *testing.T) {
	ctx := context.Background()
	for name, rule := range map[string]string{
		"no rules folder": "",
		"live rule":       strings.Replace(specRule, "mode: shadow", "mode: live", 1),
	} {
		r := newRepo(t, rule)
		head := r.lane("l", map[string]string{"b.go": "package a\n"})
		rec := &recorder{}
		res, err := observer(rec, new([]string)).Observe(ctx, r.input(head))
		if err != nil || res != nil || len(rec.rows) != 0 {
			t.Errorf("%s: %+v %v rows %d", name, res, err, len(rec.rows))
		}
	}
}

func TestBrokenRuleOrBaseIsReportedOnceAndMeansNoRule(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t, "---\nkind: merge\nbogus: 1\n---\nx\n")
	head := r.lane("l", map[string]string{"b.go": "package a\n"})
	o := observer(&recorder{}, new([]string))
	res, err := o.Observe(ctx, r.input(head))
	if res != nil {
		t.Fatalf("a broken rule was used: %+v", res)
	}
	_ = err
	in := r.input(head)
	in.Base = "origin/nope"
	if _, err := o.Observe(ctx, in); err == nil {
		t.Fatal("an unreadable base gave no error")
	}
	if _, err := o.Observe(ctx, in); err != nil {
		t.Errorf("the same failure was reported twice: %v", err)
	}
}

func TestRowOnlyOnChangeAndOutcome(t *testing.T) {
	r := newRepo(t, specRule)
	head := r.lane("l", map[string]string{"b.go": "package a\n"})
	ctx := context.Background()
	rec := &recorder{}
	var feed []string
	o := observer(rec, &feed)
	in := r.input(head)
	in.Pipeline = "running"
	for range 3 {
		if _, err := o.Observe(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.rows) != 1 {
		t.Fatalf("a repeat poll wrote rows: %d", len(rec.rows))
	}
	in.Pipeline = "success"
	o.Observe(ctx, in)
	o.Observe(ctx, in)
	if len(rec.rows) != 2 || rec.rows[1].Kind != crook.Proceed || len(feed) != 1 {
		t.Fatalf("rows %d feed %v", len(rec.rows), feed)
	}
	if err := o.Merged(ctx, in, "abcdef1234567890"); err != nil {
		t.Fatal(err)
	}
	last := rec.rows[len(rec.rows)-1]
	if len(rec.rows) != 3 || last.Stage != StageOutcome || !strings.Contains(last.Reason, "abcdef12") || !strings.Contains(last.Reason, "proceed") {
		t.Fatalf("outcome row = %+v", last)
	}
	if err := o.Merged(ctx, in, "x"); err != nil || len(rec.rows) != 3 {
		t.Errorf("a second outcome was written")
	}
}

func TestShadowStages(t *testing.T) {
	if !IsShadowStage(StageShadow) || !IsShadowStage(StageOutcome) || IsShadowStage("policy") {
		t.Error("IsShadowStage")
	}
}

type failOnce struct {
	recorder
	fail bool
}

func (f *failOnce) Record(ctx context.Context, e crook.Entry) error {
	if f.fail {
		f.fail = false
		return context.DeadlineExceeded
	}
	return f.recorder.Record(ctx, e)
}

func TestFailedWriteIsRetriedOnTheNextPoll(t *testing.T) {
	r := newRepo(t, specRule)
	head := r.lane("l", map[string]string{"b.go": "package a\n"})
	rec := &failOnce{fail: true}
	o := &Observer{Rec: rec}
	ctx := context.Background()
	if res, err := o.Observe(ctx, r.input(head)); err == nil || res == nil || res.Recorded {
		t.Fatalf("first poll = %+v, %v", res, err)
	}
	if res, err := o.Observe(ctx, r.input(head)); err != nil || !res.Recorded || len(rec.rows) != 1 {
		t.Fatalf("second poll = %+v, %v, rows %d", res, err, len(rec.rows))
	}
}

func TestFeedLineIsOncePerHead(t *testing.T) {
	r := newRepo(t, specRule)
	head := r.lane("l", map[string]string{"b.go": "package a\n"})
	var feed []string
	o := observer(&recorder{}, &feed)
	ctx := context.Background()
	in := r.input(head)
	for _, p := range []string{"success", "failed", "success"} {
		in.Pipeline = p
		if _, err := o.Observe(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if len(feed) != 1 {
		t.Fatalf("feed = %v, want one line for the head", feed)
	}
	// A new head gets its own line.
	in.Head, in.GateSHA = r.lane("l2", map[string]string{"c.go": "package a\n"}), ""
	in.GateSHA = in.Head
	in.Pipeline = "success"
	o.Observe(ctx, in)
	if len(feed) != 2 {
		t.Errorf("feed = %v, want a second line for the new head", feed)
	}
}
