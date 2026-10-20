package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func TestCrookVerdictLog(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	now := time.Now().UTC()
	add := func(kind, rule, lane string, run int64, ago time.Duration) store.CrookVerdict {
		v, err := db.AddCrookVerdict(ctx, store.CrookVerdict{At: now.Add(-ago), Action: "push.agent", Actor: "worker", Kind: kind,
			Rule: rule, RuleVersion: 1, Stage: "policy", Reason: "r", Lane: lane, Run: run})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	old := add("proceed", "builtin/a", "l1", 1, 100*24*time.Hour)
	oldRefuse := add("refuse", "builtin/a", "l1", 1, 100*24*time.Hour)
	add("proceed", "builtin/a", "l1", 1, time.Hour)
	add("refuse", "builtin/b", "l2", 2, time.Minute)
	last := add("hold", "builtin/b", "l2", 2, 0)

	got, err := db.CrookVerdict(ctx, last.ID)
	if err != nil || got.Kind != "hold" || got.Facts != "{}" || got.Lane != "l2" || got.Run != 2 || got.RuleVersion != 1 || got.At.Before(now.Add(-time.Second)) {
		t.Fatalf("one: %+v %v", got, err)
	}
	if _, err := db.CrookVerdict(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	all, err := db.CrookVerdicts(ctx, store.CrookFilter{})
	if err != nil || len(all) != 5 || all[0].ID != last.ID {
		t.Fatalf("newest first: %d %v", len(all), err)
	}
	for name, c := range map[string]struct {
		f    store.CrookFilter
		want int
	}{
		"rule": {store.CrookFilter{Rule: "builtin/b"}, 2}, "kind": {store.CrookFilter{Kind: "refuse"}, 2},
		"kind since": {store.CrookFilter{Kind: "refuse", Since: now.Add(-time.Hour * 2)}, 1},
		"lane":       {store.CrookFilter{Lane: "l2"}, 2}, "run": {store.CrookFilter{Run: 2}, 2},
	} {
		if vs, _ := db.CrookVerdicts(ctx, c.f); len(vs) != c.want {
			t.Errorf("%s: %d, want %d", name, len(vs), c.want)
		}
	}
	if vs, _ := db.CrookVerdicts(ctx, store.CrookFilter{Limit: 2}); len(vs) != 2 {
		t.Errorf("limit: %d", len(vs))
	}
	counts, err := db.CrookCounts(ctx, now.Add(-7*24*time.Hour))
	if err != nil || len(counts) != 2 || counts[0] != (store.CrookRuleCount{Rule: "builtin/a", Proceed: 1}) ||
		counts[1] != (store.CrookRuleCount{Rule: "builtin/b", Hold: 1, Refuse: 1}) {
		t.Fatalf("counts: %+v %v", counts, err)
	}
	n, err := db.PruneCrookProceeds(ctx, now.Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if _, err := db.CrookVerdict(ctx, old.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the old proceed should be gone: %v", err)
	}
	if _, err := db.CrookVerdict(ctx, oldRefuse.ID); err != nil {
		t.Fatalf("an old refusal is kept: %v", err)
	}
}
