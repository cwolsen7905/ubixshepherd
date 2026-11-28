package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Accounts is the store's view as an auth.Store for the owner (user 1): their password,
// second factors, recovery codes and device tokens. It is a separate interface from
// store.Store, which other stores need not implement. Users is every user's.
func (s *DB) Accounts() auth.Store { return accountStore{s, auth.OwnerID} }

// accountStore is one user's account: every query names user.
type accountStore struct {
	s    *DB
	user int64
}

var _ auth.Store = accountStore{}

func (a accountStore) Account(ctx context.Context) (auth.Account, error) {
	var acc auth.Account
	var created, updated string
	err := a.s.db.QueryRowContext(ctx, `SELECT password_hash, created, updated FROM users WHERE id = ?`, a.user).Scan(&acc.PasswordHash, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && acc.PasswordHash == "") {
		return auth.Account{}, auth.ErrNoAccount
	}
	acc.Created, acc.Updated = parseTime(created), parseTime(updated)
	return acc, err
}

// PutAccount sets the user's password. The owner is made on first use, as `account set`
// on a new store does: user 1, alias local/owner, the owner of the default tenant.
func (a accountStore) PutAccount(ctx context.Context, acc auth.Account) error {
	tx, err := a.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated = ? WHERE id = ?`, acc.PasswordHash, tstr(acc.Updated), a.user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if a.user != auth.OwnerID {
			return auth.ErrNoUser
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO users (id, name, password_hash, created, updated) VALUES (?, ?, ?, ?, ?)`, []any{auth.OwnerID, auth.AccountName, acc.PasswordHash, tstr(acc.Created), tstr(acc.Updated)}},
			{`INSERT INTO user_aliases (user_id, method, subject) VALUES (?, ?, ?)`, []any{auth.OwnerID, auth.AliasLocal, auth.AccountName}},
			{`INSERT INTO memberships (user_id, tenant_id, role) VALUES (?, ?, ?)`, []any{auth.OwnerID, store.DefaultTenantID, string(auth.RoleOwner)}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (a accountStore) Factors(ctx context.Context) ([]auth.FactorRecord, error) {
	rows, err := a.s.db.QueryContext(ctx, `SELECT id, kind, label, secret, created, last_step FROM account_factors WHERE user_id = ? ORDER BY created, id`, a.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.FactorRecord
	for rows.Next() {
		var f auth.FactorRecord
		var created string
		if err := rows.Scan(&f.ID, &f.Kind, &f.Label, &f.Secret, &created, &f.LastStep); err != nil {
			return nil, err
		}
		f.Created = parseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (a accountStore) PutFactor(ctx context.Context, f auth.FactorRecord) error {
	_, err := a.s.db.ExecContext(ctx, `INSERT OR REPLACE INTO account_factors (id, kind, label, secret, created, last_step, user_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Kind, f.Label, f.Secret, tstr(f.Created), f.LastStep, a.user)
	return err
}

func (a accountStore) DeleteFactor(ctx context.Context, id string) error {
	_, err := a.s.db.ExecContext(ctx, `DELETE FROM account_factors WHERE id = ? AND user_id = ?`, id, a.user)
	return err
}

func (a accountStore) AdvanceStep(ctx context.Context, id string, step int64) (bool, error) {
	res, err := a.s.db.ExecContext(ctx, `UPDATE account_factors SET last_step = ? WHERE id = ? AND user_id = ? AND last_step < ?`, step, id, a.user, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (a accountStore) ReplaceRecovery(ctx context.Context, codes []auth.RecoveryCode) error {
	tx, err := a.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_recovery WHERE user_id = ?`, a.user); err != nil {
		return err
	}
	for i, c := range codes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_recovery (id, hash, used, seq, user_id) VALUES (?, ?, ?, ?, ?)`, c.ID, c.Hash, b2i(c.Used), i, a.user); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a accountStore) RecoveryCodes(ctx context.Context) ([]auth.RecoveryCode, error) {
	rows, err := a.s.db.QueryContext(ctx, `SELECT id, hash, used FROM account_recovery WHERE user_id = ? ORDER BY seq`, a.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.RecoveryCode
	for rows.Next() {
		var c auth.RecoveryCode
		var used int
		if err := rows.Scan(&c.ID, &c.Hash, &used); err != nil {
			return nil, err
		}
		c.Used = used != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a accountStore) UseRecovery(ctx context.Context, id string) (bool, error) {
	res, err := a.s.db.ExecContext(ctx, `UPDATE account_recovery SET used = 1 WHERE id = ? AND user_id = ? AND used = 0`, id, a.user)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (a accountStore) PutDevice(ctx context.Context, d auth.Device) error {
	_, err := a.s.db.ExecContext(ctx, `INSERT OR REPLACE INTO device_tokens (id, name, hash, created, last_used, expires, user_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.Hash, tstr(d.Created), tstr(d.LastUsed), tstr(d.Expires), a.user)
	return err
}

func scanDevice(sc interface{ Scan(...any) error }) (auth.Device, error) {
	var d auth.Device
	var created, used, expires string
	if err := sc.Scan(&d.ID, &d.Name, &d.Hash, &created, &used, &expires); err != nil {
		return auth.Device{}, err
	}
	d.Created, d.LastUsed, d.Expires = parseTime(created), parseTime(used), parseTime(expires)
	return d, nil
}

func (a accountStore) DeviceByHash(ctx context.Context, hash string) (auth.Device, bool, error) {
	d, err := scanDevice(a.s.db.QueryRowContext(ctx, `SELECT id, name, hash, created, last_used, expires FROM device_tokens WHERE hash = ? AND user_id = ?`, hash, a.user))
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Device{}, false, nil
	}
	return d, err == nil, err
}

func (a accountStore) TouchDevice(ctx context.Context, id string, used, expires time.Time) error {
	_, err := a.s.db.ExecContext(ctx, `UPDATE device_tokens SET last_used = ?, expires = ? WHERE id = ? AND user_id = ?`, tstr(used), tstr(expires), id, a.user)
	return err
}

func (a accountStore) Devices(ctx context.Context) ([]auth.Device, error) {
	rows, err := a.s.db.QueryContext(ctx, `SELECT id, name, hash, created, last_used, expires FROM device_tokens WHERE user_id = ? ORDER BY created, id`, a.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a accountStore) DeleteDevice(ctx context.Context, id string) (bool, error) {
	res, err := a.s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE id = ? AND user_id = ?`, id, a.user)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (a accountStore) DeleteDevices(ctx context.Context) (int, error) {
	res, err := a.s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE user_id = ?`, a.user)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (a accountStore) PruneDevices(ctx context.Context, now time.Time) error {
	_, err := a.s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE expires <= ?`, tstr(now))
	return err
}
