package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// The owner account becomes user 1, `owner`, the owner of `default`, keeping its password,
// factor, recovery codes, device token and browser session: nobody signs in again.
func TestOwnerAccountBecomesTheOwnerUser(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	undoUsers(t, db) // as a store one release old left it
	now := "2026-11-05T12:00:00Z"
	for _, q := range []string{
		`INSERT INTO owner_account (id, password_hash, created, updated) VALUES (1, '$argon2id$x', '` + now + `', '` + now + `')`,
		`INSERT INTO account_factors (id, kind, label, secret, created, last_step) VALUES ('f1', 'totp', 'owner', x'010203', '` + now + `', 7)`,
		`INSERT INTO account_recovery (id, hash, used, seq) VALUES ('r1', 'h1', 0, 0)`,
		`INSERT INTO device_tokens (id, name, hash, created, last_used, expires) VALUES ('d1', 'laptop', 'dh', '` + now + `', '` + now + `', '2099-01-01T00:00:00Z')`,
		`INSERT INTO web_sessions (hash, csrf, port, created, last_seen, expires) VALUES ('sh', 'c', 'public', '` + now + `', '` + now + `', '2099-01-01T00:00:00Z')`,
		fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)-1),
	} {
		if _, err := db.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer db.Close()
	u, err := db.Users().UserByAlias(ctx, auth.AliasLocal, "owner")
	if err != nil || u.ID != auth.OwnerID || !u.HasPassword || len(u.Members) != 1 || u.Members[0] != (auth.Membership{Tenant: "default", Role: auth.RoleOwner}) {
		t.Fatalf("owner user = %+v, %v", u, err)
	}
	a := db.Accounts()
	if acc, err := a.Account(ctx); err != nil || acc.PasswordHash != "$argon2id$x" {
		t.Fatalf("account = %+v, %v", acc, err)
	}
	if fs, _ := a.Factors(ctx); len(fs) != 1 || fs[0].LastStep != 7 {
		t.Fatalf("factors = %+v", fs)
	}
	if rc, _ := a.RecoveryCodes(ctx); len(rc) != 1 {
		t.Fatalf("recovery = %+v", rc)
	}
	if _, found, _ := a.DeviceByHash(ctx, "dh"); !found {
		t.Fatal("the device token is gone")
	}
	ws, err := db.LiveWebSessions(ctx, time.Now())
	if err != nil || len(ws) != 1 || ws[0].UserID != auth.OwnerID {
		t.Fatalf("sessions = %+v, %v", ws, err)
	}
}

// A store with no owner account migrates to no users; `account set` makes the owner.
func TestAccountSetMakesTheOwnerUser(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	if us, err := db.Users().Users(ctx); err != nil || len(us) != 0 {
		t.Fatalf("users = %+v, %v", us, err)
	}
	now := time.Now().UTC()
	if err := db.Accounts().PutAccount(ctx, auth.Account{PasswordHash: "h", Created: now, Updated: now}); err != nil {
		t.Fatal(err)
	}
	u, err := db.Users().User(ctx, auth.OwnerID)
	if err != nil || u.Name != "owner" || u.Members[0].Role != auth.RoleOwner {
		t.Fatalf("owner = %+v, %v", u, err)
	}
	// Setting it again updates the one row.
	if err := db.Accounts().PutAccount(ctx, auth.Account{PasswordHash: "h2", Created: now, Updated: now}); err != nil {
		t.Fatal(err)
	}
	if acc, _ := db.Accounts().Account(ctx); acc.PasswordHash != "h2" {
		t.Fatalf("account = %+v", acc)
	}
}

