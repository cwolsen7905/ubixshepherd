package dispatch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

const widens = "repos:\n  app:\n    widen: auto\n"

// widenFixture is a lane whose scope is the single file src/a.go, with a.go committed
// and the runner able to widen through a real Fold.
func widenFixture(t *testing.T, profile string) *fixture {
	t.Helper()
	f, _ := shipFixture(t, "ok", profile)
	ctx := context.Background()
	fl := &fold.Fold{Store: f.st, Config: config.Default()}
	f.runner.Rescope = fl.Rescope
	lane, err := fl.Rescope(ctx, f.lane.ID, []string{"src/a.go"}, []string{"src/**"})
	if err != nil {
		t.Fatal(err)
	}
	f.lane = lane
	commitInLane(t, f, "src/a.go", "feat: a")
	return f
}

func laneScope(t *testing.T, f *fixture) []string {
	t.Helper()
	l, err := f.st.Lane(context.Background(), f.lane.ID)
	if err != nil {
		t.Fatal(err)
	}
	return l.Scope
}

func TestAutoWidenAfterARunEndsOutside(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	commitInLane(t, f, "src/b.go", "feat: b")
	run := endedRun(t, f)
	run.Outside = []string{"src/b.go"}
	if !f.runner.autoWidenOutside(ctx, run, f.lane) {
		t.Fatal("did not widen and continue")
	}
	if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go", "src/b.go"}) {
		t.Errorf("scope = %v", got)
	}
	waitFeed(t, f, "lane work scope auto-widened: +src/b.go (run ")
	waitFeed(t, f, "checks passed: repo opts in, exact files")
	runs, _ := f.st.Runs(ctx, f.lane.ID, "", 5)
	var told bool
	for _, r := range runs {
		told = told || strings.Contains(r.Prompt, "scope now includes src/b.go")
	}
	if !told {
		t.Errorf("no continued run told the agent; runs: %+v", runs)
	}
}

func TestAutoWidenFallsBack(t *testing.T) {
	cases := []struct {
		name, profile, file string
	}{
		{"human repo", "repos:\n  app:\n    widen: human\n", "src/b.go"},
		{"unset", pushes, "src/b.go"},
		{"far away", widens, "other/b.go"},
		{"protected", widens, "src/go.mod"},
		{"repo protected", widens + "    widen_protected: [\"src/b.go\"]\n", "src/b.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := widenFixture(t, tc.profile)
			ctx := context.Background()
			commitInLane(t, f, tc.file, "feat: b")
			run := endedRun(t, f)
			run.Outside = []string{tc.file}
			if f.runner.autoWidenOutside(ctx, run, f.lane) {
				t.Fatal("widened")
			}
			if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go"}) {
				t.Errorf("scope = %v", got)
			}
			if strings.Contains(f.feedText(t), "auto-widened") {
				t.Error("fell back but recorded an auto-widen")
			}
		})
	}
}

func TestAutoWidenWaitsForOtherLanes(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	repo, _ := f.st.Repo(ctx, f.lane.RepoID)
	other := &fold.Fold{Store: f.st, Config: config.Default()}
	if _, err := other.Open(ctx, fold.OpenRequest{RepoID: repo.ID, Name: "other", Scope: []string{"src/b.go"}}); err != nil {
		t.Fatal(err)
	}
	commitInLane(t, f, "src/b.go", "feat: b")
	run := endedRun(t, f)
	run.Outside = []string{"src/b.go"}
	if f.runner.autoWidenOutside(ctx, run, f.lane) {
		t.Error("widened into another open lane's scope")
	}
}

func TestAutoWidenCap(t *testing.T) {
	f := widenFixture(t, widens+"    widen_cap: 2\n")
	ctx := context.Background()
	for _, name := range []string{"b", "c", "d"} {
		file := "src/" + name + ".go"
		commitInLane(t, f, file, "feat: "+name)
		run := endedRun(t, f)
		lane, _ := f.st.Lane(ctx, f.lane.ID)
		_, _, _, got := f.runner.autoWiden(ctx, run, lane, []string{file})
		if want := name != "d"; got != want {
			t.Fatalf("%s: widened = %v, want %v\n%s", name, got, want, allFeed(f))
		}
	}
	if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go", "src/b.go", "src/c.go"}) {
		t.Errorf("scope = %v", got)
	}
}

func TestAutoWidenAScopeDecision(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	run := endedRun(t, f)
	d, err := f.runner.Ask(ctx, store.Decision{RunID: run.ID, Question: "May I widen the scope to include `src/b.go`? Run `make check` after."})
	if err != nil {
		t.Fatal(err)
	}
	if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go", "src/b.go"}) {
		t.Errorf("scope = %v", got)
	}
	got, _ := f.st.Decision(ctx, d.ID)
	if got.State != store.DecisionAnswered || !strings.Contains(got.Answer, "not by the person") {
		t.Errorf("decision = %+v", got)
	}
	// A question with options offers a choice, so it waits for the person.
	opt, _ := f.runner.Ask(ctx, store.Decision{RunID: run.ID, Question: "Widen the scope to `src/c.go`?", Options: []string{"yes", "revert it"}})
	if got, _ := f.st.Decision(ctx, opt.ID); got.State != store.DecisionOpen {
		t.Errorf("a question with options was answered: %s", got.State)
	}
	// A question that names no file, or a protected one, waits for the person.
	for _, q := range []string{"Should the scope grow?", "Widen the scope to `go.mod` too?", "Use `make check`?"} {
		d, _ := f.runner.Ask(ctx, store.Decision{RunID: run.ID, Question: q})
		if got, _ := f.st.Decision(ctx, d.ID); got.State != store.DecisionOpen {
			t.Errorf("%q: state %s, want open", q, got.State)
		}
	}
}

