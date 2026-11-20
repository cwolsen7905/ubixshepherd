package fold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func (f *fixture) addItem(title, plan, state string, planBody string) store.NextItem {
	f.t.Helper()
	if plan != "" {
		p := filepath.Join(f.repo.Path, plan)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(planBody), 0o644); err != nil {
			f.t.Fatal(err)
		}
		// Plans are read as committed at the lane's start, so land it on origin.
		gitT(f.t, f.repo.Path, "add", plan)
		gitT(f.t, f.repo.Path, "commit", "-q", "-m", "plan "+plan)
		gitT(f.t, f.repo.Path, "push", "-q", "origin", "HEAD:main")
	}
	it, err := f.fold.Store.AddNextItem(context.Background(), store.NextItem{Title: title, Repo: f.repo.Name, Plan: plan, State: state})
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}

func warnings(o Opened) string {
	var w []string
	for _, n := range o.Notes {
		if strings.HasPrefix(n, "warning:") {
			w = append(w, n)
		}
	}
	return strings.Join(w, "\n")
}

func TestOpenWarnsWhenScopeBlocksApprovedItem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	it := f.addItem("rework the cache", "plans/cache.md", store.NextApproved, "# Cache\n\nScope: src/cache/**, docs/cache.md\n")
	planned := f.addItem("planned only", "plans/p.md", store.NextPlanned, "Scope: src/**\n")
	noScope := f.addItem("no scope data", "plans/n.md", store.NextApproved, "nothing here\n")
	noPlan := f.addItem("no plan", "", store.NextApproved, "")
	_, _, _ = planned, noScope, noPlan

	// Overlap: succeeds, warns, names the item and both globs.
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "wide", Scope: []string{"src/**"}})
	if err != nil {
		t.Fatalf("a warning must not refuse: %v", err)
	}
	w := warnings(o)
	if !strings.Contains(w, "src/cache/**") || !strings.Contains(w, "src/**") || !strings.Contains(w, "rework the cache") {
		t.Errorf("warning = %q", w)
	}
	if strings.Count(w, "warning:") != 1 {
		t.Errorf("only the approved item with scope data should be named: %q", w)
	}

	// Disjoint: no warning.
	o, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "elsewhere", Scope: []string{"docs/other.md"}})
	if err != nil || warnings(o) != "" {
		t.Errorf("disjoint: %v %q", err, warnings(o))
	}

	// The item's own lane (by id) is not a conflict; nor is one that has a lane.
	f2 := newFixture(t)
	own := f2.addItem("own", "plans/own.md", store.NextApproved, "Scope: src/own/**\n")
	o, err = f2.fold.Open(ctx, OpenRequest{RepoID: f2.repo.ID, Name: "own-lane", Scope: []string{"src/own/**"}, ForItem: own.ID})
	if err != nil || warnings(o) != "" {
		t.Errorf("own item: %v %q", err, warnings(o))
	}
	_ = it
}

func TestOpenNoWarningOnceItemHasLane(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	it := f.addItem("linked", "plans/l.md", store.NextApproved, "Scope: src/**\n")
	l := f.open("holder") // scope src/**; warns, ok
	if _, err := f.fold.Store.UpdateNextItem(ctx, it.ID, store.NextPatch{LaneID: &l.ID}); err != nil {
		t.Fatal(err)
	}
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "other", Scope: []string{"docs/**"}})
	if err != nil || warnings(o) != "" {
		t.Errorf("%v %q", err, warnings(o))
	}
}

func TestPlanScopeReadFromStartNotWorkingCopy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.addItem("committed", "plans/c.md", store.NextApproved, "Scope: src/a/**\n")
	// An uncommitted edit to the plan must not change the warning.
	os.WriteFile(filepath.Join(f.repo.Path, "plans/c.md"), []byte("Scope: docs/**\n"), 0o644)
	o, err := f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "x", Scope: []string{"docs/**"}})
	if err != nil || warnings(o) != "" {
		t.Errorf("working-copy edit used: %v %q", err, warnings(o))
	}
	o, err = f.fold.Open(ctx, OpenRequest{RepoID: f.repo.ID, Name: "y", Scope: []string{"src/a/y/**"}})
	if err != nil || warnings(o) == "" {
		t.Errorf("committed scope missed: %v %q", err, warnings(o))
	}
}

func TestOpenWithExactPlanScopeIsNotBlocking(t *testing.T) {
	f := newFixture(t)
	f.addItem("drive item", "plans/d.md", store.NextApproved, "Scope: src/d/**, docs/d.md\n")
	// The keep-going drive opens with the plan's scope and no item id.
	o, err := f.fold.Open(context.Background(), OpenRequest{RepoID: f.repo.ID, Name: "item-1-drive", Scope: []string{"docs/d.md", "src/d/**"}})
	if err != nil || warnings(o) != "" {
		t.Errorf("%v %q", err, warnings(o))
	}
}
