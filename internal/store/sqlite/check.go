package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

const checkCols = `id, repo_id, path, blob, base_commit, item_id, set_name, approved, approved_by, released, release_why`

func scanCheck(sc interface{ Scan(...any) error }) (store.CheckFile, error) {
	var c store.CheckFile
	var approved, released string
	err := sc.Scan(&c.ID, &c.RepoID, &c.Path, &c.Blob, &c.BaseCommit, &c.ItemID, &c.Set, &approved, &c.ApprovedBy, &released, &c.ReleaseWhy)
	c.Approved = parseTime(approved)
	if released != "" {
		t := parseTime(released)
		c.Released = &t
	}
	return c, err
}

func (s *DB) CheckFiles(ctx context.Context, repoID int64, liveOnly bool) ([]store.CheckFile, error) {
	q := `SELECT ` + checkCols + ` FROM check_files WHERE repo_id = ?`
	if liveOnly {
		q += ` AND released = ''`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.CheckFile
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *DB) ApproveCheckFiles(ctx context.Context, files []store.CheckFile) ([]store.CheckFile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t := now()
	var out []store.CheckFile
	for _, f := range files {
		if f.Path == "" || f.Blob == "" || f.BaseCommit == "" || f.ApprovedBy == "" {
			return nil, fmt.Errorf("a check file needs a path, a blob, a base commit and an approver")
		}
		var live int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM check_files WHERE repo_id = ? AND path = ? AND released = ''`, f.RepoID, f.Path).Scan(&live); err != nil {
			return nil, err
		}
		if live > 0 {
			return nil, fmt.Errorf("%w: %s is already an approved check file", store.ErrConflict, f.Path)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO check_files (repo_id, path, blob, base_commit, item_id, set_name, approved, approved_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, f.RepoID, f.Path, f.Blob, f.BaseCommit, f.ItemID, f.Set, t, f.ApprovedBy)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		c, err := scanCheck(tx.QueryRowContext(ctx, `SELECT `+checkCols+` FROM check_files WHERE id = ?`, id))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, tx.Commit()
}

func (s *DB) ReleaseCheckFiles(ctx context.Context, repoID int64, paths []string, why string) ([]store.CheckFile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t := now()
	var out []store.CheckFile
	for _, p := range paths {
		c, err := scanCheck(tx.QueryRowContext(ctx, `SELECT `+checkCols+` FROM check_files WHERE repo_id = ? AND path = ? AND released = ''`, repoID, p))
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				return nil, fmt.Errorf("%w: %s is not an approved check file", store.ErrNotFound, p)
			}
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE check_files SET released = ?, release_why = ? WHERE id = ?`, t, why, c.ID); err != nil {
			return nil, err
		}
		rel := parseTime(t)
		c.Released, c.ReleaseWhy = &rel, why
		out = append(out, c)
	}
	return out, tx.Commit()
}
