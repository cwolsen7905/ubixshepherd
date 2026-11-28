package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
)

// Users is the store's directory of users, as an auth.Users.
func (s *DB) Users() auth.Users { return userDir{s} }

type userDir struct{ s *DB }

var _ auth.Users = userDir{}

func (d userDir) ForUser(id int64) auth.Store { return accountStore{d.s, id} }

// read fills one user's memberships and returns it. q selects id, name, password_hash,
// created and updated.
func (d userDir) read(ctx context.Context, q string, args ...any) (auth.User, error) {
	var u auth.User
	var hash, created, updated string
	err := d.s.db.QueryRowContext(ctx, q, args...).Scan(&u.ID, &u.Name, &hash, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrNoUser
	}
	if err != nil {
		return auth.User{}, err
	}
	u.HasPassword = hash != ""
	u.Created, u.Updated = parseTime(created), parseTime(updated)
	u.Members, err = d.memberships(ctx, u.ID)
	return u, err
}

func (d userDir) memberships(ctx context.Context, id int64) ([]auth.Membership, error) {
	rows, err := d.s.db.QueryContext(ctx, `SELECT t.slug, m.role FROM memberships m JOIN tenants t ON t.id = m.tenant_id WHERE m.user_id = ? ORDER BY t.slug`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auth.Membership{}
	for rows.Next() {
		var m auth.Membership
		if err := rows.Scan(&m.Tenant, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d userDir) UserByAlias(ctx context.Context, method, subject string) (auth.User, error) {
	return d.read(ctx, `SELECT u.id, u.name, u.password_hash, u.created, u.updated FROM users u
		JOIN user_aliases a ON a.user_id = u.id WHERE a.method = ? AND a.subject = ?`, method, subject)
}

func (d userDir) User(ctx context.Context, id int64) (auth.User, error) {
	return d.read(ctx, `SELECT id, name, password_hash, created, updated FROM users WHERE id = ?`, id)
}

func (d userDir) Users(ctx context.Context) ([]auth.User, error) {
	rows, err := d.s.db.QueryContext(ctx, `SELECT id FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []auth.User{}
	for _, id := range ids {
		u, err := d.User(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func tenantID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = ?`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, auth.ErrNoTenant
	}
	return id, err
}

func (d userDir) AddUser(ctx context.Context, name, tenant string, role auth.Role, now time.Time) (auth.User, error) {
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback()
	tid, err := tenantID(ctx, tx, tenant)
	if err != nil {
		return auth.User{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users (name, password_hash, created, updated) VALUES (?, '', ?, ?)`, name, tstr(now), tstr(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return auth.User{}, auth.ErrUserExists
		}
		return auth.User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return auth.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_aliases (user_id, method, subject) VALUES (?, ?, ?)`, id, auth.AliasLocal, name); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return auth.User{}, auth.ErrUserExists
		}
		return auth.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships (user_id, tenant_id, role) VALUES (?, ?, ?)`, id, tid, string(role)); err != nil {
		return auth.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return auth.User{}, err
	}
	return d.User(ctx, id)
}

// soleOwner says whether the user is the only owner of the tenant.
func soleOwner(ctx context.Context, tx *sql.Tx, id, tenant int64) (bool, error) {
	var owners, mine int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(user_id = ?), 0) FROM memberships WHERE tenant_id = ? AND role = 'owner'`, id, tenant).Scan(&owners, &mine)
	return owners == 1 && mine == 1, err
}

func (d userDir) RemoveUser(ctx context.Context, id int64) error {
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return auth.ErrNoUser
	}
	rows, err := tx.QueryContext(ctx, `SELECT tenant_id FROM memberships WHERE user_id = ? AND role = 'owner'`, id)
	if err != nil {
		return err
	}
	var owned []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		owned = append(owned, t)
	}
	rows.Close()
	for _, t := range owned {
		sole, err := soleOwner(ctx, tx, id, t)
		if err != nil {
			return err
		}
		if sole {
			return auth.ErrLastOwner
		}
	}
	for _, tbl := range []string{"account_factors", "account_recovery", "device_tokens", "web_sessions"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tbl+` WHERE user_id = ?`, id); err != nil {
			return err
		}
	}
	// Aliases and memberships go with the user (ON DELETE CASCADE).
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (d userDir) SetRole(ctx context.Context, id int64, tenant string, role auth.Role) error {
	tx, err := d.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return auth.ErrNoUser
	}
	tid, err := tenantID(ctx, tx, tenant)
	if err != nil {
		return err
	}
	if role != auth.RoleOwner {
		sole, err := soleOwner(ctx, tx, id, tid)
		if err != nil {
			return err
		}
		if sole {
			return auth.ErrLastOwner
		}
	}
	if role == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM memberships WHERE user_id = ? AND tenant_id = ?`, id, tid)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO memberships (user_id, tenant_id, role) VALUES (?, ?, ?)
			ON CONFLICT (user_id, tenant_id) DO UPDATE SET role = excluded.role`, id, tid, string(role))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (d userDir) DeviceUser(ctx context.Context, hash string) (int64, bool, error) {
	var id int64
	err := d.s.db.QueryRowContext(ctx, `SELECT user_id FROM device_tokens WHERE hash = ?`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}
