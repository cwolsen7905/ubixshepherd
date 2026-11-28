package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/store"
)

// tenancyKind says how a table's rows belong to a tenant.
type tenancyKind int

const (
	// rootTable rows carry tenant_id themselves.
	rootTable tenancyKind = iota + 1
	// childTable rows belong to the tenant of the parent row they point at.
	childTable
	// deploymentTable rows belong to the deployment, not to a tenant.
	deploymentTable
)

// tableTenancy is how each table belongs to a tenant. Every table in the schema must be
// listed (TestTenancyClassification fails otherwise), so a new table cannot skip tenancy:
// give it tenant_id in its migration and list it as a root, or list the parent and the
// column that reaches it.
//
// sharedKeys names unique keys that are still unique across all tenants. They are correct
// while there is one tenant; the change that lets a second tenant be made makes each unique
// within a tenant first. A filesystem path (a workspace's, a repo's, a worktree) stays
// unique across tenants: a path is one place on a machine whoever uses it.
//
// The map is also how queries are scoped (scope): a root table is filtered on its
// tenant_id, a child table through the chain of parents up to a root.
var tableTenancy = map[string]tableTenure{
	"workspaces":      {kind: rootTable, sharedKeys: []string{"name"}},
	"settings":        {kind: rootTable, sharedKeys: []string{"key"}},
	"feed":            {kind: rootTable},
	"spend":           {kind: rootTable},
	"conversations":   {kind: rootTable},
	"project_briefs":  {kind: rootTable, sharedKeys: []string{"one approved and one draft per project"}},
	"usage":           {kind: rootTable},
	"next_items":      {kind: rootTable},
	"held_starts":     {kind: rootTable},
	"topics":          {kind: rootTable},
	"crook_verdicts":  {kind: rootTable},
	"muster_items":    {kind: rootTable, sharedKeys: []string{"key"}},
	"cluster_members": {kind: rootTable, sharedKeys: []string{"name"}},

	"repos":              {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"lanes":              {kind: childTable, parent: "repos", via: "repo_id"},
	"runs":               {kind: childTable, parent: "lanes", via: "lane_id"},
	"events":             {kind: childTable, parent: "runs", via: "run_id"},
	"decisions":          {kind: childTable, parent: "runs", via: "run_id"},
	"requests":           {kind: childTable, parent: "runs", via: "from_run"},
	"run_diagnoses":      {kind: childTable, parent: "runs", via: "run_id"},
	"lane_forge":         {kind: childTable, parent: "lanes", via: "lane_id"},
	"gate_results":       {kind: childTable, parent: "lanes", via: "lane_id"},
	"keep_going_starts":  {kind: childTable, parent: "lanes", via: "lane_id"},
	"artifacts":          {kind: childTable, parent: "lanes", via: "lane_id"},
	"artifact_versions":  {kind: childTable, parent: "artifacts", via: "artifact_id"},
	"artifact_links":     {kind: childTable, parent: "artifacts", via: "artifact_id"},
	"reservations":       {kind: childTable, parent: "repos", via: "repo_id"},
	"check_files":        {kind: childTable, parent: "repos", via: "repo_id"},
	"keep_going":         {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"desk_events":        {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"desk_questions":     {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"desk_turn_log":      {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"desk_attachments":   {kind: childTable, parent: "workspaces", via: "workspace_id"},
	"topic_links":        {kind: childTable, parent: "topics", via: "topic_id", sharedKeys: []string{"kind, ref"}},
	"member_credentials": {kind: childTable, parent: "cluster_members", via: "member", key: "name"},

	// Who is in a tenant, with what role, is that tenant's own data.
	"memberships": {kind: rootTable},

	// The tenants themselves, and the people: users are global (one person, several
	// tenants, a membership each), and their factors, recovery codes, device tokens and
	// browser sessions belong to a user, not a tenant.
	"tenants":          {kind: deploymentTable},
	"users":            {kind: deploymentTable},
	"user_aliases":     {kind: deploymentTable},
	"account_factors":  {kind: deploymentTable},
	"account_recovery": {kind: deploymentTable},
	"device_tokens":    {kind: deploymentTable},
	"web_sessions":     {kind: deploymentTable},
}

// tableTenure is one table's entry in tableTenancy.
type tableTenure struct {
	kind tenancyKind
	// parent and via name, for a child table, the table it belongs to and its column
	// that holds the parent's key; key is that key's column in the parent, "id" when empty.
	parent, via, key string
	sharedKeys       []string
}

// tenantExpr is the SQL for the tenant of the row alias of table: the root's tenant_id,
// or a child's parent's, followed up the chain as correlated subqueries.
func tenantExpr(table, alias string) string {
	t, ok := tableTenancy[table]
	switch {
	case !ok:
		panic("tenantExpr: table " + table + " is not in tableTenancy")
	case t.kind == rootTable:
		return alias + ".tenant_id"
	case t.kind == childTable:
		key := t.key
		if key == "" {
			key = "id"
		}
		p := "tp_" + t.parent
		return fmt.Sprintf("(SELECT %s FROM %s %s WHERE %s.%s = %s.%s)", tenantExpr(t.parent, p), t.parent, p, p, key, alias, t.via)
	}
	panic("tenantExpr: " + table + " belongs to the deployment, not a tenant")
}

// ForTenant is the store seen by one tenant: every read and write of a tenant's table
// touches only that tenant's rows, and a new root row is that tenant's. The unscoped
// store (Open's) sees every tenant and puts new rows in the default one.
func (s *DB) ForTenant(id int64) store.Store {
	return &DB{db: s.db, tenant: id}
}

// Tenant is the tenant this store is scoped to, 0 when it is not.
func (s *DB) Tenant() int64 { return s.tenant }

// newRowTenant is the tenant a new root row gets: this store's, or the default one.
func (s *DB) newRowTenant() int64 {
	if s.tenant == 0 {
		return store.DefaultTenantID
	}
	return s.tenant
}

// scope is a condition, to AND into a WHERE clause, that the row alias of table is this
// store's tenant's, with its argument; on the unscoped store it is true and takes none.
func (s *DB) scope(table, alias string) (string, []any) {
	if s.tenant == 0 {
		return "1 = 1", nil
	}
	return tenantExpr(table, alias) + " = ?", []any{s.tenant}
}

// in is " AND <scope>" for a query on table under its own name, with its arguments; on
// the unscoped store it is empty, so the query is what it was before tenants.
func (s *DB) in(table string) (string, []any) {
	return s.inAs(table, table)
}

// inAs is in for a query that names table as alias.
func (s *DB) inAs(table, alias string) (string, []any) {
	if s.tenant == 0 {
		return "", nil
	}
	c, a := s.scope(table, alias)
	return " AND " + c, a
}

// mustOwn is nil when the row of table whose column is key is this tenant's (or exists, on
// the unscoped store), and store.ErrNotFound otherwise. On the unscoped store it checks
// nothing, so a missing parent fails as it did before tenants.
func (s *DB) mustOwn(ctx context.Context, q querier, table, column string, key any) error {
	if s.tenant == 0 {
		return nil
	}
	ok, err := s.owns(ctx, q, table, column, key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: no %s %v", store.ErrNotFound, strings.TrimSuffix(table, "s"), key)
	}
	return nil
}

// owns says whether the row of table whose key column is key belongs to this store's
// tenant. It is true on the unscoped store when the row exists.
func (s *DB) owns(ctx context.Context, q querier, table, column string, key any) (bool, error) {
	cond, args := s.scope(table, table)
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ? AND `+cond, append([]any{key}, args...)...).Scan(&n)
	return n > 0, err
}

// querier is a *sql.DB or a *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *DB) Tenants(ctx context.Context) ([]store.Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, created FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("tenants: %w", err)
	}
	defer rows.Close()
	var out []store.Tenant
	for rows.Next() {
		var t store.Tenant
		var created string
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &created); err != nil {
			return nil, err
		}
		t.Created = parseTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}
