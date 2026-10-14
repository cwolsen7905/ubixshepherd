package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// upgradeSeed is data written once the database is at schema version `at`, using only
// the tables and columns that version has, so each later migration carries it forward.
type upgradeSeed struct {
	at  int
	sql []string
}

var upgradeSeeds = []upgradeSeed{
	{1, []string{
		`INSERT INTO workspaces (name, path, created) VALUES ('w', '/w', '2026-01-01T00:00:00Z')`,
		`INSERT INTO repos (workspace_id, name, path, created) VALUES (1, 'a', '/w/a', '2026-01-01T00:00:00Z')`,
		`INSERT INTO lanes (repo_id, name, branch, worktree, state, created) VALUES (1, 'live', 'live', '/w/a-wt/live', 'open', '2026-01-02T00:00:00Z')`,
		`INSERT INTO lanes (repo_id, name, branch, worktree, state, created) VALUES (1, 'done', 'done', '/w/a-wt/done', 'closed', '2026-01-03T00:00:00Z')`,
	}},
	// Version 2 frees a closed lane's name: a second lane may reuse it.
	{2, []string{
		`INSERT INTO lanes (repo_id, name, branch, worktree, state, scope, created, closed) VALUES (1, 'done', 'done2', '/w/a-wt/done2', 'closed', '["x/"]', '2026-01-04T00:00:00Z', '2026-01-05T00:00:00Z')`,
	}},
	{3, []string{
		`INSERT INTO runs (lane_id, agent, model, prompt, state, log, started) VALUES (1, 'claude', 'opus', 'p1', 'done', '/l/1', '2026-02-01T00:00:00Z')`,
		`INSERT INTO runs (lane_id, agent, model, prompt, state, log, started) VALUES (1, 'opencode', 'openrouter/some-model', 'p2', 'done', '/l/2', '2026-02-02T00:00:00Z')`,
		`INSERT INTO runs (lane_id, agent, model, prompt, state, log, started) VALUES (1, 'opencode', 'local/qwen', 'p3', 'done', '/l/3', '2026-02-03T00:00:00Z')`,
		`INSERT INTO runs (lane_id, agent, model, prompt, state, log, started) VALUES (2, 'opencode', 'plain', 'p4', 'failed', '/l/4', '2026-02-04T00:00:00Z')`,
	}},
	{4, []string{
		`INSERT INTO runs (lane_id, agent, prompt, state, log, started, session, parent) VALUES (1, 'claude', 'p5', 'done', '/l/5', '2026-02-05T00:00:00Z', 'sess-5', 1)`,
	}},
	{5, []string{
		`INSERT INTO events (run_id, kind, text, created) VALUES (1, 'status', 'working', '2026-02-01T00:01:00Z')`,
		`INSERT INTO decisions (run_id, question, state, created) VALUES (1, 'which way?', 'open', '2026-02-01T00:02:00Z')`,
	}},
	{6, []string{
		`INSERT INTO requests (from_run, kind, message, state, created, updated) VALUES (1, 'ask', 'need a thing', 'open', '2026-02-01T00:03:00Z', '2026-02-01T00:03:00Z')`,
	}},
	{7, []string{
		`INSERT INTO feed (kind, text, created) VALUES ('note', 'hello', '2026-02-01T00:04:00Z')`,
		`INSERT INTO settings (key, value) VALUES ('desk.session', 'abc')`,
	}},
	{8, []string{
		`INSERT INTO lane_forge (lane_id, mr, mr_state, mr_url, pipeline, pipeline_status, fix_tries, updated) VALUES (1, 12, 'opened', 'https://forge/mr/12', 99, 'running', 2, '2026-02-06T00:00:00Z')`,
	}},
	{17, []string{
		`INSERT INTO usage (day, kind, run_id, agent, model, created) VALUES ('2026-02-02', 'run', 2, 'opencode', 'openrouter/some-model', '2026-02-02T00:00:00Z')`,
		`INSERT INTO usage (day, kind, run_id, agent, model, created) VALUES ('2026-02-03', 'run', 3, 'opencode', 'local/qwen', '2026-02-03T00:00:00Z')`,
	}},
	{23, []string{
		`INSERT INTO next_items (title, repo, state, rank, source, lane_id, created, updated) VALUES ('do it', 'a', 'planned', 1, 'person', 1, '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z')`,
	}},
	{25, []string{
		`INSERT INTO keep_going (workspace_id, on_, by) VALUES (1, 1, 'me')`,
	}},
	{26, []string{
		`INSERT INTO topics (title, source, created, updated) VALUES ('t', 'person', '2026-03-02T00:00:00Z', '2026-03-02T00:00:00Z')`,
		`INSERT INTO topic_links (topic_id, kind, ref, by, created) VALUES (1, 'lane', 'live', 'person', '2026-03-02T00:00:00Z')`,
	}},
}