// Each user's account store holds only their own factors, recovery codes and device
// tokens; removing a user takes all of it; the last owner of a tenant stays.
func TestUsersAreKeptApart(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)
	dir := db.Users()
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	owner := db.Accounts()
	owner.PutAccount(ctx, auth.Account{PasswordHash: "ho", Created: now, Updated: now})
	alice, err := dir.AddUser(ctx, "alice", "default", auth.RoleMember, now)
	if err != nil {
		t.Fatal(err)
	}
	if alice.HasPassword {
		t.Fatal("a new user has a password")
	}
	if _, err := dir.AddUser(ctx, "alice", "default", auth.RoleMember, now); !errors.Is(err, auth.ErrUserExists) {
		t.Fatalf("second alice: %v", err)
	}
	if _, err := dir.AddUser(ctx, "bob", "nowhere", auth.RoleMember, now); !errors.Is(err, auth.ErrNoTenant) {
		t.Fatalf("unknown tenant: %v", err)
	}
	as := dir.ForUser(alice.ID)
	if _, err := as.Account(ctx); !errors.Is(err, auth.ErrNoAccount) {
		t.Fatalf("alice before a password: %v", err)
	}
	as.PutAccount(ctx, auth.Account{PasswordHash: "ha", Created: now, Updated: now})

	for i, st := range []auth.Store{owner, as} {
		id := fmt.Sprint(i)
		st.PutFactor(ctx, auth.FactorRecord{ID: "f" + id, Kind: "totp", Secret: []byte{1}, Created: now})
		st.ReplaceRecovery(ctx, []auth.RecoveryCode{{ID: "r" + id, Hash: "h"}})
		st.PutDevice(ctx, auth.Device{ID: "d" + id, Name: "n", Hash: "dh" + id, Created: now, LastUsed: now, Expires: now.Add(time.Hour)})
	}
	if acc, _ := as.Account(ctx); acc.PasswordHash != "ha" {
		t.Fatalf("alice's password = %q", acc.PasswordHash)
	}
	if acc, _ := owner.Account(ctx); acc.PasswordHash != "ho" {
		t.Fatalf("owner's password = %q", acc.PasswordHash)
	}
	for _, c := range []struct {
		st   auth.Store
		mine string
	}{{owner, "0"}, {as, "1"}} {
		fs, _ := c.st.Factors(ctx)
		rc, _ := c.st.RecoveryCodes(ctx)
		ds, _ := c.st.Devices(ctx)
		if len(fs) != 1 || fs[0].ID != "f"+c.mine || len(rc) != 1 || rc[0].ID != "r"+c.mine || len(ds) != 1 || ds[0].ID != "d"+c.mine {
			t.Fatalf("user %s sees factors %+v, recovery %+v, devices %+v", c.mine, fs, rc, ds)
		}
	}
	// Nothing of one user's can be reached or changed through another's store.
	if _, found, _ := as.DeviceByHash(ctx, "dh0"); found {
		t.Fatal("alice found the owner's device by its hash")
	}
	if ok, _ := as.DeleteDevice(ctx, "d0"); ok {
		t.Fatal("alice deleted the owner's device")
	}
	if ok, _ := as.UseRecovery(ctx, "r0"); ok {
		t.Fatal("alice used the owner's recovery code")
	}
	if n, _ := as.DeleteDevices(ctx); n != 1 {
		t.Fatalf("alice's sign-out-everywhere ended %d devices", n)
	}
	if ds, _ := owner.Devices(ctx); len(ds) != 1 {
		t.Fatalf("the owner's devices after alice's sign-out = %+v", ds)
	}
	if id, found, _ := dir.DeviceUser(ctx, "dh0"); !found || id != auth.OwnerID {
		t.Fatalf("whose device dh0: %d %v", id, found)
	}

	// The owner is the only owner of default: they can neither be removed nor demoted.
	if err := dir.RemoveUser(ctx, auth.OwnerID); !errors.Is(err, auth.ErrLastOwner) {
		t.Fatalf("removing the last owner: %v", err)
	}
	if err := dir.SetRole(ctx, auth.OwnerID, "default", auth.RoleAdmin); !errors.Is(err, auth.ErrLastOwner) {
		t.Fatalf("demoting the last owner: %v", err)
	}
	if err := dir.SetRole(ctx, alice.ID, "default", auth.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetRole(ctx, auth.OwnerID, "default", auth.RoleAdmin); err != nil {
		t.Fatalf("demoting one of two owners: %v", err)
	}
	if err := dir.SetRole(ctx, auth.OwnerID, "default", auth.RoleOwner); err != nil {
		t.Fatal(err)
	}

	db.SaveWebSession(ctx, store.WebSession{Hash: "s1", Created: now, LastSeen: now, Expires: now.Add(time.Hour), UserID: alice.ID})
	if err := dir.RemoveUser(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.User(ctx, alice.ID); !errors.Is(err, auth.ErrNoUser) {
		t.Fatalf("alice after removal: %v", err)
	}
	for _, tbl := range []string{"account_factors", "account_recovery", "device_tokens", "web_sessions", "user_aliases", "memberships"} {
		var n int
		db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl+` WHERE user_id = ?`, alice.ID).Scan(&n)
		if n != 0 {
			t.Errorf("%s keeps %d rows of a removed user", tbl, n)
		}
	}
	if fs, _ := owner.Factors(ctx); len(fs) != 1 {
		t.Fatalf("the owner's factors after alice went = %+v", fs)
	}
}
