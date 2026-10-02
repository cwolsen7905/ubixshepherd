// Package sqlite implements store.Store on SQLite through a pure-Go driver, so the binary
// cross-compiles with CGO_ENABLED=0.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// migrations run in order; each runs once. Append, never edit.
var migrations = []string{
	`CREATE TABLE workspaces (
		id      INTEGER PRIMARY KEY,
		name    TEXT NOT NULL UNIQUE,
		path    TEXT NOT NULL UNIQUE,
		created TEXT NOT NULL
	);
	CREATE TABLE repos (
		id           INTEGER PRIMARY KEY,
		workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
		name         TEXT NOT NULL,
		path         TEXT NOT NULL UNIQUE,
		remote       TEXT NOT NULL DEFAULT '',
		stacks       TEXT NOT NULL DEFAULT '[]',
		created      TEXT NOT NULL,
		UNIQUE (workspace_id, name)
	);
	CREATE TABLE lanes (
		id       INTEGER PRIMARY KEY,
		repo_id  INTEGER NOT NULL REFERENCES repos(id),
		name     TEXT NOT NULL,
		branch   TEXT NOT NULL,
		worktree TEXT NOT NULL,
		state    TEXT NOT NULL,
		created  TEXT NOT NULL,
		UNIQUE (repo_id, name)
	);`,
}

// DB is a SQLite-backed store.Store.
type DB struct {
	db *sql.DB
}

var _ store.Store = (*DB)(nil)

// Open opens or creates the database at path and brings its schema up to date.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One writer keeps SQLite's locking simple; the daemon is the only client.
	db.SetMaxOpenConns(1)
	s := &DB{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return s, nil
}

func (s *DB) migrate(ctx context.Context) error {
	var v int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("schema version %d is newer than this binary knows (%d)", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *DB) Driver() string { return "sqlite" }
func (s *DB) Close() error   { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func (s *DB) SaveWorkspace(ctx context.Context, ws store.Workspace, repos []store.Repo) (store.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspaces (name, path, created) VALUES (?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET name = excluded.name`,
		ws.Name, ws.Path, now())
	if err != nil {
		return store.Workspace{}, fmt.Errorf("save workspace %q: %w", ws.Name, err)
	}
	var out store.Workspace
	var created string
	err = tx.QueryRowContext(ctx, `SELECT id, name, path, created FROM workspaces WHERE path = ?`, ws.Path).
		Scan(&out.ID, &out.Name, &out.Path, &created)
	if err != nil {
		return store.Workspace{}, err
	}
	out.Created = parseTime(created)

	for _, r := range repos {
		stacks, err := json.Marshal(nonNil(r.Stacks))
		if err != nil {
			return store.Workspace{}, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO repos (workspace_id, name, path, remote, stacks, created) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (path) DO UPDATE SET
				workspace_id = excluded.workspace_id, name = excluded.name,
				remote = excluded.remote, stacks = excluded.stacks`,
			out.ID, r.Name, r.Path, r.Remote, string(stacks), now())
		if err != nil {
			return store.Workspace{}, fmt.Errorf("save repo %q: %w", r.Name, err)
		}
	}
	return out, tx.Commit()
}

func (s *DB) Workspaces(ctx context.Context) ([]store.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, created FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Workspace
	for rows.Next() {
		var w store.Workspace
		var created string
		if err := rows.Scan(&w.ID, &w.Name, &w.Path, &created); err != nil {
			return nil, err
		}
		w.Created = parseTime(created)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *DB) Repos(ctx context.Context, workspaceID int64) ([]store.Repo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, name, path, remote, stacks, created
		FROM repos WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Repo
	for rows.Next() {
		var r store.Repo
		var stacks, created string
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Path, &r.Remote, &stacks, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(stacks), &r.Stacks); err != nil {
			return nil, fmt.Errorf("repo %q stacks: %w", r.Name, err)
		}
		r.Created = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DB) Lanes(ctx context.Context, repoID int64) ([]store.Lane, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, repo_id, name, branch, worktree, state, created
		FROM lanes WHERE repo_id = ? ORDER BY name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Lane
	for rows.Next() {
		var l store.Lane
		var created string
		if err := rows.Scan(&l.ID, &l.RepoID, &l.Name, &l.Branch, &l.Worktree, &l.State, &created); err != nil {
			return nil, err
		}
		l.Created = parseTime(created)
		out = append(out, l)
	}
	return out, rows.Err()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
