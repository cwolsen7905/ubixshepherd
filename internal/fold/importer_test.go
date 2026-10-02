package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

const coordDoc = "# Coordination\n\nIntro.\n\n## 1. Agents\n\n" +
	"| Agent | Scope | Branch prefix |\n| ----- | ----- | ------------- |\n" +
	"| billing (Claude) | Billing seam: `src/billing/**`, `docs/{billing,payments}.md`, `App\\\\Billing\\\\*` namespace | `feat/billing-*` |\n" +
	"| ci (Claude) | `.gitlab-ci.yml`, `bin/ci/**` | `chore/ci-*` |\n" +
	"| idle (Claude) | `idle/**` | `feat/idle-*` |\n\n" +
	"Add a row first.\n\n## 6. Log\n\n- 2026-01-01 someone did something\n"

func TestParseCoord(t *testing.T) {
	rows := ParseCoord(coordDoc)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	b := rows[0]
	if strings.Join(b.Scope, " ") != "src/billing/** docs/billing.md docs/payments.md" || b.Prefixes[0] != "feat/billing-*" {
		t.Errorf("billing row = %+v", b)
	}
	if len(b.Dropped) != 1 || !strings.Contains(b.Dropped[0], "Billing") {
		t.Errorf("dropped = %v", b.Dropped)
	}
	if got := laneNameFor("feat/Billing/Seam_v2"); got != "feat/billing-seam_v2" {
		t.Errorf("laneNameFor = %q", got)
	}
}

func TestImportPlanAndApply(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(f.repo.Path, "AGENTS-COORD.md"), []byte(coordDoc), 0o644)
	wtRoot := filepath.Join(f.ws, "app-worktrees")
	add := func(name, branch string, commit bool) {
		dir := filepath.Join(wtRoot, name)
		gitT(t, f.repo.Path, "worktree", "add", "-q", "-b", branch, dir, "origin/main")
		if commit {
			f.commit(dir, name+".txt")
		}
	}
	add("billing", "feat/billing-seam", true) // matches a row, has work: import
	add("ci-old", "chore/ci-cache", false)    // matches, but nothing beyond main: finished
	add("stray", "fix/stray", true)           // no row claims it

	plan, err := f.fold.Import(ctx, f.repo.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	why := map[string]ImportItem{}
	for _, it := range plan.Items {
		why[it.Branch] = it
	}
	if it := why["feat/billing-seam"]; it.Action != "import" || it.Lane != "feat/billing-seam" || len(it.Scope) != 3 || !strings.Contains(it.Why, "not paths") {
		t.Errorf("billing = %+v", it)
	}
	if it := why["chore/ci-cache"]; it.Action != "skip" || !strings.Contains(it.Why, "fold gc") {
		t.Errorf("finished worktree = %+v", it)
	}
	if it := why["fix/stray"]; it.Action != "skip" || !strings.Contains(it.Why, "no row") {
		t.Errorf("unclaimed worktree = %+v", it)
	}
	if len(plan.Unclaimed) != 2 { // ci (its only worktree is finished) and idle
		t.Errorf("unclaimed rows = %v", plan.Unclaimed)
	}
	if lanes, _ := f.fold.Store.Lanes(ctx, f.repo.ID); len(lanes) != 0 {
		t.Error("a dry run created lanes")
	}

	plan, err = f.fold.Import(ctx, f.repo.ID, "", true)
	if err != nil || !plan.Applied {
		t.Fatal(err)
	}
	lanes, _ := f.fold.Store.Lanes(ctx, f.repo.ID)
	if len(lanes) != 1 || lanes[0].Branch != "feat/billing-seam" || !strings.HasSuffix(lanes[0].Worktree, "billing") {
		t.Fatalf("lanes = %+v", lanes)
	}
	// Running it again imports nothing new.
	plan, _ = f.fold.Import(ctx, f.repo.ID, "", true)
	for _, it := range plan.Items {
		if it.Action == "import" {
			t.Errorf("imported twice: %+v", it)
		}
	}
}

func TestViewIsInsertedOnceAndLeavesTheRestAlone(t *testing.T) {
	doc := "# Coordination\n\nIntro.\n\n## 6. Log\n\n- entry\n"
	v1 := viewBegin + "\nfirst\n" + viewEnd
	once := InsertView(doc, v1)
	if !strings.HasPrefix(once, "# Coordination\n\n"+viewBegin) || !strings.Contains(once, "- entry") {
		t.Fatalf("inserted:\n%s", once)
	}
	twice := InsertView(once, viewBegin+"\nsecond\n"+viewEnd)
	if strings.Count(twice, viewBegin) != 1 || strings.Contains(twice, "first") || !strings.Contains(twice, "second") || !strings.HasSuffix(twice, "- entry\n") {
		t.Errorf("replaced:\n%s", twice)
	}
	if got := InsertView("no title\n", v1); !strings.HasPrefix(got, viewBegin) {
		t.Errorf("no title: %q", got)
	}
}

func TestWriteViewOnlyWithACoordFile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	file := filepath.Join(f.repo.Path, "AGENTS-COORD.md")
	os.WriteFile(file, []byte(coordDoc), 0o644)
	f.open("feat/view")
	if err := f.fold.WriteView(ctx, f.repo.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); strings.Contains(string(b), viewBegin) {
		t.Error("wrote a view for a repo with no coord_file")
	}
	f.fold.Config, _ = config.Parse([]byte("repos:\n  app:\n    coord_file: AGENTS-COORD.md\n"))
	if _, err := f.fold.Reserve(ctx, f.repo.ID, 0, "minor"); err != nil {
		t.Fatal(err)
	}
	if err := f.fold.WriteView(ctx, f.repo.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	s := string(b)
	for _, want := range []string{viewBegin, "| feat/view | `feat/view` | by hand | `src/**`", "`v0.1.0` (no lane, reserved)", "- 2026-01-01 someone did something"} {
		if !strings.Contains(s, want) {
			t.Errorf("view lacks %q:\n%s", want, s)
		}
	}
}
