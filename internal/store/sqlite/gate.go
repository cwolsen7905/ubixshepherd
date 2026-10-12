package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const gateCols = `id, lane_id, sha, gate, passed, at, source`

func scanGate(row *sql.Row) (*store.GateResult, error) {
	var g store.GateResult
	var passed int
	var at string
	err := row.Scan(&g.ID, &g.LaneID, &g.SHA, &g.Gate, &passed, &at, &g.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.Passed, g.At = passed != 0, parseTime(at)
	return &g, nil
}

func (s *DB) AddGateResult(ctx context.Context, g store.GateResult) (store.GateResult, error) {
	if g.Source == "" {
		g.Source = store.GateSourceShip
	}
	if g.At.IsZero() {
		g.At = parseTime(now())
	}
	passed := 0
	if g.Passed {
		passed = 1
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO gate_results (lane_id, sha, gate, passed, at, source) VALUES (?, ?, ?, ?, ?, ?)`,
		g.LaneID, g.SHA, g.Gate, passed, g.At.UTC().Format(time.RFC3339Nano), g.Source)
	if err != nil {
		return g, err
	}
	g.ID, _ = res.LastInsertId()
	return g, nil
}

func (s *DB) GateResultFor(ctx context.Context, laneID int64, sha string) (*store.GateResult, error) {
	return scanGate(s.db.QueryRowContext(ctx, `SELECT `+gateCols+` FROM gate_results WHERE lane_id = ? AND sha = ? ORDER BY id DESC LIMIT 1`, laneID, sha))
}

func (s *DB) LastGateResult(ctx context.Context, laneID int64) (*store.GateResult, error) {
	return scanGate(s.db.QueryRowContext(ctx, `SELECT `+gateCols+` FROM gate_results WHERE lane_id = ? ORDER BY id DESC LIMIT 1`, laneID))
}
