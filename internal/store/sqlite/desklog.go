package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func (s *DB) SaveDeskTurnLog(ctx context.Context, l store.DeskTurnLog) error {
	events, err := json.Marshal(l.Events)
	if err != nil {
		return err
	}
	if l.Events == nil {
		events = []byte("[]")
	}
	created := l.Created
	if created.IsZero() {
		created = time.Now()
	}
	var waiting any
	if l.Waiting != nil {
		waiting = *l.Waiting
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO desk_turn_log (workspace_id, turn, started_by, topic, waiting, events, created)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id, turn) DO UPDATE SET started_by = excluded.started_by, topic = excluded.topic, waiting = excluded.waiting,
			events = excluded.events, created = excluded.created`,
		l.WorkspaceID, l.Turn, l.StartedBy, l.Topic, waiting, string(events), created.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *DB) DeskTurnLogs(ctx context.Context, workspaceID int64) ([]store.DeskTurnLog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workspace_id, turn, started_by, topic, waiting, events, created
		FROM desk_turn_log WHERE workspace_id = ? ORDER BY turn`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.DeskTurnLog
	for rows.Next() {
		var l store.DeskTurnLog
		var waiting sql.NullInt64
		var events, created string
		if err := rows.Scan(&l.WorkspaceID, &l.Turn, &l.StartedBy, &l.Topic, &waiting, &events, &created); err != nil {
			return nil, err
		}
		if waiting.Valid {
			n := int(waiting.Int64)
			l.Waiting = &n
		}
		if err := json.Unmarshal([]byte(events), &l.Events); err != nil {
			return nil, err
		}
		l.Created = parseTime(created)
		out = append(out, l)
	}
	return out, rows.Err()
}
