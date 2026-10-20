package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// crookTime is fixed width, so stored times compare as text.
const crookTime = "2006-01-02T15:04:05.000000000Z"

const crookCols = `id, at, action, actor, kind, rule, rule_version, stage, reason, repo, lane, run_id, facts`

func (s *DB) AddCrookVerdict(ctx context.Context, v store.CrookVerdict) (store.CrookVerdict, error) {
	if v.At.IsZero() {
		v.At = time.Now()
	}
	v.At = v.At.UTC()
	if v.Facts == "" {
		v.Facts = "{}"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO crook_verdicts
		(at, action, actor, kind, rule, rule_version, stage, reason, repo, lane, run_id, facts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.At.Format(crookTime), v.Action, v.Actor, v.Kind, v.Rule, v.RuleVersion, v.Stage, v.Reason, v.Repo, v.Lane, v.Run, v.Facts)
	if err != nil {
		return v, err
	}
	v.ID, err = res.LastInsertId()
	return v, err
}

type rowScanner interface{ Scan(...any) error }

func scanCrook(r rowScanner) (store.CrookVerdict, error) {
	var v store.CrookVerdict
	var at string
	err := r.Scan(&v.ID, &at, &v.Action, &v.Actor, &v.Kind, &v.Rule, &v.RuleVersion, &v.Stage, &v.Reason, &v.Repo, &v.Lane, &v.Run, &v.Facts)
	v.At = parseTime(at)
	return v, err
}

func (s *DB) CrookVerdict(ctx context.Context, id int64) (store.CrookVerdict, error) {
	v, err := scanCrook(s.db.QueryRowContext(ctx, `SELECT `+crookCols+` FROM crook_verdicts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, store.ErrNotFound
	}
	return v, err
}

func (s *DB) CrookVerdicts(ctx context.Context, f store.CrookFilter) ([]store.CrookVerdict, error) {
	var where []string
	var args []any
	add := func(cond string, a any) { where = append(where, cond); args = append(args, a) }
	if f.Rule != "" {
		add("rule = ?", f.Rule)
	}
	if f.Kind != "" {
		add("kind = ?", f.Kind)
	}
	if f.Action != "" {
		add("action = ?", f.Action)
	}
	if f.Lane != "" {
		add("lane = ?", f.Lane)
	}
	if f.Run != 0 {
		add("run_id = ?", f.Run)
	}
	if !f.Since.IsZero() {
		add("at >= ?", f.Since.UTC().Format(crookTime))
	}
	q := `SELECT ` + crookCols + ` FROM crook_verdicts`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.CrookVerdict
	for rows.Next() {
		v, err := scanCrook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *DB) CrookCounts(ctx context.Context, since time.Time) ([]store.CrookRuleCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rule, kind, COUNT(*) FROM crook_verdicts WHERE at >= ?
		GROUP BY rule, kind ORDER BY rule`, since.UTC().Format(crookTime))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.CrookRuleCount
	for rows.Next() {
		var rule, kind string
		var n int
		if err := rows.Scan(&rule, &kind, &n); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Rule != rule {
			out = append(out, store.CrookRuleCount{Rule: rule})
		}
		c := &out[len(out)-1]
		switch kind {
		case "proceed":
			c.Proceed += n
		case "ask":
			c.Ask += n
		case "hold":
			c.Hold += n
		case "refuse":
			c.Refuse += n
		}
	}
	return out, rows.Err()
}

func (s *DB) PruneCrookProceeds(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM crook_verdicts WHERE kind = 'proceed' AND at < ?`, before.UTC().Format(crookTime))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
