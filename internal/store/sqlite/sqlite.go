// Package sqlite implements store.Store on SQLite through a pure-Go driver, so the binary
// cross-compiles with CGO_ENABLED=0.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	// Lanes keep their history: a closed lane frees its name, so uniqueness covers
	// unclosed lanes only. Adds the lane's scope and when it closed.
	`CREATE TABLE lanes_v2 (
		id       INTEGER PRIMARY KEY,
		repo_id  INTEGER NOT NULL REFERENCES repos(id),
		name     TEXT NOT NULL,
		branch   TEXT NOT NULL,
		base     TEXT NOT NULL DEFAULT '',
		worktree TEXT NOT NULL,
		scope    TEXT NOT NULL DEFAULT '[]',
		state    TEXT NOT NULL,
		created  TEXT NOT NULL,
		closed   TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO lanes_v2 (id, repo_id, name, branch, worktree, state, created)
		SELECT id, repo_id, name, branch, worktree, state, created FROM lanes;
	DROP TABLE lanes;
	ALTER TABLE lanes_v2 RENAME TO lanes;
	CREATE UNIQUE INDEX lanes_live_name ON lanes (repo_id, name) WHERE state != 'closed';
	CREATE UNIQUE INDEX lanes_live_worktree ON lanes (worktree) WHERE state != 'closed';`,
	// Agent runs: one agent started in one lane, and what came of it.
	`CREATE TABLE runs (
		id        INTEGER PRIMARY KEY,
		lane_id   INTEGER NOT NULL REFERENCES lanes(id),
		agent     TEXT NOT NULL,
		model     TEXT NOT NULL DEFAULT '',
		prompt    TEXT NOT NULL,
		state     TEXT NOT NULL,
		pid       INTEGER NOT NULL DEFAULT 0,
		log       TEXT NOT NULL,
		start_sha TEXT NOT NULL DEFAULT '',
		end_sha   TEXT NOT NULL DEFAULT '',
		commits   INTEGER NOT NULL DEFAULT 0,
		outside   TEXT NOT NULL DEFAULT '[]',
		exit_code INTEGER,
		error     TEXT NOT NULL DEFAULT '',
		started   TEXT NOT NULL,
		ended     TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX runs_lane ON runs (lane_id);`,
	// The agent's own session id, so a lane keeps its conversation, and the run a
	// continuation follows.
	`ALTER TABLE runs ADD COLUMN session TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN parent INTEGER NOT NULL DEFAULT 0;`,
	// What agents tell Shepherd while they work, and the questions they hold for a person.
	`CREATE TABLE events (
		id      INTEGER PRIMARY KEY,
		run_id  INTEGER NOT NULL REFERENCES runs(id),
		kind    TEXT NOT NULL,
		status  TEXT NOT NULL DEFAULT '',
		text    TEXT NOT NULL,
		created TEXT NOT NULL
	);
	CREATE INDEX events_run ON events (run_id);
	CREATE TABLE decisions (
		id             INTEGER PRIMARY KEY,
		run_id         INTEGER NOT NULL REFERENCES runs(id),
		question       TEXT NOT NULL,
		options        TEXT NOT NULL DEFAULT '[]',
		recommendation TEXT NOT NULL DEFAULT '',
		why            TEXT NOT NULL DEFAULT '',
		state          TEXT NOT NULL,
		answer         TEXT NOT NULL DEFAULT '',
		answer_run     INTEGER NOT NULL DEFAULT 0,
		created        TEXT NOT NULL,
		answered       TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX decisions_state ON decisions (state);`,
	// Requests between lanes: an agent's ask_shepherd, and how it was routed and answered.
	`CREATE TABLE requests (
		id         INTEGER PRIMARY KEY,
		from_run   INTEGER NOT NULL REFERENCES runs(id),
		kind       TEXT NOT NULL,
		lane       TEXT NOT NULL DEFAULT '',
		message    TEXT NOT NULL,
		state      TEXT NOT NULL,
		agent      TEXT NOT NULL DEFAULT '',
		target_run INTEGER NOT NULL DEFAULT 0,
		reply      TEXT NOT NULL DEFAULT '',
		reply_run  INTEGER NOT NULL DEFAULT 0,
		depth      INTEGER NOT NULL DEFAULT 1,
		note       TEXT NOT NULL DEFAULT '',
		created    TEXT NOT NULL,
		updated    TEXT NOT NULL
	);
	CREATE INDEX requests_state ON requests (state);`,
	// The feed: what happened across the swarm, in order, for the person's thread.
	// Settings: small values the daemon keeps, such as the front desk's session.
	`CREATE TABLE feed (
		id      INTEGER PRIMARY KEY,
		kind    TEXT NOT NULL,
		text    TEXT NOT NULL,
		ref     INTEGER NOT NULL DEFAULT 0,
		created TEXT NOT NULL
	);
	CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
	// What the forge last said about each lane's branch, so the watcher acts on changes.
	`CREATE TABLE lane_forge (
		lane_id         INTEGER PRIMARY KEY REFERENCES lanes(id),
		mr              INTEGER NOT NULL DEFAULT 0,
		mr_state        TEXT NOT NULL DEFAULT '',
		mr_url          TEXT NOT NULL DEFAULT '',
		pipeline        INTEGER NOT NULL DEFAULT 0,
		pipeline_status TEXT NOT NULL DEFAULT '',
		fix_tries       INTEGER NOT NULL DEFAULT 0,
		updated         TEXT NOT NULL
	);`,
	// Gate failures handed back to a lane's agent before Shepherd would push it.
	`ALTER TABLE lane_forge ADD COLUMN gate_tries INTEGER NOT NULL DEFAULT 0;`,
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

const laneCols = `id, repo_id, name, branch, base, worktree, scope, state, created, closed`

func scanLane(sc interface{ Scan(...any) error }) (store.Lane, error) {
	var l store.Lane
	var scope, created, closed string
	if err := sc.Scan(&l.ID, &l.RepoID, &l.Name, &l.Branch, &l.Base, &l.Worktree, &scope, &l.State, &created, &closed); err != nil {
		return l, err
	}
	if err := json.Unmarshal([]byte(scope), &l.Scope); err != nil {
		return l, fmt.Errorf("lane %q scope: %w", l.Name, err)
	}
	l.Created = parseTime(created)
	if closed != "" {
		t := parseTime(closed)
		l.Closed = &t
	}
	return l, nil
}

// Lanes returns a repo's lanes that are not closed.
func (s *DB) Lanes(ctx context.Context, repoID int64) ([]store.Lane, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+laneCols+`
		FROM lanes WHERE repo_id = ? AND state != ? ORDER BY name`, repoID, store.LaneClosed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Lane
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *DB) Lane(ctx context.Context, id int64) (store.Lane, error) {
	l, err := scanLane(s.db.QueryRowContext(ctx, `SELECT `+laneCols+` FROM lanes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return l, store.ErrNotFound
	}
	return l, err
}