func TestAutoWidenInTheMergeRequest(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	commitInLane(t, f, "src/b.go", "feat: b")
	run := endedRun(t, f)
	run.Outside = []string{"src/b.go"}
	f.runner.autoWidenOutside(ctx, run, f.lane)
	_, body := f.runner.mrText(ctx, f.lane, "true", "work")
	if !strings.Contains(body, "Scope widened by Shepherd's rule") || !strings.Contains(body, "+src/b.go") {
		t.Errorf("body = %s", body)
	}
}

func TestScopeFiles(t *testing.T) {
	for q, want := range map[string][]string{
		"Widen scope to `a/b.go` and `c.go`?":                        {"a/b.go", "c.go"},
		"Widen scope to `Makefile`?":                                 nil,
		"Widen scope: `rm -rf /`":                                    nil,
		"`src/b.go` is outside the scope; should I revert it?":       nil,
		"Widen the scope to `a/b.go`, or move the change elsewhere?": nil,
		"The change touches `a/b.go`, outside the scope. Widen it?":  {"a/b.go"},
		"Edit `a/b.go`?":           nil,
		"Scope: `internal/x/*.go`": nil,
	} {
		got := scopeFiles(store.Decision{Question: q})
		if !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", q, got, want)
		}
	}
}

func allFeed(f *fixture) string {
	items, _ := f.st.Feed(context.Background(), 0, 500)
	var all []string
	for _, it := range items {
		all = append(all, it.Text)
	}
	return strings.Join(all, "\n")
}

func TestAutoWidenUndoneWhenTheAgentCannotBeTold(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	commitInLane(t, f, "src/b.go", "feat: b")
	run := endedRun(t, f)
	run.Outside = []string{"src/b.go"}
	f.runner.lookPath = func(string) (string, error) { return "", errors.New("no agent") }
	if f.runner.autoWidenOutside(ctx, run, f.lane) {
		t.Fatal("reported a continued run that did not start")
	}
	waitFeed(t, f, "the auto-widen of src/b.go was undone")
	if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go"}) {
		t.Errorf("scope = %v", got)
	}
}

func TestWidenCountSurvivesARestart(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	commitInLane(t, f, "src/b.go", "feat: b")
	if _, _, _, ok := f.runner.autoWiden(ctx, endedRun(t, f), f.lane, []string{"src/b.go"}); !ok {
		t.Fatal("did not widen")
	}
	if n := f.runner.widenCount(ctx, f.lane); n != 1 {
		t.Errorf("count = %d", n)
	}
	f.runner.widens = nil // a new process reads the feed again
	if n := f.runner.widenCount(ctx, f.lane); n != 1 {
		t.Errorf("count after restart = %d", n)
	}
}

func TestAutoWidenPartialNamesWhatItLeftOut(t *testing.T) {
	f := widenFixture(t, widens)
	ctx := context.Background()
	commitInLane(t, f, "src/b.go", "feat: b")
	commitInLane(t, f, "src/.env", "feat: env")
	run := endedRun(t, f)
	run.Outside = []string{"src/b.go", "src/.env"}
	if f.runner.autoWidenOutside(ctx, run, f.lane) {
		t.Fatal("carried the lane on with a protected file still outside scope")
	}
	if got := laneScope(t, f); !slices.Equal(got, []string{"src/a.go", "src/b.go"}) {
		t.Errorf("scope = %v", got)
	}
	waitFeed(t, f, "left out, needs an explicit widen: src/.env is protected (**/.env: environment file)")
	runs, _ := f.st.Runs(ctx, f.lane.ID, "", 5)
	for _, r := range runs {
		if strings.Contains(r.Prompt, "scope now includes") {
			t.Errorf("continued the run: %q", r.Prompt)
		}
	}
}

func TestWidenNoteNamesRefusedFiles(t *testing.T) {
	lane := store.Lane{Scope: []string{"src/a.go", "src/b.go"}}
	if got := widenNote([]string{"src/b.go"}, lane, ""); strings.Contains(got, "Not widened") {
		t.Errorf("note without refusals mentions them: %q", got)
	}
	got := widenNote([]string{"src/b.go"}, lane, "src/.env is protected (**/.env: environment file)")
	for _, want := range []string{"Not widened", "src/.env is protected", "explicit widen"} {
		if !strings.Contains(got, want) {
			t.Errorf("note %q lacks %q", got, want)
		}
	}
}
