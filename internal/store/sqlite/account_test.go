package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/auth"
)

func TestAccountSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	a := db.Accounts()
	if _, err := a.Account(ctx); err != auth.ErrNoAccount {
		t.Fatalf("empty store: %v", err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	a.PutAccount(ctx, auth.Account{PasswordHash: "$argon2id$x", Created: now, Updated: now})
	a.PutFactor(ctx, auth.FactorRecord{ID: "f1", Kind: "totp", Label: "owner", Secret: []byte{1, 2, 3}, Created: now})
	a.ReplaceRecovery(ctx, []auth.RecoveryCode{{ID: "r1", Hash: "h1"}, {ID: "r2", Hash: "h2"}})
	a.PutDevice(ctx, auth.Device{ID: "d1", Name: "laptop", Hash: "dh", Created: now, LastUsed: now, Expires: now.Add(auth.DeviceIdle)})
	if ok, _ := a.AdvanceStep(ctx, "f1", 10); !ok {
		t.Fatal("advance")
	}
	if ok, _ := a.AdvanceStep(ctx, "f1", 10); ok {
		t.Fatal("a step advanced twice")
	}
	if ok, _ := a.UseRecovery(ctx, "r1"); !ok {
		t.Fatal("use")
	}
	if ok, _ := a.UseRecovery(ctx, "r1"); ok {
		t.Fatal("a recovery code used twice")
	}
	db.Close()

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a = db.Accounts()
	if acc, err := a.Account(ctx); err != nil || acc.PasswordHash != "$argon2id$x" {
		t.Fatalf("%v %+v", err, acc)
	}
	fs, _ := a.Factors(ctx)
	if len(fs) != 1 || fs[0].LastStep != 10 || string(fs[0].Secret) != "\x01\x02\x03" {
		t.Fatalf("%+v", fs)
	}
	rc, _ := a.RecoveryCodes(ctx)
	if len(rc) != 2 || !rc[0].Used || rc[1].Used {
		t.Fatalf("%+v", rc)
	}
	d, found, _ := a.DeviceByHash(ctx, "dh")
	if !found || d.Name != "laptop" || !d.Expires.Equal(now.Add(auth.DeviceIdle)) {
		t.Fatalf("%+v", d)
	}
	if n, _ := a.DeleteDevices(ctx); n != 1 {
		t.Fatal(n)
	}
	if _, found, _ := a.DeviceByHash(ctx, "dh"); found {
		t.Fatal("device survived delete")
	}
	_ = strings.TrimSpace
}
