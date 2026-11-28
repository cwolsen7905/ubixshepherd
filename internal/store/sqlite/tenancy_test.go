package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func columnNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTenancyClassification fails on a table the schema has and tableTenancy does not
// list, so a new table cannot skip tenancy, and checks each entry against the schema.
func TestTenancyClassification(t *testing.T) {
	db, _ := open(t)
	tables := tableNames(t, db.db)

	for _, name := range tables {
		if _, ok := tableTenancy[name]; !ok {
			t.Errorf("table %s is not in tableTenancy: list it as a root (with tenant_id), a child (with its parent and column) or a deployment table", name)
		}
	}
	for name, ten := range tableTenancy {
		if !slices.Contains(tables, name) {
			t.Errorf("tableTenancy lists %s, which the schema does not have", name)
			continue
		}
		cols := columnNames(t, db.db, name)
		hasTenant := slices.Contains(cols, "tenant_id")
		switch ten.kind {
		case rootTable:
			if !hasTenant {
				t.Errorf("root table %s has no tenant_id column", name)
			}
		case childTable:
			if hasTenant {
				t.Errorf("child table %s has its own tenant_id; list it as a root", name)
			}
			p, ok := tableTenancy[ten.parent]
			if !ok {
				t.Errorf("child table %s names parent %q, which is not in tableTenancy", name, ten.parent)
			} else if p.kind == deploymentTable {
				t.Errorf("child table %s has a deployment table, %s, as its parent", name, ten.parent)
			}
			if !slices.Contains(cols, ten.via) {
				t.Errorf("child table %s has no column %q to reach %s", name, ten.via, ten.parent)
			}
		case deploymentTable:
			if hasTenant {
				t.Errorf("deployment table %s has tenant_id; list it as a root", name)
			}
		default:
			t.Errorf("table %s has no tenancy kind", name)
		}
	}

	// Every child must reach a root by following parents, with no loop.
	for name, ten := range tableTenancy {
		seen := map[string]bool{}
		for ten.kind == childTable {
			if seen[name] {
				t.Errorf("tableTenancy has a loop through %s", name)
				break
			}
			seen[name] = true
			name = ten.parent
			ten = tableTenancy[name]
		}
	}
}

func TestDefaultTenant(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)

	got, err := db.Tenants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("tenants = %+v, want only the default one", got)
	}
	if got[0].ID != store.DefaultTenantID || got[0].Slug != "default" || got[0].Created.IsZero() {
		t.Errorf("default tenant = %+v", got[0])
	}
}

// TestNewRowsJoinTheDefaultTenant writes through the store, which names no tenant, and
// finds the rows in the default one.
func TestNewRowsJoinTheDefaultTenant(t *testing.T) {
	ctx := context.Background()
	db, _ := open(t)

	if _, err := db.SaveWorkspace(ctx, store.Workspace{Name: "w", Path: "/w"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(ctx, "desk.model", "m"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workspaces", "settings"} {
		var n int
		if err := db.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id != ?`, store.DefaultTenantID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows outside the default tenant", table, n)
		}
	}
}

// undoTenants takes the tenancy migration back out of a fresh database, for the tests that
// step the schema back to an older version and open it again.
func undoTenants(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	for name, ten := range tableTenancy {
		if ten.kind != rootTable {
			continue
		}
		for _, q := range []string{`DROP INDEX ` + name + `_tenant`, `ALTER TABLE ` + name + ` DROP COLUMN tenant_id`} {
			if _, err := db.db.ExecContext(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	if _, err := db.db.ExecContext(ctx, `DROP TABLE tenants`); err != nil {
		t.Fatal(err)
	}
}
