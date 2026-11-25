package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestWorkerEnrolment(t *testing.T) {
	ctx := context.Background()
	db, path := open(t)
	m := db.Workers()
	now := time.Now()
	if err := m.AddWorker(ctx, "work", "codehash", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enrol(ctx, "wrong", "id1", "c1", "h1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := m.Enrol(ctx, "codehash", "id1", "c1", "h1", now.Add(6*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired code: %v", err)
	}
	if name, err := m.Enrol(ctx, "codehash", "id1", "c1", "h1", now); err != nil || name != "work" {
		t.Fatalf("enrol: %q %v", name, err)
	}
	if _, err := m.Enrol(ctx, "codehash", "id2", "c2", "h2", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a spent code worked again: %v", err)
	}
	if err := m.AddWorker(ctx, "work", "again", now.Add(time.Minute)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("add of an enrolled name: %v", err)
	}
	if n, err := m.CredentialWorker(ctx, "h1"); err != nil || n != "work" {
		t.Fatalf("credential: %q %v", n, err)
	}
	db.Close()
	db, err := Open(ctx, path) // a restart keeps them
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m = db.Workers()
	if err := m.Handshake(ctx, "work", "1.2.3", 4, []string{"status"}, now); err != nil {
		t.Fatal(err)
	}
	ms, _ := m.Workers(ctx)
	if len(ms) != 1 || !ms[0].Enrolled || ms[0].DaemonID != "id1" || ms[0].APILevel != 4 || ms[0].Capabilities[0] != "status" || !ms[0].CodeUntil.IsZero() {
		t.Fatalf("worker: %+v", ms)
	}
	if err := m.ConfirmID(ctx, "work"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("confirm with nothing pending: %v", err)
	}
	m.SetPendingID(ctx, "work", "id9")
	if err := m.ConfirmID(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if ms, _ = m.Workers(ctx); ms[0].DaemonID != "id9" || ms[0].PendingID != "" {
		t.Fatalf("after confirm: %+v", ms[0])
	}
	if err := m.RevokeWorker(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CredentialWorker(ctx, "h1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked credential: %v", err)
	}
	if ms, _ = m.Workers(ctx); len(ms) != 1 || ms[0].Enrolled {
		t.Fatalf("revoked worker: %+v", ms)
	}
	if err := m.RemoveWorker(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if ms, _ = m.Workers(ctx); len(ms) != 0 {
		t.Fatalf("removed worker: %+v", ms)
	}
}
