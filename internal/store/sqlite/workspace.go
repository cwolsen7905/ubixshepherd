package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// RenameWorkspace changes the name of the workspace with the given id. Its path, repos,
// lanes and history are keyed by id and stay as they are.
func (s *DB) RenameWorkspace(ctx context.Context, id int64, name string) (store.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Workspace{}, fmt.Errorf("workspace name must not be empty")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, err
	}
	defer tx.Rollback()

	var other int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name = ? AND id != ?`, name, id).Scan(&other)
	switch {
	case err == nil:
		return store.Workspace{}, fmt.Errorf("%w: workspace name %q is already used by another workspace", store.ErrConflict, name)
	case !errors.Is(err, sql.ErrNoRows):
		return store.Workspace{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		// A rename that raced past the check above still meets the UNIQUE column.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return store.Workspace{}, fmt.Errorf("%w: workspace name %q is already used by another workspace", store.ErrConflict, name)
		}
		return store.Workspace{}, fmt.Errorf("rename workspace %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.Workspace{}, fmt.Errorf("%w: no workspace %d", store.ErrNotFound, id)
	}
	var out store.Workspace
	var created string
	if err := tx.QueryRowContext(ctx, `SELECT id, name, path, created FROM workspaces WHERE id = ?`, id).
		Scan(&out.ID, &out.Name, &out.Path, &created); err != nil {
		return store.Workspace{}, err
	}
	out.Created = parseTime(created)
	return out, tx.Commit()
}
