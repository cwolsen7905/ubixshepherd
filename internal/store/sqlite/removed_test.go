package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestRemovedRepoMigrationApplies(t *testing.T) {
	ctx := context.Background()
	db, path := open(t)
	ws, _ := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "a", Path: "/w/a"}})
	// Put the database back to the version before the migration, as an older daemon left it.
	for _, c := range []string{"approved", "require_approval"} {
		if _, err := db.db.ExecContext(ctx, `ALTER TABLE lane_forge DROP COLUMN `+c); err != nil {
			t.Fatal(err)
		}
	}
	for _, tbl := range []string{"keep_going", "keep_going_starts"} {
		if _, err := db.db.ExecContext(ctx, `DROP TABLE `+tbl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE gate_results`); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"topic_links", "topics"} {
		if _, err := db.db.ExecContext(ctx, `DROP TABLE `+tbl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE lane_forge DROP COLUMN threads_seen`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE desk_questions`); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"runs", "usage"} {
		if _, err := db.db.ExecContext(ctx, `ALTER TABLE `+tbl+` DROP COLUMN cost_estimated`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE next_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE repos DROP COLUMN removed`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)-10)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	repos, err := db.Repos(ctx, ws.ID)
	if err != nil || len(repos) != 1 || repos[0].Removed != nil {
		t.Fatalf("repos after migration = %+v, %v", repos, err)
	}
}

func TestRemovedRepoIsHiddenAndRestored(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	ws, _ := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"},
		[]store.Repo{{Name: "a", Path: "/w/a"}, {Name: "b", Path: "/w/b"}})
	repos, _ := db.Repos(ctx, ws.ID)
	a := repos[0]
	lane, err := db.CreateLane(ctx, store.Lane{RepoID: a.ID, Name: "l", Branch: "l", Worktree: "/wt/l", State: store.LaneClosed})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, store.Run{LaneID: lane.ID, Agent: "claude", State: store.RunSucceeded})
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.SetRepoRemoved(ctx, a.ID, &at); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Repos(ctx, ws.ID); len(got) != 1 || got[0].Name != "b" {
		t.Errorf("Repos = %+v, want only b", got)
	}
	all, _ := db.AllRepos(ctx, ws.ID)
	if len(all) != 2 || all[0].Removed == nil || !all[0].Removed.Equal(at) || all[1].Removed != nil {
		t.Errorf("AllRepos = %+v", all)
	}
	// History is still read: the repo by id, its lane and its run.
	if r, err := db.Repo(ctx, a.ID); err != nil || r.Removed == nil {
		t.Errorf("Repo = %+v, %v", r, err)
	}
	if _, err := db.Lane(ctx, lane.ID); err != nil {
		t.Errorf("lane: %v", err)
	}
	if _, err := db.Run(ctx, run.ID); err != nil {
		t.Errorf("run: %v", err)
	}

	// Saving the repo again manages it again, as the same row.
	if _, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "a", Path: "/w/a"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Repos(ctx, ws.ID)
	if len(got) != 2 || got[0].ID != a.ID || got[0].Removed != nil {
		t.Errorf("after restore = %+v", got)
	}

	if err := db.SetRepoRemoved(ctx, 9999, &at); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown repo: %v", err)
	}
}
