package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const artifactCols = `id, workspace_id, repo_id, lane_id, run_id, path, title, kind, source, intent, intent_by, state, sha256, size, pinned, created, updated, closed`

type scanner interface{ Scan(dest ...any) error }

func scanArtifact(row scanner) (store.Artifact, error) {
	var a store.Artifact
	var pinned int
	var created, updated, closed string
	err := row.Scan(&a.ID, &a.WorkspaceID, &a.RepoID, &a.LaneID, &a.RunID, &a.Path, &a.Title, &a.Kind, &a.Source,
		&a.Intent, &a.IntentBy, &a.State, &a.SHA256, &a.Size, &pinned, &created, &updated, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return a, store.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Pinned = pinned != 0
	a.Created, a.Updated = parseTime(created), parseTime(updated)
	if closed != "" {
		t := parseTime(closed)
		a.Closed = &t
	}
	return a, nil
}

func (s *DB) LaneArtifact(ctx context.Context, laneID int64, path string) (store.Artifact, error) {
	return scanArtifact(s.db.QueryRowContext(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE lane_id = ? AND path = ?`, laneID, path))
}

func (s *DB) UpsertArtifact(ctx context.Context, a store.Artifact) (store.Artifact, bool, error) {
	if a.LaneID == 0 || a.Path == "" {
		return a, false, errors.New("an artifact needs a lane and a path")
	}
	if a.Intent == "" {
		a.Intent = store.ArtifactUndecided
	}
	if a.IntentBy == "" {
		a.IntentBy = store.IntentByDefault
	}
	if a.Kind == "" {
		a.Kind = store.ArtifactOther
	}
	if a.State == "" {
		a.State = store.ArtifactLive
	}
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO artifacts
		(workspace_id, repo_id, lane_id, run_id, path, title, kind, source, intent, intent_by, state, sha256, size, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.WorkspaceID, a.RepoID, a.LaneID, a.RunID, a.Path, a.Title, a.Kind, a.Source, a.Intent, a.IntentBy, a.State, a.SHA256, a.Size, t, t)
	if err != nil {
		return a, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		got, err := s.LaneArtifact(ctx, a.LaneID, a.Path)
		return got, true, err
	}
	cur, err := s.LaneArtifact(ctx, a.LaneID, a.Path)
	if err != nil {
		return a, false, err
	}
	// The intent moves only to one at least as strong in who chose it, and a person's
	// choice never moves. An undecided incoming intent never replaces a decided one.
	intent, by := cur.Intent, cur.IntentBy
	rank := map[string]int{store.IntentByDefault: 0, store.IntentByAgent: 1, store.IntentByPerson: 2}
	if cur.IntentBy != store.IntentByPerson && rank[a.IntentBy] >= rank[cur.IntentBy] &&
		(a.Intent != store.ArtifactUndecided || cur.Intent == store.ArtifactUndecided) {
		intent, by = a.Intent, a.IntentBy
	}
	title := cur.Title
	if title == "" {
		title = a.Title
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE artifacts SET title = ?, intent = ?, intent_by = ?, updated = ? WHERE id = ?`,
		title, intent, by, t, cur.ID); err != nil {
		return a, false, err
	}
	got, err := s.Artifact(ctx, cur.ID)
	got.Versions, got.Links = nil, nil
	return got, false, err
}

func (s *DB) AddArtifactVersion(ctx context.Context, v store.ArtifactVersion) (bool, error) {
	if v.SHA256 == "" {
		return false, errors.New("a version needs a content hash")
	}
	t := now()
	if !v.Seen.IsZero() {
		t = v.Seen.UTC().Format(time.RFC3339Nano)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO artifact_versions (artifact_id, sha256, size, run_id, seen) VALUES (?, ?, ?, ?, ?)`,
		v.ArtifactID, v.SHA256, v.Size, v.RunID, t)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET sha256 = ?, size = ?, updated = ? WHERE id = ?`,
			v.SHA256, v.Size, now(), v.ArtifactID); err != nil {
			return false, err
		}
	}
	return n == 1, tx.Commit()
}

func (s *DB) Artifact(ctx context.Context, id int64) (store.Artifact, error) {
	a, err := scanArtifact(s.db.QueryRowContext(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE id = ?`, id))
	if err != nil {
		return a, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT artifact_id, sha256, size, run_id, seen, snapshotted FROM artifact_versions WHERE artifact_id = ? ORDER BY seen DESC, rowid DESC`, id)
	if err != nil {
		return a, err
	}
	for rows.Next() {
		var v store.ArtifactVersion
		var seen, snap string
		if err := rows.Scan(&v.ArtifactID, &v.SHA256, &v.Size, &v.RunID, &seen, &snap); err != nil {
			rows.Close()
			return a, err
		}
		v.Seen = parseTime(seen)
		if snap != "" {
			t := parseTime(snap)
			v.Snapshotted = &t
		}
		a.Versions = append(a.Versions, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT artifact_id, kind, ref, created FROM artifact_links WHERE artifact_id = ? ORDER BY rowid`, id)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var l store.ArtifactLink
		var created string
		if err := rows.Scan(&l.ArtifactID, &l.Kind, &l.Ref, &created); err != nil {
			return a, err
		}
		l.Created = parseTime(created)
		a.Links = append(a.Links, l)
	}
	return a, rows.Err()
}

func (s *DB) ListArtifacts(ctx context.Context, f store.ArtifactFilter) ([]store.Artifact, error) {
	var where []string
	var args []any
	add := func(cond string, a ...any) {
		where = append(where, cond)
		args = append(args, a...)
	}
	if f.WorkspaceID != 0 {
		add(`a.workspace_id = ?`, f.WorkspaceID)
	}
	if f.LaneID != 0 {
		add(`a.lane_id = ?`, f.LaneID)
	}
	if f.RunID != 0 {
		add(`a.run_id = ?`, f.RunID)
	}
	if f.State != "" {
		add(`a.state = ?`, f.State)
	}
	if f.Kind != "" {
		add(`a.kind = ?`, f.Kind)
	}
	if !f.Since.IsZero() {
		add(`a.updated >= ?`, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.DecisionID != 0 {
		add(`a.id IN (SELECT artifact_id FROM artifact_links WHERE kind = ? AND ref = ?)`,
			store.ArtifactLinkDecision, strconv.FormatInt(f.DecisionID, 10))
	}
	if f.TopicID != 0 {
		// Its own link, or none and its lane's.
		add(`(a.id IN (SELECT CAST(ref AS INTEGER) FROM topic_links WHERE kind = ? AND topic_id = ?)
			OR (NOT EXISTS (SELECT 1 FROM topic_links WHERE kind = ? AND ref = CAST(a.id AS TEXT))
			    AND EXISTS (SELECT 1 FROM topic_links WHERE kind = ? AND topic_id = ? AND ref = CAST(a.lane_id AS TEXT))))`,
			store.TopicArtifact, f.TopicID, store.TopicArtifact, store.TopicLane, f.TopicID)
	}
	q := `SELECT ` + aliased(artifactCols) + ` FROM artifacts a`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	q += fmt.Sprintf(` ORDER BY a.updated DESC, a.id DESC LIMIT %d`, limit)
	return s.queryArtifacts(ctx, q, args...)
}

func (s *DB) queryArtifacts(ctx context.Context, q string, args ...any) ([]store.Artifact, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *DB) LinkArtifact(ctx context.Context, l store.ArtifactLink) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO artifact_links (artifact_id, kind, ref, created) VALUES (?, ?, ?, ?)`,
		l.ArtifactID, l.Kind, l.Ref, now())
	return err
}

func (s *DB) DecisionArtifacts(ctx context.Context, decisionID int64) ([]store.Artifact, error) {
	return s.ListArtifacts(ctx, store.ArtifactFilter{DecisionID: decisionID})
}

// aliased prefixes each column of a comma list with the table alias a.
func aliased(cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = "a." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

func (s *DB) SetArtifactIntent(ctx context.Context, id int64, intent, by string) error {
	return s.artifactUpdate(ctx, id, `UPDATE artifacts SET intent = ?, intent_by = ?, updated = ? WHERE id = ?`, intent, by, now(), id)
}

func (s *DB) SetArtifactPinned(ctx context.Context, id int64, pinned bool) error {
	p := 0
	if pinned {
		p = 1
	}
	return s.artifactUpdate(ctx, id, `UPDATE artifacts SET pinned = ?, updated = ? WHERE id = ?`, p, now(), id)
}

func (s *DB) SetArtifactState(ctx context.Context, id int64, state string, closing bool) error {
	if closing {
		return s.artifactUpdate(ctx, id, `UPDATE artifacts SET state = ?, closed = CASE WHEN closed = '' THEN ? ELSE closed END, updated = ? WHERE id = ?`,
			state, now(), now(), id)
	}
	return s.artifactUpdate(ctx, id, `UPDATE artifacts SET state = ?, closed = '', updated = ? WHERE id = ?`, state, now(), id)
}

func (s *DB) MarkVersionSnapshotted(ctx context.Context, artifactID int64, sha string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE artifact_versions SET snapshotted = ? WHERE artifact_id = ? AND sha256 = ? AND snapshotted = ''`,
		now(), artifactID, sha)
	return err
}

func (s *DB) artifactUpdate(ctx context.Context, id int64, q string, args ...any) error {
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *DB) SetArtifactTitle(ctx context.Context, id int64, title string) error {
	return s.artifactUpdate(ctx, id, `UPDATE artifacts SET title = ?, updated = ? WHERE id = ?`, title, now(), id)
}

func (s *DB) RenameArtifact(ctx context.Context, id int64, path string) error {
	a, err := s.Artifact(ctx, id)
	if err != nil {
		return err
	}
	if other, err := s.LaneArtifact(ctx, a.LaneID, path); err == nil && other.ID != id {
		return fmt.Errorf("%w: lane already has an artifact at %s", store.ErrConflict, path)
	}
	return s.artifactUpdate(ctx, id, `UPDATE artifacts SET path = ?, kind = ?, updated = ? WHERE id = ?`,
		path, store.ArtifactKindOf(path), now(), id)
}
