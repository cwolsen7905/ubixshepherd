package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"strings"
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
	undoUsers(t, db)
	for name, ten := range tableTenancy {
		if ten.kind != rootTable || name == "memberships" { // undoUsers dropped it
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

// undoUsers takes the store back from users to the one owner account, as tenancy phase
// T2 found it, for tests that step the schema back over it. The owner account table
// comes back empty: the hosted account migration that made it is re-runnable.
func undoUsers(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	qs := []string{}
	for _, tbl := range []string{"account_factors", "account_recovery", "device_tokens", "web_sessions"} {
		qs = append(qs, `DROP INDEX `+tbl+`_user`, `ALTER TABLE `+tbl+` DROP COLUMN user_id`)
	}
	qs = append(qs, `DROP TABLE memberships`, `DROP TABLE user_aliases`, `DROP TABLE users`,
		`CREATE TABLE owner_account (id INTEGER PRIMARY KEY CHECK (id = 1), password_hash TEXT NOT NULL, created TEXT NOT NULL, updated TEXT NOT NULL)`)
	for _, q := range qs {
		if _, err := db.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// notRefs are id-looking columns of tenant tables that do not hold the id of a tenant row,
// each with why. Anything else that looks like an id must be the table's via, tenant_id,
// or a declared ref in tableTenancy.
var notRefs = map[string]string{
	"feed.ref":                   "polymorphic by kind (a run, decision or request); a reader resolves it through its own tenant's view, where another tenant's id is not found",
	"muster_items.subject_id":    "polymorphic by subject_kind; resolved through the reader's own tenant's view like feed.ref",
	"topic_links.ref":            "text naming a thing by kind (a lane name, an MR); not a row id",
	"memberships.user_id":        "a user, which belongs to the deployment, not to a tenant",
	"artifact_links.ref":         "text whose meaning is its kind (a decision or topic id); listings follow it only to artifacts filtered by their own tenant",
	"cluster_members.daemon_id":  "the worker daemon's own id, not a row",
	"cluster_members.pending_id": "a daemon id waiting for confirmation, not a row",
}

// looksLikeID is the rule for a column that may hold the id of another row: its name ends
// in _id, _run or _into, or it is parent, ref or member. Names stored as text (repo, lane)
// and counters (turn, pid) are not ids.
func looksLikeID(col string) bool {
	for _, suf := range []string{"_id", "_run", "_into"} {
		if strings.HasSuffix(col, suf) {
			return true
		}
	}
	return col == "parent" || col == "ref" || col == "member"
}

// Every column of a tenant table that may hold another row's id is the table's via,
// tenant_id, a declared ref (which a tenant's view checks on write), or listed in
// notRefs with a reason, so a new reference column cannot be added unchecked. Each
// declared ref names a tenant table and a column the table has.
func TestTenancyRefsAreDeclared(t *testing.T) {
	db, _ := open(t)
	for name, ten := range tableTenancy {
		if ten.kind == deploymentTable {
			if len(ten.refs) > 0 {
				t.Errorf("deployment table %s declares refs", name)
			}
			continue
		}
		cols := columnNames(t, db.db, name)
		declared := map[string]bool{}
		for _, r := range ten.refs {
			declared[r.column] = true
			if !slices.Contains(cols, r.column) {
				t.Errorf("%s declares ref %s, which it has no column for", name, r.column)
			}
			if k := tableTenancy[r.table].kind; k != rootTable && k != childTable {
				t.Errorf("%s.%s refers to %s, which is not a tenant table", name, r.column, r.table)
			}
		}
		for _, c := range cols {
			if !looksLikeID(c) || c == "tenant_id" || c == ten.via || declared[c] {
				continue
			}
			if _, ok := notRefs[name+"."+c]; !ok {
				t.Errorf("%s.%s looks like the id of another row: declare it in tableTenancy's refs (a tenant's view then checks it on write), or list it in notRefs with why it is not one", name, c)
			}
		}
	}
	for k := range notRefs {
		table, col, _ := strings.Cut(k, ".")
		if !slices.Contains(columnNames(t, db.db, table), col) {
			t.Errorf("notRefs lists %s, which the schema does not have", k)
		}
	}
}

func TestForTenantRefusesNoTenant(t *testing.T) {
	db, _ := open(t)
	for _, id := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ForTenant(%d) did not panic", id)
				}
			}()
			db.ForTenant(id)
		}()
	}
	if got := db.ForTenant(3).Tenant(); got != 3 {
		t.Fatalf("ForTenant(3).Tenant() = %d", got)
	}
}