// upgradeCheck reads one row, as columns joined by '|' (NULL as "<null>"), from rows that
// existed at schema version `at`; the values include what later migrations add or set.
type upgradeCheck struct {
	at    int
	query string
	want  string
}

var upgradeChecks = []upgradeCheck{
	{1, `SELECT name || '|' || branch || '|' || state || '|' || created FROM lanes WHERE id = 1`, `live|live|open|2026-01-02T00:00:00Z`},
	// columns lanes gained: base, scope, closed, then the origin
	{1, `SELECT base || '|' || scope || '|' || closed || '|' || origin_via || '|' || origin_agent || '|' || origin_session || '|' || origin_run || '|' || origin_pid || '|' || origin_dir || '|' || origin_detail FROM lanes WHERE id = 1`, `|[]|||||0|0||`},
	{1, `SELECT state || '|' || worktree FROM lanes WHERE id = 2`, `closed|/w/a-wt/done`},
	{1, `SELECT remote || '|' || stacks || '|' || removed FROM repos WHERE id = 1`, `|[]|`},
	{2, `SELECT name || '|' || scope || '|' || closed FROM lanes WHERE id = 3`, `done|["x/"]|2026-01-05T00:00:00Z`},
	{3, `SELECT agent || '|' || model || '|' || state || '|' || pid || '|' || commits || '|' || outside || '|' || ifnull(exit_code, '<null>') || '|' || session || '|' || parent || '|' || cost_usd || '|' || credits || '|' || session_usd || '|' || session_credits || '|' || dismissed FROM runs WHERE id = 1`,
		`claude|opus|done|0|0|[]|<null>||0|0.0|0.0|0.0|0.0|`},
	// the estimate rule: a hosted OpenCode model, and only that
	{3, `SELECT group_concat(id || ':' || cost_estimated, ',') FROM (SELECT id, cost_estimated FROM runs WHERE id <= 4 ORDER BY id)`, `1:0,2:1,3:0,4:0`},
	{4, `SELECT session || '|' || parent || '|' || cost_estimated FROM runs WHERE id = 5`, `sess-5|1|0`},
	{5, `SELECT text FROM events WHERE run_id = 1`, `working`},
	{5, `SELECT question || '|' || options || '|' || recommendation || '|' || why || '|' || state || '|' || answer || '|' || answer_run || '|' || answered FROM decisions WHERE id = 1`,
		`which way?|[]|||open||0|`},
	{6, `SELECT message || '|' || state || '|' || lane || '|' || depth || '|' || target_run || '|' || ifnull(project, '<null>') || '|' || ifnull(from_project, '<null>') || '|' || ifnull(evidence, '<null>') || '|' || ifnull(touches, '<null>') || '|' || ifnull(version, '<null>') FROM requests WHERE id = 1`,
		`need a thing|open||1|0|<null>|<null>|<null>|<null>|<null>`},
	{7, `SELECT text FROM feed WHERE id = 1`, `hello`},
	{7, `SELECT value FROM settings WHERE key = 'desk.session'`, `abc`},
	{8, `SELECT mr || '|' || mr_state || '|' || mr_url || '|' || pipeline || '|' || pipeline_status || '|' || fix_tries || '|' || gate_tries || '|' || merge_sha || '|' || mr_created || '|' || mr_updated || '|' || pipeline_started || '|' || pipeline_finished || '|' || approved || '|' || require_approval FROM lane_forge WHERE lane_id = 1`,
		`12|opened|https://forge/mr/12|99|running|2|0||||||0|`},
	{17, `SELECT group_concat(run_id || ':' || cost_estimated || ':' || requests || ':' || input_tokens, ',') FROM (SELECT * FROM usage ORDER BY run_id)`, `2:1:0:0,3:0:0:0`},
	{23, `SELECT title || '|' || repo || '|' || plan || '|' || lane || '|' || lane_id || '|' || mr || '|' || state || '|' || rank || '|' || note || '|' || done_at FROM next_items WHERE id = 1`,
		`do it|a|||1||planned|1||`},
	{25, `SELECT on_ || '|' || by || '|' || since || '|' || cap_usd || '|' || max_runs FROM keep_going WHERE workspace_id = 1`, `1|me||0.0|0`},
	{26, `SELECT t.title || '|' || t.status || '|' || t.summary || '|' || t.cleared_at || '|' || t.merged_into || '|' || l.kind || '|' || l.ref FROM topics t JOIN topic_links l ON l.topic_id = t.id`,
		`t|open|||0|lane|live`},
}

func schemaRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT type || '|' || name || '|' || tbl_name || '|' || ifnull(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestUpgradeFromOlderSchemas builds a database as an older binary left it, with rows
// written at each version along the way, then opens it with the normal path and checks
// that every later migration ran and carried the rows.
func TestUpgradeFromOlderSchemas(t *testing.T) {
	ctx := context.Background()
	latest := len(migrations)

	fresh, _ := open(t)
	wantSchema := schemaRows(t, fresh.db)
	if v := userVersion(t, fresh.db); v != latest {
		t.Fatalf("fresh database at version %d, want %d", v, latest)
	}

	// Every seed and check must belong to a version that exists and be reachable.
	for _, s := range upgradeSeeds {
		if s.at < 1 || s.at > latest {
			t.Fatalf("seed at version %d out of range", s.at)
		}
	}

	starts := []int{1, 2, 3, 5, 8, 13, 17, 22, 25, 26, latest - 1}
	for _, start := range slices.Compact(slices.Clone(starts)) {
		t.Run(fmt.Sprintf("from_v%d", start), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shepherd.db")
			raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
			if err != nil {
				t.Fatal(err)
			}
			raw.SetMaxOpenConns(1)
			for v := 1; v <= start; v++ {
				if _, err := raw.ExecContext(ctx, migrations[v-1]); err != nil {
					t.Fatalf("migration %d: %v", v, err)
				}
				for _, s := range upgradeSeeds {
					if s.at != v {
						continue
					}
					for _, q := range s.sql {
						if _, err := raw.ExecContext(ctx, q); err != nil {
							t.Fatalf("seed at v%d: %s: %v", v, q, err)
						}
					}
				}
				if _, err := raw.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, v)); err != nil {
					t.Fatal(err)
				}
			}
			if got := userVersion(t, raw); got != start {
				t.Fatalf("built at version %d, want %d", got, start)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}

			db, err := Open(ctx, path)
			if err != nil {
				t.Fatalf("open at v%d: %v", start, err)
			}
			defer db.Close()

			if got := userVersion(t, db.db); got != latest {
				t.Errorf("schema version = %d, want %d", got, latest)
			}
			got := schemaRows(t, db.db)
			if !slices.Equal(got, wantSchema) {
				for _, g := range got {
					if !slices.Contains(wantSchema, g) {
						t.Errorf("schema differs from a fresh database, upgraded has:\n%s", g)
					}
				}
				for _, w := range wantSchema {
					if !slices.Contains(got, w) {
						t.Errorf("schema differs from a fresh database, fresh has:\n%s", w)
					}
				}
			}

			for _, c := range upgradeChecks {
				if c.at > start {
					continue
				}
				var have sql.NullString
				if err := db.db.QueryRowContext(ctx, c.query).Scan(&have); err != nil {
					t.Errorf("%s: %v", c.query, err)
					continue
				}
				if !have.Valid || have.String != c.want {
					t.Errorf("row seeded at v%d\n%s\n got %q\nwant %q", c.at, c.query, have.String, c.want)
				}
			}

			// The foreign keys still hold after tables were rebuilt.
			rows, err := db.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
			if err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				t.Errorf("foreign_key_check reports a violation after upgrade")
			}
			rows.Close()

			// A second open is a no-op, and the upgraded store takes new writes.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			again, err := Open(ctx, path)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer again.Close()
			if got := schemaRows(t, again.db); !slices.Equal(got, wantSchema) {
				t.Errorf("schema changed on reopen")
			}
			if _, err := again.Lanes(ctx, 1); err != nil {
				t.Errorf("Lanes after upgrade: %v", err)
			}
			// The desk_questions table (the newest migration) takes and closes a row.
			q, err := again.AddQuestion(ctx, store.Question{WorkspaceID: 1, Turn: 7, Text: "which way?"})
			if err != nil {
				t.Fatalf("AddQuestion after upgrade: %v", err)
			}
			if _, err := again.CloseQuestion(ctx, q.ID, store.QuestionSkipped, 0); err != nil {
				t.Errorf("CloseQuestion after upgrade: %v", err)
			}
		})
	}
}
