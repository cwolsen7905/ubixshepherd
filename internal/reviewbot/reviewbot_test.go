package reviewbot

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

type fakeForge struct {
	mrs   []forge.MRSummary
	more  bool
	disc  map[int][]forge.Discussion
	fail  map[int]error
	calls *int
}

func (f fakeForge) Name() string { return "fake" }
func (f fakeForge) MRForBranch(context.Context, string) (*forge.MR, error) {
	return nil, nil
}
func (f fakeForge) FailedJobs(context.Context, int64) ([]forge.Job, error) { return nil, nil }
func (f fakeForge) JobLog(context.Context, int64, int) (string, error)     { return "", nil }
func (f fakeForge) CreateMR(context.Context, string, string, string, string) (*forge.MR, error) {
	return nil, nil
}
func (f fakeForge) RefPipeline(context.Context, string) (*forge.Pipeline, error) { return nil, nil }
func (f fakeForge) MergeRequests(context.Context, time.Time, int) ([]forge.MRSummary, bool, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.mrs, f.more, nil
}
func (f fakeForge) Discussions(_ context.Context, mr int) ([]forge.Discussion, error) {
	return f.disc[mr], f.fail[mr]
}

func note(author, text string) forge.Note { return forge.Note{Author: author, Text: text} }

var bot = config.ReviewBot{Name: "Reviewer", User: "review-bot", LabelPrefix: "bot::", Projects: []string{"app", "lib", "gone", "plain"}}

func fixture() fakeForge {
	return fakeForge{
		mrs: []forge.MRSummary{
			{IID: 1, Title: "fixed", State: "merged", Labels: []string{"bot::pushed"}},
			{IID: 2, Title: "proposed", State: "opened", Labels: []string{"bot::proposed", "x"}},
			{IID: 3, Title: "stuck", State: "opened", URL: "u3", Labels: []string{"bot::needs-you"}},
			{IID: 4, Title: "unrelated", State: "opened", Labels: []string{"bug"}},
			{IID: 5, Title: "note only", State: "opened"},
			{IID: 6, Title: "old stuck", State: "merged", Labels: []string{"bot::needs-you"}},
			{IID: 7, Title: "running", State: "opened", Labels: []string{"bot::working"}},
		},
		disc: map[int][]forge.Discussion{
			1: {
				{ID: "a", Resolvable: true, Resolved: true, ResolvedBy: "review-bot", Notes: []forge.Note{note("review-bot", "finding")}},
				{ID: "b", Notes: []forge.Note{note("review-bot", "<!-- ai-review:summary round=1 -->\nDone.\n\n_Model `x/y`, effort default, 12.5k in / 1.4k out, $0.0022 (estimate), on `abc`._")}},
			},
			2: {
				{ID: "a", Resolvable: true, Resolved: true, ResolvedBy: "sam", Notes: []forge.Note{note("review-bot", "finding"), note("sam", "no")}},
				{ID: "b", Resolvable: true, Resolved: true, ResolvedBy: "sam", Notes: []forge.Note{note("sam", "mine")}},
				{ID: "c", Resolvable: true, Resolved: false, Notes: []forge.Note{note("review-bot", "open")}},
				{ID: "d", Notes: []forge.Note{note("review-bot", "Model `m` and later $9.99 is quoted")}},
			},
			4: {{ID: "a", Notes: []forge.Note{note("sam", "hello")}}},
			5: {{ID: "a", Notes: []forge.Note{note("Review-Bot", "_Model `m`, effort high, 1k in / 1k out, $0.0100 (estimate), on `d`._")}}},
		},
	}
}

