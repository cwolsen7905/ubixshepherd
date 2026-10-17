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

const nextCols = `id, title, repo, plan, lane, lane_id, mr, state, rank, source, note, created, updated, done_at,
	done_when, depends_on, suggestion, suggested, checked`

func scanNext(sc interface{ Scan(...any) error }) (store.NextItem, error) {
	var it store.NextItem
	var created, updated, done, deps, suggested, checked string
	err := sc.Scan(&it.ID, &it.Title, &it.Repo, &it.Plan, &it.Lane, &it.LaneID, &it.MR, &it.State, &it.Rank,
		&it.Source, &it.Note, &created, &updated, &done, &it.DoneWhen, &deps, &it.Suggestion, &suggested, &checked)
	it.Created, it.Updated = parseTime(created), parseTime(updated)
	it.DependsOn = store.ParseDeps(deps)
	if suggested != "" {
		it.Suggested = parseTime(suggested)
	}
	if checked != "" {
		it.Checked = parseTime(checked)
	}
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
	it.DoneWhen = strings.TrimSpace(it.DoneWhen)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return it, err
	}
	defer tx.Rollback()
	if it.DependsOn, err = checkDeps(ctx, tx, 0, it.DependsOn); err != nil {
		return it, err
	}
	rank, err := bottomRank(ctx, tx, it.State)
	if err != nil {
		return it, err
	}
	t := now()
	done := ""
	if it.State == store.NextDone {
		done = t
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO next_items (title, repo, plan, lane, lane_id, mr, state, rank, source, note, created, updated, done_at, done_when, depends_on)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Title, it.Repo, it.Plan, it.Lane, it.LaneID, it.MR, it.State, rank, it.Source, it.Note, t, t, done,
		it.DoneWhen, store.FormatDeps(it.DependsOn))
	if err != nil {
		return it, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return it, err
	}
	return s.NextItem(ctx, id)
}

// checkDeps returns deps without repeats and checks them for item id (0 for one not yet
// added): each must be another item, and the list must not close a cycle.
func checkDeps(ctx context.Context, tx *sql.Tx, id int64, deps []int64) ([]int64, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, depends_on FROM next_items`)
	if err != nil {
		return nil, err
	}
	all := map[int64][]int64{}
	for rows.Next() {
		var n int64
		var d string
		if err := rows.Scan(&n, &d); err != nil {
			rows.Close()
			return nil, err
		}
		all[n] = store.ParseDeps(d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	seen := map[int64]bool{}
	var out []int64
	for _, d := range deps {
		if seen[d] {
			continue
		}
		seen[d] = true
		if d == id {
			return nil, fmt.Errorf("%w: item %d cannot depend on itself", store.ErrConflict, id)
		}
		if _, ok := all[d]; !ok {
			return nil, fmt.Errorf("%w: no item %d to depend on", store.ErrConflict, d)
		}
		out = append(out, d)
	}
	if id != 0 && store.DependencyCycle(id, out, all) {
		return nil, fmt.Errorf("%w: those dependencies would make item %d wait on itself", store.ErrConflict, id)
	}
	return out, nil
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
	if p.DoneWhen != nil {
		it.DoneWhen = strings.TrimSpace(*p.DoneWhen)
	}
	if p.DependsOn != nil {
		if it.DependsOn, err = checkDeps(ctx, tx, id, *p.DependsOn); err != nil {
			return it, err
		}
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
	if p.Suggestion != nil {
		it.Suggestion = strings.TrimSpace(*p.Suggestion)
		it.Suggested = time.Time{}
		if it.Suggestion != "" {
			it.Suggested = time.Now().UTC()
		}
	}
	if p.Checked != nil {
		it.Checked = p.Checked.UTC()
	}
	// A suggestion is about an item still open: finishing or dropping it answers it.
	if it.State == store.NextDone || it.State == store.NextDropped {
		it.Suggestion, it.Suggested = "", time.Time{}
	}
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
	var suggested, checked string
	if !it.Suggested.IsZero() {
		suggested = it.Suggested.UTC().Format(time.RFC3339Nano)
	}
	if !it.Checked.IsZero() {
		checked = it.Checked.UTC().Format(time.RFC3339Nano)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE next_items SET title = ?, repo = ?, plan = ?, lane = ?, lane_id = ?, mr = ?,
		state = ?, rank = ?, note = ?, updated = ?, done_at = ?, done_when = ?, depends_on = ?, suggestion = ?,
		suggested = ?, checked = ? WHERE id = ?`,
		it.Title, it.Repo, it.Plan, it.Lane, it.LaneID, it.MR, it.State, it.Rank, it.Note, t, done,
		it.DoneWhen, store.FormatDeps(it.DependsOn), it.Suggestion, suggested, checked, id); err != nil {
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
