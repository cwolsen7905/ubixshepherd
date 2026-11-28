package sqlite

import (
	"context"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// AddHeldStart queues a start for the daemon's next start.
func (s *DB) AddHeldStart(ctx context.Context, h store.HeldStart) (store.HeldStart, error) {
	at := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO held_starts (request, why, auto, created, tenant_id) VALUES (?, ?, ?, ?, ?)`,
		h.Request, h.Why, h.Auto, at.Format(time.RFC3339Nano), s.newRowTenant())
	if err != nil {
		return h, err
	}
	h.ID, err = res.LastInsertId()
	h.Created = at
	return h, err
}

// HeldStarts lists the queued starts, oldest first.
func (s *DB) HeldStarts(ctx context.Context) ([]store.HeldStart, error) {
	cond, args := s.in("held_starts")
	rows, err := s.db.QueryContext(ctx, `SELECT id, request, why, auto, created FROM held_starts WHERE 1 = 1`+cond+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.HeldStart
	for rows.Next() {
		var h store.HeldStart
		var created string
		if err := rows.Scan(&h.ID, &h.Request, &h.Why, &h.Auto, &created); err != nil {
			return nil, err
		}
		h.Created = parseTime(created)
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteHeldStart removes a queued start.
func (s *DB) DeleteHeldStart(ctx context.Context, id int64) error {
	cond, args := s.in("held_starts")
	_, err := s.db.ExecContext(ctx, `DELETE FROM held_starts WHERE id = ?`+cond, append([]any{id}, args...)...)
	return err
}

// SetRunReason sets a run's reason and nothing else.
func (s *DB) SetRunReason(ctx context.Context, id int64, reason string) error {
	cond, args := s.in("runs")
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET reason = ? WHERE id = ?`+cond, append([]any{reason, id}, args...)...)
	return err
}
