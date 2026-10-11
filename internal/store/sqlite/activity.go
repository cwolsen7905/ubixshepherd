package sqlite

import (
	"context"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// laneActivitySQL gives each of a repo's open lanes, per source, the greatest stored
// time. A request counts for the lane of the run that sent it and of the run it was
// routed to.
const laneActivitySQL = `
SELECT lane_id, kind, MAX(at) FROM (
	SELECT r.lane_id AS lane_id, 'run' AS kind, r.started AS at FROM runs r
	UNION ALL SELECT r.lane_id, 'run', r.ended FROM runs r WHERE r.ended IS NOT NULL AND r.ended != ''
	UNION ALL SELECT r.lane_id, 'decision', d.created FROM decisions d JOIN runs r ON r.id = d.run_id
	UNION ALL SELECT r.lane_id, 'decision', d.answered FROM decisions d JOIN runs r ON r.id = d.run_id WHERE d.answered != ''
	UNION ALL SELECT r.lane_id, 'request', q.created FROM requests q JOIN runs r ON r.id = q.from_run
	UNION ALL SELECT r.lane_id, 'request', q.updated FROM requests q JOIN runs r ON r.id = q.from_run
	UNION ALL SELECT r.lane_id, 'request', q.updated FROM requests q JOIN runs r ON r.id = q.target_run
)
WHERE lane_id IN (SELECT id FROM lanes WHERE repo_id = ? AND state != ?)
GROUP BY lane_id, kind`

// LaneActivity folds the per-source maxima in Go: the stored times are RFC3339 text, and
// parsing them keeps the comparison exact where a SQL string MAX would not be.
func (s *DB) LaneActivity(ctx context.Context, repoID int64) (map[int64]store.Activity, error) {
	rows, err := s.db.QueryContext(ctx, laneActivitySQL, repoID, store.LaneClosed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]store.Activity{}
	for rows.Next() {
		var lane int64
		var kind, at string
		if err := rows.Scan(&lane, &kind, &at); err != nil {
			return nil, err
		}
		t := parseTime(at)
		if t.IsZero() {
			continue
		}
		if cur, ok := out[lane]; !ok || t.After(cur.At) {
			out[lane] = store.Activity{At: t, By: kind}
		}
	}
	return out, rows.Err()
}
