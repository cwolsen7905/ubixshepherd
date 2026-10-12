package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func optTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return tstr(t)
}

func (s *DB) KeepGoing(ctx context.Context, workspaceID int64) (store.KeepGoing, error) {
	k := store.KeepGoing{WorkspaceID: workspaceID}
	var on int
	var since, off, last string
	err := s.db.QueryRowContext(ctx, `
		SELECT on_, since, by, spent_at_start, reason_off, off_at, last_action, cap_usd, max_runs, idle_hours
		FROM keep_going WHERE workspace_id = ?`, workspaceID).
		Scan(&on, &since, &k.By, &k.SpentAtStart, &k.ReasonOff, &off, &last, &k.Cap, &k.MaxRuns, &k.IdleHours)
	if errors.Is(err, sql.ErrNoRows) {
		return k, nil
	}
	if err != nil {
		return k, err
	}
	k.On = on != 0
	if since != "" {
		k.Since = parseTime(since)
	}
	if off != "" {
		k.OffAt = parseTime(off)
	}
	if last != "" {
		k.LastAction = parseTime(last)
	}
	return k, nil
}

func (s *DB) SetKeepGoing(ctx context.Context, k store.KeepGoing) error {
	on := 0
	if k.On {
		on = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO keep_going (workspace_id, on_, since, by, spent_at_start, reason_off, off_at, last_action, cap_usd, max_runs, idle_hours)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET on_ = excluded.on_, since = excluded.since, by = excluded.by,
			spent_at_start = excluded.spent_at_start, reason_off = excluded.reason_off, off_at = excluded.off_at,
			last_action = excluded.last_action, cap_usd = excluded.cap_usd, max_runs = excluded.max_runs,
			idle_hours = excluded.idle_hours`,
		k.WorkspaceID, on, optTime(k.Since), k.By, k.SpentAtStart, k.ReasonOff, optTime(k.OffAt),
		optTime(k.LastAction), k.Cap, k.MaxRuns, k.IdleHours)
	return err
}

func (s *DB) LaneStarts(ctx context.Context, laneID int64, day string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT n FROM keep_going_starts WHERE lane_id = ? AND day = ?`, laneID, day).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

func (s *DB) CountLaneStart(ctx context.Context, laneID int64, day string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO keep_going_starts (lane_id, day, n) VALUES (?, ?, 1)
		ON CONFLICT(lane_id, day) DO UPDATE SET n = n + 1 RETURNING n`, laneID, day).Scan(&n)
	return n, err
}