func (s *DB) CreateLane(ctx context.Context, l store.Lane) (store.Lane, error) {
	scope, err := json.Marshal(nonNil(l.Scope))
	if err != nil {
		return l, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO lanes (repo_id, name, branch, base, worktree, scope, state, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.RepoID, l.Name, l.Branch, l.Base, l.Worktree, string(scope), l.State, now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return l, fmt.Errorf("%w: a lane named %q or at %s is already open", store.ErrConflict, l.Name, l.Worktree)
		}
		return l, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return l, err
	}
	return s.Lane(ctx, id)
}

func (s *DB) SetLaneState(ctx context.Context, id int64, state string) error {
	closed := ""
	if state == store.LaneClosed {
		closed = now()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE lanes SET state = ?, closed = ? WHERE id = ?`, state, closed, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *DB) DeleteLane(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM lanes WHERE id = ?`, id)
	return err
}

func (s *DB) Repo(ctx context.Context, id int64) (store.Repo, error) {
	var r store.Repo
	var stacks, created string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, path, remote, stacks, created FROM repos WHERE id = ?`, id).
		Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Path, &r.Remote, &stacks, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, store.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Created = parseTime(created)
	return r, json.Unmarshal([]byte(stacks), &r.Stacks)
}

const runCols = `id, lane_id, agent, model, prompt, state, pid, log, start_sha, end_sha, commits, outside, exit_code, error, started, ended, session, parent`

func scanRun(sc interface{ Scan(...any) error }) (store.Run, error) {
	var r store.Run
	var outside, started, ended string
	var exit sql.NullInt64
	if err := sc.Scan(&r.ID, &r.LaneID, &r.Agent, &r.Model, &r.Prompt, &r.State, &r.PID, &r.Log,
		&r.StartSHA, &r.EndSHA, &r.Commits, &outside, &exit, &r.Error, &started, &ended, &r.Session, &r.Parent); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(outside), &r.Outside); err != nil {
		return r, err
	}
	if exit.Valid {
		code := int(exit.Int64)
		r.ExitCode = &code
	}
	r.Started = parseTime(started)
	if ended != "" {
		t := parseTime(ended)
		r.Ended = &t
	}
	return r, nil
}

func (s *DB) CreateRun(ctx context.Context, r store.Run) (store.Run, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (lane_id, agent, model, prompt, state, log, start_sha, started, session, parent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.LaneID, r.Agent, r.Model, r.Prompt, r.State, r.Log, r.StartSHA, now(), r.Session, r.Parent)
	if err != nil {
		return r, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return r, err
	}
	return s.Run(ctx, id)
}

// UpdateRun writes a run's mutable fields: state, pid, end, commits, outcome.
func (s *DB) UpdateRun(ctx context.Context, r store.Run) error {
	outside, err := json.Marshal(nonNil(r.Outside))
	if err != nil {
		return err
	}
	var exit any
	if r.ExitCode != nil {
		exit = *r.ExitCode
	}
	ended := ""
	if r.Ended != nil {
		ended = r.Ended.UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE runs SET state = ?, pid = ?, log = ?, end_sha = ?, commits = ?, outside = ?,
			exit_code = ?, error = ?, ended = ?, session = ? WHERE id = ?`,
		r.State, r.PID, r.Log, r.EndSHA, r.Commits, string(outside), exit, r.Error, ended, r.Session, r.ID)
	return err
}

