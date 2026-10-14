package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRule(t *testing.T, dir, id, text string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBriefPlanFirst(t *testing.T) {
	with := Brief("t", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{PlanFirst: true}, true, "", "")
	if !strings.Contains(with, "Plan first") || !strings.Contains(with, "ask_human") {
		t.Errorf("plan_first brief:\n%s", with)
	}
	noTools := Brief("t", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{PlanFirst: true}, false, "", "")
	if !strings.Contains(noTools, "Plan first") || strings.Contains(noTools, "with ask_human") {
		t.Errorf("plan_first brief without tools:\n%s", noTools)
	}
	if without := Brief("t", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{}, true, "", ""); strings.Contains(without, "Plan first") {
		t.Errorf("brief asks for a plan unasked:\n%s", without)
	}
}

func TestBriefCarriesStandards(t *testing.T) {
	b := Brief("the task", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{}, false, "", "Standards. Digest here.\n")
	if !strings.Contains(b, "Standards. Digest here.") || strings.Index(b, "Digest here") > strings.Index(b, "Task:") {
		t.Errorf("brief:\n%s", b)
	}
}

const standardsProfile = `repos:
  app:
    gate: "true"
    standards: [house]
    autonomy:
      push: shepherd
`

func standardsFixture(t *testing.T, profile string) (*fixture, *shipForge, string) {
	f, sf := shipFixture(t, "ok", profile)
	root := t.TempDir()
	f.runner.Standards = root
	return f, sf, root
}

func TestRunBriefHasDigestAndPlanFirst(t *testing.T) {
	f, _, root := standardsFixture(t, standardsProfile)
	writeRule(t, filepath.Join(root, "house"), "no-leaks", "---\nlevel: must\n---\nNever print secrets.\n\n## Why\n\nPRIVATE REASON\n")
	writeRule(t, filepath.Join(f.lane.Worktree, ".shepherd", "standards"), "no-leaks", "---\nlevel: should\n---\nThe repo says: mask secrets.\n")
	os.WriteFile(filepath.Join(f.lane.Worktree, "CLAUDE.md"), []byte("rules\n"), 0o644)
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	in := stdinOf(t, f.wait(t, run.ID))
	for _, want := range []string{"take precedence over the Shepherd digest", "CLAUDE.md", "- should no-leaks: The repo says: mask secrets.", "Plan first"} {
		if !strings.Contains(in, want) {
			t.Errorf("brief lacks %q:\n%s", want, in)
		}
	}
	if strings.Contains(in, "Never print secrets") || strings.Contains(in, "PRIVATE REASON") {
		t.Errorf("brief has the overridden rule or a rationale:\n%s", in)
	}
}

func TestPlanFirstOffAndAutoRuns(t *testing.T) {
	off, _, _ := standardsFixture(t, strings.Replace(standardsProfile, "      push: shepherd\n", "      push: shepherd\n      plan_first: false\n", 1))
	run, _ := off.runner.Start(context.Background(), StartRequest{LaneID: off.lane.ID, Agent: "claude", Prompt: "work"})
	if in := stdinOf(t, off.wait(t, run.ID)); strings.Contains(in, "Plan first") {
		t.Errorf("plan_first: false still asks for a plan:\n%s", in)
	}
	auto, _, _ := standardsFixture(t, standardsProfile)
	run, _ = auto.runner.Start(context.Background(), StartRequest{LaneID: auto.lane.ID, Agent: "claude", Prompt: "work", Auto: true})
	if in := stdinOf(t, auto.wait(t, run.ID)); strings.Contains(in, "Plan first") {
		t.Errorf("a run Shepherd started itself waits for a plan nobody approves:\n%s", in)
	}
}

func TestShipHandsBackAFailedMustCheck(t *testing.T) {
	f, sf, root := standardsFixture(t, standardsProfile)
	writeRule(t, filepath.Join(root, "house"), "no-work-files", "---\nlevel: must\napplies_to: [\"src/**\"]\ncheck: \"echo found a work file; exit 1\"\n---\nNo work files.\n")
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	waitFeed(t, f, "the repo's standards checks failed in lane work; asked claude to fix it")
	if originHas(t, f, "work") || len(sf.created) != 0 {
		t.Error("pushed although a must check failed")
	}
	runs, _ := f.st.Runs(context.Background(), f.lane.ID, "", 5)
	found := false
	for _, r := range runs {
		if r.Parent == run.ID && strings.Contains(r.Prompt, "standard no-work-files (must) failed") && strings.Contains(r.Prompt, "found a work file") {
			found = true
		}
	}
	if !found {
		t.Error("the fix run does not name the rule id and output")
	}
}

func TestShipIgnoresShouldAndUntouchedChecks(t *testing.T) {
	f, sf, root := standardsFixture(t, standardsProfile)
	writeRule(t, filepath.Join(root, "house"), "soft", "---\nlevel: should\ncheck: \"exit 1\"\n---\nA soft one.\n")
	writeRule(t, filepath.Join(root, "house"), "web-only", "---\nlevel: must\napplies_to: [\"web/**\"]\ncheck: \"exit 1\"\n---\nWeb only.\n")
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	waitFeed(t, f, "opened !42 for you to review")
	waitFeed(t, f, "should-rule soft: check failed")
	if len(sf.created) != 1 {
		t.Errorf("created = %v", sf.created)
	}
}

func TestShipLaneRefusesAFailedMustCheck(t *testing.T) {
	f, _, root := standardsFixture(t, standardsProfile)
	writeRule(t, filepath.Join(root, "house"), "r", "---\nlevel: must\ncheck: \"echo bad; exit 1\"\n---\nR.\n")
	commitInLane(t, f, "src/x.go", "add x")
	_, err := f.runner.Ship(context.Background(), f.lane.ID)
	if err == nil || !strings.Contains(err.Error(), "standard r (must) failed") {
		t.Errorf("ship err = %v", err)
	}
	if originHas(t, f, "work") {
		t.Error("pushed")
	}
}

func TestDefaultStandardsDir(t *testing.T) {
	r := &Runner{Dir: filepath.Join("/h", "runs")}
	if got := r.StandardsDir(); got != filepath.Join("/h", "standards") {
		t.Errorf("dir = %s", got)
	}
}

func TestBriefCarriesLinkRule(t *testing.T) {
	for _, tools := range []bool{true, false} {
		b := Brief("t", "l", "r", "b", "main", "/w", []string{"x"}, "", Powers{}, tools, "", "")
		if !strings.Contains(b, "#note_<first_note_id>") || !strings.Contains(b, "/-/pipelines/<id>") {
			t.Errorf("brief (tools=%v) lacks the link rule:\n%s", tools, b)
		}
	}
}
