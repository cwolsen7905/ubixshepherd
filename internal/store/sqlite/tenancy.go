package sqlite

import (
	"context"
	"fmt"

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
// while there is one tenant; query scoping (tenancy phase T3) makes each unique within a
// tenant before a second tenant can be made.
var tableTenancy = map[string]tableTenure{
	"workspaces":      {kind: rootTable, sharedKeys: []string{"name", "path"}},
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

	"repos":              {kind: childTable, parent: "workspaces", via: "workspace_id", sharedKeys: []string{"path"}},
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
	"member_credentials": {kind: childTable, parent: "cluster_members", via: "member"},

	// The tenants themselves, and the one owner account with its factors, recovery codes,
	// device tokens and browser sessions. Tenancy phase T2 gives the account tables a user.
	"tenants":          {kind: deploymentTable},
	"owner_account":    {kind: deploymentTable},
	"account_factors":  {kind: deploymentTable},
	"account_recovery": {kind: deploymentTable},
	"device_tokens":    {kind: deploymentTable},
	"web_sessions":     {kind: deploymentTable},
}

// tableTenure is one table's entry in tableTenancy.
type tableTenure struct {
	kind tenancyKind
	// parent and via name, for a child table, the table it belongs to and its column
	// that holds the parent's key.
	parent, via string
	sharedKeys  []string
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