func (s *DB) Run(ctx context.Context, id int64) (store.Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, store.ErrNotFound
	}
	return r, err
}

// Runs returns the newest runs first; laneID 0 means every lane, state "" every state.
func (s *DB) Runs(ctx context.Context, laneID int64, state string, limit int) ([]store.Run, error) {
	q := `SELECT ` + runCols + ` FROM runs WHERE (? = 0 OR lane_id = ?) AND (? = '' OR state = ?) ORDER BY id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, laneID, laneID, state, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DB) AddEvent(ctx context.Context, e store.Event) (store.Event, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO events (run_id, kind, status, text, created) VALUES (?, ?, ?, ?, ?)`,
		e.RunID, e.Kind, e.Status, e.Text, now())
	if err != nil {
		return e, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

func (s *DB) Events(ctx context.Context, runID int64) ([]store.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, kind, status, text, created FROM events WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Event
	for rows.Next() {
		var e store.Event
		var created string
		if err := rows.Scan(&e.ID, &e.RunID, &e.Kind, &e.Status, &e.Text, &created); err != nil {
			return nil, err
		}
		e.Created = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

const decisionCols = `id, run_id, question, options, recommendation, why, state, answer, answer_run, created, answered`

func scanDecision(sc interface{ Scan(...any) error }) (store.Decision, error) {
	var d store.Decision
	var options, created, answered string
	if err := sc.Scan(&d.ID, &d.RunID, &d.Question, &options, &d.Recommendation, &d.Why, &d.State, &d.Answer, &d.AnswerRun, &created, &answered); err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(options), &d.Options); err != nil {
		return d, err
	}
	d.Created = parseTime(created)
	if answered != "" {
		t := parseTime(answered)
		d.Answered = &t
	}
	return d, nil
}

func (s *DB) CreateDecision(ctx context.Context, d store.Decision) (store.Decision, error) {
	options, err := json.Marshal(nonNil(d.Options))
	if err != nil {
		return d, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO decisions (run_id, question, options, recommendation, why, state, created)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.RunID, d.Question, string(options), d.Recommendation, d.Why, store.DecisionOpen, now())
	if err != nil {
		return d, err
	}
	id, _ := res.LastInsertId()
	return s.Decision(ctx, id)
}

func (s *DB) Decision(ctx context.Context, id int64) (store.Decision, error) {
	d, err := scanDecision(s.db.QueryRowContext(ctx, `SELECT `+decisionCols+` FROM decisions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, store.ErrNotFound
	}
	return d, err
}

// Decisions returns decisions in a state ("" for all), oldest first.
func (s *DB) Decisions(ctx context.Context, state string) ([]store.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+decisionCols+` FROM decisions WHERE (? = '' OR state = ?) ORDER BY id`, state, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AnswerDecision records an answer to an open decision; ErrConflict if it is not open.
func (s *DB) AnswerDecision(ctx context.Context, id int64, answer string) (store.Decision, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE decisions SET state = ?, answer = ?, answered = ? WHERE id = ? AND state = ?`,
		store.DecisionAnswered, answer, now(), id, store.DecisionOpen)
	if err != nil {
		return store.Decision{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Decision(ctx, id); err != nil {
			return store.Decision{}, err
		}
		return store.Decision{}, fmt.Errorf("%w: decision %d is not open", store.ErrConflict, id)
	}
	return s.Decision(ctx, id)
}

func (s *DB) SetDecisionRun(ctx context.Context, id, runID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE decisions SET answer_run = ? WHERE id = ?`, runID, id)
	return err
}

const requestCols = `id, from_run, kind, lane, message, state, agent, target_run, reply, reply_run, depth, note, created, updated`

func scanRequest(sc interface{ Scan(...any) error }) (store.Request, error) {
	var q store.Request
	var created, updated string
	err := sc.Scan(&q.ID, &q.FromRun, &q.Kind, &q.Lane, &q.Message, &q.State, &q.Agent, &q.TargetRun,
		&q.Reply, &q.ReplyRun, &q.Depth, &q.Note, &created, &updated)
	q.Created, q.Updated = parseTime(created), parseTime(updated)
	return q, err
}

func (s *DB) CreateRequest(ctx context.Context, q store.Request) (store.Request, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO requests (from_run, kind, lane, message, state, agent, depth, note, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.FromRun, q.Kind, q.Lane, q.Message, q.State, q.Agent, q.Depth, q.Note, t, t)
	if err != nil {
		return q, err
	}
	id, _ := res.LastInsertId()
	return s.Request(ctx, id)
}

func (s *DB) UpdateRequest(ctx context.Context, q store.Request) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE requests SET lane = ?, state = ?, agent = ?, target_run = ?, reply = ?, reply_run = ?, note = ?, updated = ?
		WHERE id = ?`,
		q.Lane, q.State, q.Agent, q.TargetRun, q.Reply, q.ReplyRun, q.Note, now(), q.ID)
	return err
}

func (s *DB) Request(ctx context.Context, id int64) (store.Request, error) {
	q, err := scanRequest(s.db.QueryRowContext(ctx, `SELECT `+requestCols+` FROM requests WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return q, store.ErrNotFound
	}
	return q, err
}

// Requests returns requests in any of the states (all when none), oldest first.
func (s *DB) Requests(ctx context.Context, states ...string) ([]store.Request, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestCols+` FROM requests ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[string]bool{}
	for _, st := range states {
		want[st] = true
	}
	var out []store.Request
	for rows.Next() {
		q, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		if len(want) == 0 || want[q.State] {
			out = append(out, q)
		}
	}
	return out, rows.Err()
}

func (s *DB) AddFeed(ctx context.Context, kind, text string, ref int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO feed (kind, text, ref, created) VALUES (?, ?, ?, ?)`, kind, text, ref, now())
	return err
}

// Feed returns items after an id, oldest first.
func (s *DB) Feed(ctx context.Context, after int64, limit int) ([]store.FeedItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, text, ref, created FROM feed WHERE id > ? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.FeedItem
	for rows.Next() {
		var f store.FeedItem
		var created string
		if err := rows.Scan(&f.ID, &f.Kind, &f.Text, &f.Ref, &created); err != nil {
			return nil, err
		}
		f.Created = parseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

// LastFeed is the newest feed id, 0 when there is none.
func (s *DB) LastFeed(ctx context.Context) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(id) FROM feed`).Scan(&id)
	return id.Int64, err
}

func (s *DB) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *DB) LaneForge(ctx context.Context, laneID int64) (store.LaneForge, error) {
	f := store.LaneForge{LaneID: laneID}
	err := s.db.QueryRowContext(ctx, `SELECT mr, mr_state, mr_url, pipeline, pipeline_status, fix_tries, gate_tries FROM lane_forge WHERE lane_id = ?`, laneID).
		Scan(&f.MR, &f.MRState, &f.MRURL, &f.Pipeline, &f.PipelineStatus, &f.FixTries, &f.GateTries)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	return f, err
}

func (s *DB) PutLaneForge(ctx context.Context, f store.LaneForge) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO lane_forge (lane_id, mr, mr_state, mr_url, pipeline, pipeline_status, fix_tries, gate_tries, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (lane_id) DO UPDATE SET mr = excluded.mr, mr_state = excluded.mr_state, mr_url = excluded.mr_url,
			pipeline = excluded.pipeline, pipeline_status = excluded.pipeline_status, fix_tries = excluded.fix_tries,
			gate_tries = excluded.gate_tries, updated = excluded.updated`,
		f.LaneID, f.MR, f.MRState, f.MRURL, f.Pipeline, f.PipelineStatus, f.FixTries, f.GateTries, now())
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
