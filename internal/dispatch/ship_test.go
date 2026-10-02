package dispatch

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

type shipForge struct {
	mu      sync.Mutex
	created []string
}

func (f *shipForge) Name() string                                           { return "fake" }
func (f *shipForge) MRForBranch(context.Context, string) (*forge.MR, error) { return nil, nil }
func (f *shipForge) FailedJobs(context.Context, int64) ([]forge.Job, error) { return nil, nil }
func (f *shipForge) JobLog(context.Context, int64, int) (string, error)     { return "", nil }
func (f *shipForge) CreateMR(_ context.Context, source, target, title, body string) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, source+" -> "+target+": "+title+"\n"+body)
	return &forge.MR{IID: 42, State: "opened", URL: "https://gl/mr/42"}, nil
}

func shipFixture(t *testing.T, mode, profile string) (*fixture, *shipForge) {
	t.Helper()
	f := newFixture(t, mode)
	cfg, err := config.Parse([]byte(profile))
	if err != nil {
		t.Fatal(err)
	}
	f.runner.Config = cfg
	sf := &shipForge{}
	f.runner.ForgeFor = func(string) (forge.Forge, error) { return sf, nil }
	return f, sf
}

const pushes = "repos:\n  app:\n    gate: \"true\"\n    autonomy:\n      push: shepherd\n"

func originHas(t *testing.T, f *fixture, branch string) bool {
	out, _ := exec.Command("git", "-C", f.origin, "branch", "--list", branch).Output()
	return strings.TrimSpace(string(out)) != ""
}

// waitFeed waits for a feed line containing s.
func waitFeed(t *testing.T, f *fixture, s string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		items, _ := f.st.Feed(context.Background(), 0, 500)
		for _, it := range items {
			if strings.Contains(it.Text, s) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	items, _ := f.st.Feed(context.Background(), 0, 500)
	var all []string
	for _, it := range items {
		all = append(all, it.Text)
	}
	t.Fatalf("no feed line with %q in:\n%s", s, strings.Join(all, "\n"))
}

func TestShipPushesAndOpensTheMR(t *testing.T) {
	f, sf := shipFixture(t, "ok", pushes)
	run, err := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t, run.ID)
	waitFeed(t, f, "opened !42 for you to review")
	if !originHas(t, f, "work") {
		t.Error("the lane was not pushed")
	}
	if len(sf.created) != 1 || !strings.HasPrefix(sf.created[0], "work -> main: agent work") || !strings.Contains(sf.created[0], "The gate, `true`, passed") {
		t.Errorf("created = %q", sf.created)
	}
}

func TestShipHandsAFailedGateBack(t *testing.T) {
	f, sf := shipFixture(t, "ok", strings.Replace(pushes, `gate: "true"`, `gate: "echo gate says no; exit 1"`, 1))
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	waitFeed(t, f, "asked claude to fix it")
	if originHas(t, f, "work") || len(sf.created) != 0 {
		t.Error("pushed although the gate failed")
	}
	runs, _ := f.st.Runs(context.Background(), f.lane.ID, "", 5)
	if !strings.Contains(runs[0].Prompt, "gate says no") || runs[0].Parent != run.ID {
		t.Errorf("fix run = %+v", runs[0])
	}
	f.wait(t, runs[0].ID)
	// The fix run commits again and the gate fails again: the second and last try.
	waitFeed(t, f, "try 2 of 2")
}

func TestShipOnlyWhenOptedInAndFinished(t *testing.T) {
	f, sf := shipFixture(t, "ok", "repos:\n  app:\n    gate: \"true\"\n")
	run, _ := f.runner.Start(context.Background(), StartRequest{LaneID: f.lane.ID, Agent: "claude", Prompt: "work"})
	f.wait(t, run.ID)
	time.Sleep(300 * time.Millisecond)
	if originHas(t, f, "work") || len(sf.created) != 0 {
		t.Error("pushed for a repo that is not opted in")
	}

	g, gf := shipFixture(t, "stray", pushes)
	run, _ = g.runner.Start(context.Background(), StartRequest{LaneID: g.lane.ID, Agent: "claude", Prompt: "work"})
	g.wait(t, run.ID)
	waitFeed(t, g, "changed files outside its scope")
	if originHas(t, g, "work") || len(gf.created) != 0 {
		t.Error("pushed work outside the scope")
	}

	h, hf := shipFixture(t, "ok", pushes)
	run, _ = h.runner.Start(context.Background(), StartRequest{LaneID: h.lane.ID, Agent: "claude", Prompt: "work"})
	h.runner.Ask(context.Background(), store.Decision{RunID: run.ID, Question: "which?", Recommendation: "a"})
	h.wait(t, run.ID)
	time.Sleep(300 * time.Millisecond)
	if originHas(t, h, "work") || len(hf.created) != 0 {
		t.Error("pushed while the agent waits on a decision")
	}
}
