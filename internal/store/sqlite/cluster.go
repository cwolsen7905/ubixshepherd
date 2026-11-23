package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Members is the store's view as a store.MemberStore.
func (s *DB) Members() store.MemberStore { return memberStore{s} }

type memberStore struct{ s *DB }

var _ store.MemberStore = memberStore{}

func (m memberStore) AddMember(ctx context.Context, name, codeHash string, until time.Time) error {
	tx, err := m.s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM member_credentials WHERE member = ?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return store.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO cluster_members (name, code_hash, code_until, created) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET code_hash = excluded.code_hash, code_until = excluded.code_until`,
		name, codeHash, tstr(until), tstr(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}

func (m memberStore) Members(ctx context.Context) ([]store.Member, error) {
	rows, err := m.s.db.QueryContext(ctx, `SELECT name, daemon_id, pending_id, code_hash, code_until, version, api_level,
		capabilities, created, last_seen, EXISTS (SELECT 1 FROM member_credentials c WHERE c.member = cluster_members.name)
		FROM cluster_members ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Member
	for rows.Next() {
		var mb store.Member
		var code, until, caps, created, seen string
		if err := rows.Scan(&mb.Name, &mb.DaemonID, &mb.PendingID, &code, &until, &mb.Version, &mb.APILevel, &caps, &created, &seen, &mb.Enrolled); err != nil {
			return nil, err
		}
		if code != "" {
			mb.CodeUntil = parseTime(until)
		}
		json.Unmarshal([]byte(caps), &mb.Capabilities)
		mb.Created, mb.LastSeen = parseTime(created), parseTime(seen)
		out = append(out, mb)
	}
	return out, rows.Err()
}

func (m memberStore) Enrol(ctx context.Context, codeHash, daemonID, credID, credHash string, now time.Time) (string, error) {
	tx, err := m.s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var name string
	// Clearing the hash is what spends the code, so two enrolments with one code cannot
	// both find it.
	err = tx.QueryRowContext(ctx, `UPDATE cluster_members SET code_hash = '', code_until = '', daemon_id = ?, pending_id = ''
		WHERE code_hash = ? AND code_hash != '' AND code_until > ? RETURNING name`, daemonID, codeHash, tstr(now)).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO member_credentials (id, member, hash, created) VALUES (?, ?, ?, ?)`,
		credID, name, credHash, tstr(now)); err != nil {
		return "", err
	}
	return name, tx.Commit()
}

func (m memberStore) CredentialMember(ctx context.Context, credHash string) (string, error) {
	var name string
	err := m.s.db.QueryRowContext(ctx, `SELECT member FROM member_credentials WHERE hash = ?`, credHash).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	return name, err
}

func (m memberStore) Handshake(ctx context.Context, name, version string, apiLevel int, caps []string, seen time.Time) error {
	if caps == nil {
		caps = []string{}
	}
	b, _ := json.Marshal(caps)
	return m.exec(ctx, `UPDATE cluster_members SET version = ?, api_level = ?, capabilities = ?, last_seen = ? WHERE name = ?`,
		version, apiLevel, string(b), tstr(seen), name)
}

func (m memberStore) Seen(ctx context.Context, name string, seen time.Time) error {
	return m.exec(ctx, `UPDATE cluster_members SET last_seen = ? WHERE name = ?`, tstr(seen), name)
}

func (m memberStore) SetPendingID(ctx context.Context, name, id string) error {
	return m.exec(ctx, `UPDATE cluster_members SET pending_id = ? WHERE name = ?`, id, name)
}

func (m memberStore) ConfirmID(ctx context.Context, name string) error {
	return m.exec(ctx, `UPDATE cluster_members SET daemon_id = pending_id, pending_id = '' WHERE name = ? AND pending_id != ''`, name)
}

func (m memberStore) RevokeMember(ctx context.Context, name string) error {
	if _, err := m.s.db.ExecContext(ctx, `DELETE FROM member_credentials WHERE member = ?`, name); err != nil {
		return err
	}
	return m.exec(ctx, `UPDATE cluster_members SET daemon_id = '', pending_id = '' WHERE name = ?`, name)
}

func (m memberStore) RemoveMember(ctx context.Context, name string) error {
	return m.exec(ctx, `DELETE FROM cluster_members WHERE name = ?`, name)
}

// exec runs a write that must touch one member.
func (m memberStore) exec(ctx context.Context, q string, args ...any) error {
	res, err := m.s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}
