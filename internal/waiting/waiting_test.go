package waiting

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/store"
	"github.com/ubixsys/ubixshepherd/internal/store/sqlite"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// lane makes an open lane whose worktree is a real repo with `commits` commits beyond main.
func lane(t *testing.T, st store.Store, repo store.Repo, name string, commits int) store.Lane {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, dir, "checkout", "-q", "-b", name)
	for i := 0; i < commits; i++ {
		git(t, dir, "commit", "-q", "--allow-empty", "-m", "work")
	}
	l, err := st.CreateLane(context.Background(), store.Lane{RepoID: repo.ID, Name: name, Branch: name, Base: "main", Worktree: dir, State: store.LaneOpen})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCollectGathersWhatWaitsOnThePerson(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{
		{Name: "app", Path: filepath.Join(root, "app")},
		{Name: "tool", Path: filepath.Join(root, "tool")},
		{Name: "agentrepo", Path: filepath.Join(root, "agentrepo")},
	})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(ctx, ws.ID)
	byName := map[string]store.Repo{}
	for _, r := range repos {
		byName[r.Name] = r
	}
	conf := config.Default()
	conf.Repos = map[string]config.Profile{
		"app":       {Autonomy: config.Autonomy{Push: config.Shepherd}},
		"tool":      {Autonomy: config.Autonomy{Push: config.Human}},
		"agentrepo": {Autonomy: config.Autonomy{Push: config.Agent}},
	}

	// 1. An open decision, asked by a run in a lane.
	held := lane(t, st, byName["app"], "held", 0)
	run, err := st.CreateRun(ctx, store.Run{LaneID: held.ID, Agent: "claude", Prompt: "x", State: store.RunSucceeded, Log: filepath.Join(root, "r.log")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDecision(ctx, store.Decision{RunID: run.ID, Question: "Which\n  option?", State: store.DecisionOpen}); err != nil {
		t.Fatal(err)
	}

	// 2. A merge request held at require-approval with no approval; another approved; a
	// third whose pipeline failed elsewhere.
	put := func(l store.Lane, f store.LaneForge) {
		f.LaneID = l.ID
		if err := st.PutLaneForge(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	put(held, store.LaneForge{MR: 175, MRState: "opened", PipelineStatus: "blocked", RequireApproval: "manual", PipelineStarted: time.Now().Add(-time.Hour).UTC()})
	approved := lane(t, st, byName["app"], "approved", 0)
	put(approved, store.LaneForge{MR: 176, MRState: "opened", PipelineStatus: "blocked", RequireApproval: "manual", Approved: true})
	broken := lane(t, st, byName["agentrepo"], "broken", 0)
	put(broken, store.LaneForge{MR: 177, MRState: "opened", PipelineStatus: "failed", RequireApproval: "success"})
	merged := lane(t, st, byName["app"], "merged", 0)
	put(merged, store.LaneForge{MR: 178, MRState: "merged", PipelineStatus: "failed"})
	if _, _, err := st.UpsertArtifact(ctx, store.Artifact{LaneID: merged.ID, WorkspaceID: ws.ID, RepoID: byName["app"].ID, Path: "design.md", Intent: store.ArtifactUndecided, IntentBy: "default", State: store.ArtifactLive, Kind: store.ArtifactMarkdown, Source: store.ArtifactDeclared, Size: 1024}); err != nil {
		t.Fatal(err)
	}

	// 3. Lanes with unpushed work: shepherd pushes (ship), human pushes (open), agent
	// pushes (nothing waits).
	lane(t, st, byName["app"], "toship", 2)
	lane(t, st, byName["tool"], "tohuman", 1)
	lane(t, st, byName["agentrepo"], "byagent", 1)

	// 4. Next-up: proposed by the desk and planned waits; the person's own and an
	// approved one do not, nor does work the desk registered from chat.
	for _, it := range []store.NextItem{
		{Title: "Idea", Source: store.NextByDesk, State: store.NextPlanned},
		{Title: "Mine", Source: store.NextByPerson, State: store.NextPlanned},
		{Title: "Agreed in chat", Source: store.NextByChat, State: store.NextPlanned},
		{Title: "Ok", Source: store.NextByDesk, State: store.NextApproved},
	} {
		if _, err := st.AddNextItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	w, err := Collect(ctx, st, conf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Counts != (api.WaitingCounts{Decisions: 1, MRs: 2, Ship: 2, Next: 1, Artifacts: 1}) || w.Count != 7 || len(w.Items) != 7 {
		t.Fatalf("counts %+v count %d: %+v", w.Counts, w.Count, w.Items)
	}
	var approve, ship, push, failed *api.WaitingItem
	for i, it := range w.Items {
		switch {
		case it.Kind == api.WaitingMR && it.Action == "approve":
			approve = &w.Items[i]
		case it.Kind == api.WaitingMR:
			failed = &w.Items[i]
		case it.Kind == api.WaitingShip && it.Action == "ship":
			ship = &w.Items[i]
		case it.Kind == api.WaitingShip:
			push = &w.Items[i]
		}
	}
	if approve == nil || approve.MR != 175 || approve.Lane != "held" || approve.LaneID != held.ID {
		t.Errorf("approve item: %+v", approve)
	}
	if failed == nil || failed.MR != 177 || failed.Action != "open" {
		t.Errorf("failed item: %+v", failed)
	}
	if ship == nil || ship.Lane != "toship" || !strings.Contains(ship.Title, "2 commits") {
		t.Errorf("ship item: %+v", ship)
	}
	if push == nil || push.Lane != "tohuman" || push.Action != "open" {
		t.Errorf("push item: %+v", push)
	}

	line := w.Line()
	for _, want := range []string{"[Waiting on you (7)] ", "decision 1 (app:held): decision 1: Which option?", "MR !175 (app:held)", "next-up item 1", "artifacts (app:merged)"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Errorf("line is not one line: %q", line)
	}

	// Another workspace's lanes are left out when one is asked for; Next-up stays.
	other, err := Collect(ctx, st, conf, ws.ID+100)
	if err != nil {
		t.Fatal(err)
	}
	if other.Counts != (api.WaitingCounts{Next: 1}) {
		t.Errorf("other workspace: %+v", other.Counts)
	}
	if (api.Waiting{}).Line() != "" {
		t.Error("an empty list has a line")
	}
}

func TestMRBeingFixedIsNotWaiting(t *testing.T) {
	l := store.Lane{ID: 1, Name: "a"}
	lf := store.LaneForge{MR: 5, MRState: "opened", PipelineStatus: "failed"}
	if _, ok := mrItem(l, store.Repo{}, lf, true); ok {
		t.Error("a failed pipeline with a run fixing it waits on the person")
	}
	if it, ok := mrItem(l, store.Repo{}, lf, false); !ok || it.Action != "open" {
		t.Errorf("failed pipeline, nothing running: %+v %v", it, ok)
	}
}

func TestRequestsNeedingRoutingWaitAndVersionMoves(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	ws, err := st.SaveWorkspace(ctx, store.Workspace{Name: "ws", Path: root}, []store.Repo{{Name: "app", Path: filepath.Join(root, "app")}})
	if err != nil {
		t.Fatal(err)
	}
	repos, _ := st.Repos(ctx, ws.ID)
	l := lane(t, st, repos[0], "asker", 0)
	run, err := st.CreateRun(ctx, store.Run{LaneID: l.ID, Agent: "claude", Prompt: "x", State: store.RunSucceeded, Log: filepath.Join(root, "r.log")})
	if err != nil {
		t.Fatal(err)
	}
	conf := config.Default()

	empty, err := Collect(ctx, st, conf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Version == "" {
		t.Fatal("no version on an empty list")
	}
	again, _ := Collect(ctx, st, conf, 0)
	if again.Version != empty.Version {
		t.Errorf("version moved with no change: %s then %s", empty.Version, again.Version)
	}

	stuck, err := st.CreateRequest(ctx, store.Request{FromRun: run.ID, Kind: store.RequestQuestion, Message: "who owns\n the schema?", State: store.RequestNeedsRouting, Note: "no lane named"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRequest(ctx, store.Request{FromRun: run.ID, Kind: store.RequestQuestion, Message: "routed", State: store.RequestRouted}); err != nil {
		t.Fatal(err)
	}
	w, err := Collect(ctx, st, conf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Count != 1 || w.Counts.Requests != 1 || len(w.Items) != 1 {
		t.Fatalf("want one request: %+v", w)
	}
	it := w.Items[0]
	if it.Kind != api.WaitingRequest || it.Action != "route" || it.RequestID != stuck.ID || it.Lane != "asker" || it.Repo != "app" || it.Title != "who owns the schema?" {
		t.Errorf("item: %+v", it)
	}
	if w.Version == empty.Version {
		t.Error("version did not move when a request began to wait")
	}
	if !strings.Contains(w.Line(), "request 1 (app:asker)") {
		t.Errorf("line: %s", w.Line())
	}
	if other, _ := Collect(ctx, st, conf, ws.ID+100); other.Counts.Requests != 0 {
		t.Errorf("another workspace's request counted: %+v", other.Counts)
	}

	// Closing it clears it and moves the version again.
	stuck.State = store.RequestClosed
	if err := st.UpdateRequest(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	after, _ := Collect(ctx, st, conf, 0)
	if after.Count != 0 || after.Version != empty.Version {
		t.Errorf("after close: %+v (version %s, empty %s)", after, after.Version, empty.Version)
	}
}
