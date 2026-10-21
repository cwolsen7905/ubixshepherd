package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func checkRepo(t *testing.T, db *DB) int64 {
	t.Helper()
	ws, err := db.SaveWorkspace(context.Background(), store.Workspace{Name: "w", Path: "/w"}, []store.Repo{{Name: "a", Path: "/w/a"}})
	if err != nil {
		t.Fatal(err)
	}
	repos, err := db.Repos(context.Background(), ws.ID)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos: %v %v", repos, err)
	}
	return repos[0].ID
}

func TestCheckFiles(t *testing.T) {
	db, path := open(t)
	ctx := context.Background()
	repo := checkRepo(t, db)

	got, err := db.ApproveCheckFiles(ctx, []store.CheckFile{
		{RepoID: repo, Path: "t/a_test.go", Blob: "b1", BaseCommit: "c1", ItemID: 7, Set: "core", ApprovedBy: "person (cli)"},
		{RepoID: repo, Path: "t/b_test.go", Blob: "b2", BaseCommit: "c1", ApprovedBy: "person (cli)"},
	})
	if err != nil || len(got) != 2 || got[0].ID == 0 || got[0].Approved.IsZero() || !got[0].Live() || got[0].ItemID != 7 || got[0].Set != "core" {
		t.Fatalf("approve: %+v %v", got, err)
	}

	// The unique live index, and the all-or-none approve.
	_, err = db.ApproveCheckFiles(ctx, []store.CheckFile{
		{RepoID: repo, Path: "t/c_test.go", Blob: "b3", BaseCommit: "c1", ApprovedBy: "person (cli)"},
		{RepoID: repo, Path: "t/a_test.go", Blob: "b9", BaseCommit: "c2", ApprovedBy: "person (cli)"},
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate live = %v", err)
	}
	if live, _ := db.CheckFiles(ctx, repo, true); len(live) != 2 {
		t.Fatalf("a refused approve left rows: %+v", live)
	}
	// The index itself refuses a duplicate that gets past the check.
	if _, err := db.db.ExecContext(ctx, `INSERT INTO check_files (repo_id, path, blob, base_commit, approved, approved_by) VALUES (?, 't/a_test.go', 'x', 'y', 'z', 'p')`, repo); err == nil {
		t.Error("the unique live index accepted a duplicate")
	}
	if _, err := db.ApproveCheckFiles(ctx, []store.CheckFile{{RepoID: repo, Path: "x"}}); err == nil {
		t.Error("an incomplete check file was accepted")
	}

	// Release, all or none.
	if _, err := db.ReleaseCheckFiles(ctx, repo, []string{"t/a_test.go", "t/none_test.go"}, "wrong"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("release of an unmarked path = %v", err)
	}
	if live, _ := db.CheckFiles(ctx, repo, true); len(live) != 2 {
		t.Fatalf("a refused release changed rows: %+v", live)
	}
	rel, err := db.ReleaseCheckFiles(ctx, repo, []string{"t/a_test.go"}, "the check is wrong")
	if err != nil || len(rel) != 1 || rel[0].Live() || rel[0].ReleaseWhy != "the check is wrong" {
		t.Fatalf("release: %+v %v", rel, err)
	}
	if live, _ := db.CheckFiles(ctx, repo, true); len(live) != 1 || live[0].Path != "t/b_test.go" {
		t.Errorf("live after release: %+v", live)
	}
	if all, _ := db.CheckFiles(ctx, repo, false); len(all) != 2 {
		t.Errorf("history: %+v", all)
	}

	// Re-approve after a release is a new row.
	again, err := db.ApproveCheckFiles(ctx, []store.CheckFile{{RepoID: repo, Path: "t/a_test.go", Blob: "b4", BaseCommit: "c3", ApprovedBy: "person (web)"}})
	if err != nil || again[0].ID == got[0].ID {
		t.Fatalf("re-approve: %+v %v", again, err)
	}
	all, _ := db.CheckFiles(ctx, repo, false)
	if len(all) != 3 || all[0].Blob != "b4" {
		t.Fatalf("all: %+v", all)
	}

	// Round trip through a fresh open of the same database.
	db.Close()
	db2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	back, _ := db2.CheckFiles(ctx, repo, false)
	if len(back) != 3 || back[2].Set != "core" || back[2].Released == nil || back[0].BaseCommit != "c3" {
		t.Errorf("after reopen: %+v", back)
	}
	// Another repo sees none.
	if other, _ := db2.CheckFiles(ctx, repo+1, false); len(other) != 0 {
		t.Errorf("other repo: %+v", other)
	}
}
