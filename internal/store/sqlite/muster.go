package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const musterCols = `id, key, kind, subject_kind, subject_id, repo, reason, fix, fix_arg, before, state, auto, created, updated, applied, undo_until`

func scanMuster(sc interface{ Scan(...any) error }) (store.MusterItem, error) {
	var m store.MusterItem
	var auto int
	var created, updated, applied, undo string
	err := sc.Scan(&m.ID, &m.Key, &m.Kind, &m.SubjectKind, &m.SubjectID, &m.Repo, &m.Reason, &m.Fix, &m.FixArg,
		&m.Before, &m.State, &auto, &created, &updated, &applied, &undo)
	m.Auto = auto != 0
	m.Created, m.Updated = parseTime(created), parseTime(updated)
	if applied != "" {
		t := parseTime(applied)
		m.Applied = &t
	}
	if undo != "" {
		t := parseTime(undo)
		m.UndoUntil = &t
	}
	return m, err
}

func ptrText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (s *DB) AddMuster(ctx context.Context, it store.MusterItem) (store.MusterItem, bool, error) {
	if it.Key == "" || it.Kind == "" || it.SubjectKind == "" {
		return it, false, errors.New("a muster item needs a key, a kind and a subject")
	}
	if it.State == "" {
		it.State = store.MusterOpen
	}
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO muster_items
		(key, kind, subject_kind, subject_id, repo, reason, fix, fix_arg, before, state, auto, created, updated, applied, undo_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Key, it.Kind, it.SubjectKind, it.SubjectID, it.Repo, it.Reason, it.Fix, it.FixArg, it.Before, it.State,
		b2i(it.Auto), t, t, ptrText(it.Applied), ptrText(it.UndoUntil))
	if err != nil {
		return it, false, err
	}
	n, _ := res.RowsAffected()
	got, err := scanMuster(s.db.QueryRowContext(ctx, `SELECT `+musterCols+` FROM muster_items WHERE key = ?`, it.Key))
	return got, n > 0, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *DB) MusterItem(ctx context.Context, id int64) (store.MusterItem, error) {
	m, err := scanMuster(s.db.QueryRowContext(ctx, `SELECT `+musterCols+` FROM muster_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, store.ErrNotFound
	}
	return m, err
}

func (s *DB) MusterItems(ctx context.Context, f store.MusterFilter) ([]store.MusterItem, error) {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+musterCols+` FROM muster_items
		WHERE (? = '' OR state = ?) AND (? = '' OR kind = ?) ORDER BY id DESC LIMIT ?`,
		f.State, f.State, f.Kind, f.Kind, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.MusterItem{}
	for rows.Next() {
		m, err := scanMuster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *DB) UpdateMuster(ctx context.Context, id int64, p store.MusterPatch) (store.MusterItem, error) {
	m, err := s.MusterItem(ctx, id)
	if err != nil {
		return m, err
	}
	if p.State != nil {
		m.State = *p.State
	}
	if p.Reason != nil {
		m.Reason = *p.Reason
	}
	if p.Before != nil {
		m.Before = *p.Before
	}
	if p.Applied != nil {
		m.Applied = p.Applied
	}
	if p.UndoUntil != nil {
		m.UndoUntil = p.UndoUntil
	}
	_, err = s.db.ExecContext(ctx, `UPDATE muster_items SET state = ?, reason = ?, before = ?, applied = ?, undo_until = ?, updated = ? WHERE id = ?`,
		m.State, m.Reason, m.Before, ptrText(m.Applied), ptrText(m.UndoUntil), now(), id)
	if err != nil {
		return m, err
	}
	return s.MusterItem(ctx, id)
}
