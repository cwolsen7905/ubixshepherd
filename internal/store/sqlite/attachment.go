package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const attachCols = `id, workspace_id, turn, sha, name, mime, kind, size, created`

func scanAttachment(sc interface{ Scan(...any) error }) (store.Attachment, error) {
	var a store.Attachment
	var created string
	err := sc.Scan(&a.ID, &a.WorkspaceID, &a.Turn, &a.SHA, &a.Name, &a.Mime, &a.Kind, &a.Size, &created)
	a.Created = parseTime(created)
	return a, err
}

func (s *DB) AddAttachment(ctx context.Context, a store.Attachment) (store.Attachment, error) {
	a.Created = time.Now().UTC()
	a.Turn = 0
	res, err := s.db.ExecContext(ctx, `INSERT INTO desk_attachments (workspace_id, turn, sha, name, mime, kind, size, created)
		VALUES (?, 0, ?, ?, ?, ?, ?, ?)`, a.WorkspaceID, a.SHA, a.Name, a.Mime, a.Kind, a.Size, a.Created.Format(time.RFC3339Nano))
	if err != nil {
		return a, err
	}
	a.ID, err = res.LastInsertId()
	return a, err
}

func (s *DB) Attachment(ctx context.Context, id int64) (store.Attachment, error) {
	a, err := scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attachCols+` FROM desk_attachments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, store.ErrNotFound
	}
	return a, err
}

func (s *DB) TakeAttachments(ctx context.Context, workspaceID, turn int64, ids []int64) ([]store.Attachment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var out []store.Attachment
	for _, id := range ids {
		a, err := scanAttachment(tx.QueryRowContext(ctx, `SELECT `+attachCols+` FROM desk_attachments WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (a.WorkspaceID != workspaceID || a.Turn != 0)) {
			return nil, fmt.Errorf("%w: attachment %d is unknown or already sent", store.ErrConflict, id)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE desk_attachments SET turn = ? WHERE id = ?`, turn, id); err != nil {
			return nil, err
		}
		a.Turn = turn
		out = append(out, a)
	}
	return out, tx.Commit()
}

func (s *DB) SweepAttachments(ctx context.Context, before time.Time) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT sha FROM desk_attachments WHERE
		(turn = 0 AND created < ?) OR (turn != 0 AND NOT EXISTS (
			SELECT 1 FROM desk_events e WHERE e.workspace_id = desk_attachments.workspace_id AND e.seq = desk_attachments.turn))`,
		before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	var shas []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			rows.Close()
			return nil, err
		}
		shas = append(shas, sha)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM desk_attachments WHERE
		(turn = 0 AND created < ?) OR (turn != 0 AND NOT EXISTS (
			SELECT 1 FROM desk_events e WHERE e.workspace_id = desk_attachments.workspace_id AND e.seq = desk_attachments.turn))`,
		before.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	var free []string
	for _, sha := range shas {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM desk_attachments WHERE sha = ?`, sha).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			free = append(free, sha)
		}
	}
	return free, tx.Commit()
}

// fillAttachments sets the files on the user events among es.
func (s *DB) fillAttachments(ctx context.Context, es []store.DeskEvent) error {
	var turns []any
	at := map[int64]int{}
	for i, e := range es {
		if e.Kind == store.DeskUser {
			turns = append(turns, e.Turn)
			at[e.Turn] = i
		}
	}
	if len(turns) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+attachCols+` FROM desk_attachments WHERE turn IN (?`+strings.Repeat(",?", len(turns)-1)+`) ORDER BY id`, turns...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return err
		}
		if i, ok := at[a.Turn]; ok && es[i].WorkspaceID == a.WorkspaceID {
			es[i].Attachments = append(es[i].Attachments, a)
		}
	}
	return rows.Err()
}
