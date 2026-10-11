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

const nextCols = `id, title, repo, plan, lane, lane_id, mr, state, rank, source, note, created, updated, done_at`

func scanNext(sc interface{ Scan(...any) error }) (store.NextItem, error) {
	var it store.NextItem
	var created, updated, done string
	err := sc.Scan(&it.ID, &it.Title, &it.Repo, &it.Plan, &it.Lane, &it.LaneID, &it.MR, &it.State, &it.Rank,
		&it.Source, &it.Note, &created, &updated, &done)
	it.Created, it.Updated = parseTime(created), parseTime(updated)
	if done != "" {
		t := parseTime(done)
		it.DoneAt = &t
	}
	return it, err
}

// nextOrder lists items by stored state in list order, then rank.
const nextOrder = `ORDER BY CASE state WHEN 'approved' THEN 0 WHEN 'planned' THEN 1 WHEN 'deferred' THEN 2
	WHEN 'dropped' THEN 3 ELSE 4 END, rank, id`

func validNextState(s string) bool {
	for _, v := range store.NextStates {
		if v == s {
			return true
		}
	}
	return false
}

func (s *DB) AddNextItem(ctx context.Context, it store.NextItem) (store.NextItem, error) {
	it.Title = strings.TrimSpace(it.Title)
	if it.Title == "" {
		return it, errors.New("an item needs a title")
	}
	if it.State == "" {
		it.State = store.NextPlanned
	}
	if !validNextState(it.State) {
		return it, fmt.Errorf("unknown state %q", it.State)
	}
	if it.Source == "" {
		it.Source = store.NextByPerson
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return it, err
	}
	defer tx.Rollback()
	rank, err := bottomRank(ctx, tx, it.State)
	if err != nil {
		return it, err
	}
	t := now()
	done := ""
	if it.State == store.NextDone {
		done = t
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO next_items (title, repo, plan, lane, lane_id, mr, state, rank, source, note, created, updated, done_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Title, it.Repo, it.Plan, it.Lane, it.LaneID, it.MR, it.State, rank, it.Source, it.Note, t, t, done)
	if err != nil {
		return it, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return it, err
	}
	return s.NextItem(ctx, id)
}

// bottomRank is the rank after the last item of a state.
func bottomRank(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, state string) (int, error) {
	var r int
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(rank), 0) + 1 FROM next_items WHERE state = ?`, state).Scan(&r)
	return r, err
}

func (s *DB) NextItem(ctx context.Context, id int64) (store.NextItem, error) {
	it, err := scanNext(s.db.QueryRowContext(ctx, `SELECT `+nextCols+` FROM next_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, store.ErrNotFound
	}
	return it, err
}

func (s *DB) NextItems(ctx context.Context) ([]store.NextItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nextCols+` FROM next_items `+nextOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.NextItem{}
	for rows.Next() {
		it, err := scanNext(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *DB) UpdateNextItem(ctx context.Context, id int64, p store.NextPatch) (store.NextItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.NextItem{}, err
	}
	defer tx.Rollback()
	it, err := scanNext(tx.QueryRowContext(ctx, `SELECT `+nextCols+` FROM next_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, store.ErrNotFound
	}
	if err != nil {
		return it, err
	}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" {
			return it, errors.New("an item needs a title")
		}
		it.Title = t
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&it.Repo, p.Repo)
	set(&it.Plan, p.Plan)
	set(&it.Lane, p.Lane)
	set(&it.MR, p.MR)
	set(&it.Note, p.Note)
	if p.LaneID != nil {
		it.LaneID = *p.LaneID
	}
	if p.State != nil && *p.State != it.State {
		if !validNextState(*p.State) {
			return it, fmt.Errorf("unknown state %q", *p.State)
		}
		it.State = *p.State
		if it.Rank, err = bottomRank(ctx, tx, it.State); err != nil {
			return it, err
		}
		if it.State != store.NextDone {
			it.DoneAt = nil
		}
	}
	t := now()
	if it.State == store.NextDone && it.DoneAt == nil {
		d := time.Now().UTC()
		if p.DoneAt != nil {
			d = p.DoneAt.UTC()
		}
		it.DoneAt = &d
	}
	done := ""
	if it.DoneAt != nil {
		done = it.DoneAt.UTC().Format(time.RFC3339Nano)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE next_items SET title = ?, repo = ?, plan = ?, lane = ?, lane_id = ?, mr = ?,
		state = ?, rank = ?, note = ?, updated = ?, done_at = ? WHERE id = ?`,
		it.Title, it.Repo, it.Plan, it.Lane, it.LaneID, it.MR, it.State, it.Rank, it.Note, t, done, id); err != nil {
		return it, err
	}
	if err := tx.Commit(); err != nil {
		return it, err
	}
	return s.NextItem(ctx, id)
}

func (s *DB) MoveNextItem(ctx context.Context, id int64, how string) (store.NextItem, error) {
	if how != "top" && how != "up" && how != "down" {
		return store.NextItem{}, fmt.Errorf("move to top, up or down, not %q", how)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.NextItem{}, err
	}
	defer tx.Rollback()
	it, err := scanNext(tx.QueryRowContext(ctx, `SELECT `+nextCols+` FROM next_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, store.ErrNotFound
	}
	if err != nil {
		return it, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM next_items WHERE state = ? ORDER BY rank, id`, it.State)
	if err != nil {
		return it, err
	}
	var ids []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return it, err
		}
		ids = append(ids, v)
	}
	rows.Close()
	at := -1
	for i, v := range ids {
		if v == id {
			at = i
		}
	}
	switch how {
	case "top":
		if at > 0 {
			ids = append([]int64{id}, append(ids[:at:at], ids[at+1:]...)...)
		}
	case "up":
		if at > 0 {
			ids[at], ids[at-1] = ids[at-1], ids[at]
		}
	case "down":
		if at >= 0 && at < len(ids)-1 {
			ids[at], ids[at+1] = ids[at+1], ids[at]
		}
	}
	for i, v := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE next_items SET rank = ? WHERE id = ?`, i+1, v); err != nil {
			return it, err
		}
	}
	if err := tx.Commit(); err != nil {
		return it, err
	}
	return s.NextItem(ctx, id)
}