func TestReport(t *testing.T) {
	bad := fakeForge{mrs: []forge.MRSummary{{IID: 1, Labels: []string{"bot::pushed"}}}, fail: map[int]error{1: errors.New("boom")}}
	c := &Collector{Forge: func(repo string) (forge.Forge, error) {
		switch repo {
		case "app":
			return fixture(), nil
		case "lib":
			return bad, nil
		case "plain":
			return plainOnly{}, nil
		}
		return nil, errors.New("no repo " + repo)
	}}
	rep := c.Report(context.Background(), []config.ReviewBot{bot}, false)
	if len(rep.Bots) != 1 || len(rep.Bots[0].Projects) != 4 {
		t.Fatalf("report = %+v", rep)
	}
	app := rep.Bots[0].Projects[0]
	want := Counts{Jobs: 6, Working: 1, Proposals: 1, Dismissals: 1, NeedsYou: 2, Fixes: 1, Stamped: 2}
	got := app.Counts
	if math.Abs(got.SpendUSD-0.0122) > 1e-9 {
		t.Errorf("spend = %v", got.SpendUSD)
	}
	got.SpendUSD = 0
	if got != want {
		t.Errorf("app = %+v, want %+v", got, want)
	}
	if len(app.NeedsYouItems) != 1 || app.NeedsYouItems[0].IID != 3 || app.NeedsYouItems[0].URL != "u3" {
		t.Errorf("needs you items = %+v (a merged request is not waiting)", app.NeedsYouItems)
	}
	if lib := rep.Bots[0].Projects[1]; lib.Error == "" || lib.Jobs != 0 {
		t.Errorf("lib = %+v", lib)
	}
	if gone := rep.Bots[0].Projects[2]; gone.Error != "no repo gone" {
		t.Errorf("gone = %+v", gone)
	}
	if plain := rep.Bots[0].Projects[3]; plain.Error == "" {
		t.Errorf("plain = %+v", plain)
	}
	if tot := rep.Bots[0].Total; tot.Jobs != 6 || tot.Fixes != 1 {
		t.Errorf("total = %+v", tot)
	}
}

// plainOnly is a Forge without MRLister.
type plainOnly struct{}

func (plainOnly) Name() string { return "plain" }
func (plainOnly) MRForBranch(context.Context, string) (*forge.MR, error) {
	return nil, nil
}
func (plainOnly) FailedJobs(context.Context, int64) ([]forge.Job, error) { return nil, nil }
func (plainOnly) JobLog(context.Context, int64, int) (string, error)     { return "", nil }
func (plainOnly) CreateMR(context.Context, string, string, string, string) (*forge.MR, error) {
	return nil, nil
}
func (plainOnly) RefPipeline(context.Context, string) (*forge.Pipeline, error) { return nil, nil }

func TestCacheAndTruncation(t *testing.T) {
	calls := 0
	f := fixture()
	f.calls, f.more = &calls, true
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	c := &Collector{Forge: func(string) (forge.Forge, error) { return f, nil }, Now: func() time.Time { return now }, TTL: time.Minute}
	one := config.ReviewBot{Name: "A", User: "review-bot", LabelPrefix: "bot::", Projects: []string{"app"}}
	rep := c.Report(context.Background(), []config.ReviewBot{one}, false)
	if !rep.Bots[0].Projects[0].Truncated {
		t.Error("truncated not set")
	}
	c.Report(context.Background(), []config.ReviewBot{one}, false)
	if calls != 1 {
		t.Errorf("a second read inside the TTL read the forge again: %d", calls)
	}
	c.Report(context.Background(), []config.ReviewBot{one}, true)
	if calls != 2 {
		t.Errorf("refresh did not read: %d", calls)
	}
	now = now.Add(2 * time.Minute)
	c.Report(context.Background(), []config.ReviewBot{one}, false)
	if calls != 3 {
		t.Errorf("an old report was reused: %d", calls)
	}
	changed := one
	changed.User = "other"
	c.Report(context.Background(), []config.ReviewBot{changed}, false)
	if calls != 4 {
		t.Errorf("a changed configuration reused the cache: %d", calls)
	}
}

func TestStampedCost(t *testing.T) {
	for text, want := range map[string]float64{
		"_Model `a/b`, effort x, 1k in / 2k out, $0.0022 (OpenCode estimate), on `abc`._": 0.0022,
		"_Model `a`, $12 total_":             12,
		"Model `a`, $12 total":               -1,
		"cost was $5":                        -1,
		"_No model call on `abc`: no cost._": -1,
	} {
		v, ok := stampedCost(text)
		if want < 0 && ok || want >= 0 && (!ok || v != want) {
			t.Errorf("%q = %v, %v", text, v, ok)
		}
	}
}
