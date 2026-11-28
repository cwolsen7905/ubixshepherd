package daemon

// Every request is in one tenant. auth (auth.go) resolves the caller to a principal and
// resolveTenant puts it in a tenant with a role; handlers then read and write the store
// through db(ctx), which is that tenant's view. Background work (the watcher, routing,
// muster, the desk) runs on contexts no request made, and db gives it the unscoped store.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// errNoTenant is a request that names a tenant the caller cannot use: one that does not
// exist, or one they are not a member of. Both read the same, so the answer does not say
// which tenants exist.
var errNoTenant = errors.New("no such tenant, or you are not a member of it")

// db is the store as the request in ctx may see it: its tenant's view. A context no
// request made (background work) gets the unscoped store. Handlers use this and never
// s.Store; TestHandlersUseTheTenantStore keeps it so.
func (s *Server) db(ctx context.Context) store.Store {
	if t := principalOf(ctx).tenant; t > 0 {
		return s.Store.ForTenant(t)
	}
	return s.Store
}

// inTenant is a context whose db is the tenant's view, for work a request handed on to a
// goroutine of the daemon's (the Crook log's writer). 0 leaves ctx as it is.
func inTenant(ctx context.Context, tenant int64) context.Context {
	if tenant <= 0 {
		return ctx
	}
	return context.WithValue(ctx, principalKey{}, principal{tenant: tenant})
}

// scoped serves a handler built over a store (the api package's) with the request's
// tenant view.
func (s *Server) scoped(build func(store.Store) http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { build(s.rdb(r))(w, r) }
}

// rdb is db for a request.
func (s *Server) rdb(r *http.Request) store.Store { return s.db(r.Context()) }

// users is the store's user directory, or nil for a store that keeps none.
func (s *Server) users() auth.Users {
	if ap, ok := s.Store.(interface{ Users() auth.Users }); ok {
		return ap.Users()
	}
	return nil
}

// resolveTenant puts the principal in a tenant, with its role there. The tenant comes
// from api.TenantHeader, by slug:
//
//   - The operator token, and a loopback browser of the owner before there is an owner
//     account, are the system principal: any tenant the header names, else the default.
//   - Worker and desk tokens act in the default tenant, and are refused another.
//   - A browser session or device token is its user's: the one tenant they are a member
//     of, or with several the one the header names (the default tenant, when they are a
//     member of it and the header names none).
//
// The error is the answer's text; status is its code.
func (s *Server) resolveTenant(r *http.Request, p principal) (principal, int, error) {
	ctx := r.Context()
	want := strings.TrimSpace(r.Header.Get(api.TenantHeader))
	tenants, err := s.db(ctx).Tenants(ctx)
	if err != nil {
		return p, http.StatusInternalServerError, errors.New("could not read the tenants")
	}
	bySlug := map[string]store.Tenant{}
	for _, t := range tenants {
		bySlug[t.Slug] = t
	}
	def := store.Tenant{ID: store.DefaultTenantID, Slug: "default"}
	for _, t := range tenants {
		if t.ID == store.DefaultTenantID {
			def = t
		}
	}
	set := func(t store.Tenant, role auth.Role) principal {
		p.tenant, p.tenantSlug, p.role = t.ID, t.Slug, role
		return p
	}
	switch p.Role {
	case RoleWorker, RoleDesk:
		if want != "" && want != def.Slug {
			return p, http.StatusForbidden, fmt.Errorf("a %s token acts in tenant %s only", p.Role, def.Slug)
		}
		return set(def, ""), 0, nil
	case RoleWeb:
	default:
		// The operator, on loopback: the system principal.
		if want == "" {
			return set(def, ""), 0, nil
		}
		t, ok := bySlug[want]
		if !ok {
			return p, http.StatusNotFound, errNoTenant
		}
		return set(t, ""), 0, nil
	}
	members, err := s.memberships(ctx, r, p)
	if err != nil {
		return p, http.StatusUnauthorized, err
	}
	if members == nil {
		// The owner's loopback session before any account was set: the person on
		// their own machine, as the operator token is.
		if want == "" {
			return set(def, ""), 0, nil
		}
		t, ok := bySlug[want]
		if !ok {
			return p, http.StatusNotFound, errNoTenant
		}
		return set(t, ""), 0, nil
	}
	pick := want
	if pick == "" {
		switch {
		case len(members) == 1:
			pick = members[0].Tenant
		case len(members) > 1:
			for _, m := range members {
				if m.Tenant == def.Slug {
					pick = def.Slug
				}
			}
			if pick == "" {
				return p, http.StatusBadRequest, fmt.Errorf("you are a member of several tenants: name one with the %s header (shepherd --tenant)", api.TenantHeader)
			}
		}
	}
	for _, m := range members {
		if t, ok := bySlug[m.Tenant]; ok && m.Tenant == pick {
			return set(t, m.Role), 0, nil
		}
	}
	return p, http.StatusForbidden, errNoTenant
}

// memberships are the tenants a browser session's or device token's user belongs to.
// nil, with no error, is the owner on loopback before there is any account: a store with
// no users, or no row for the owner yet.
func (s *Server) memberships(ctx context.Context, r *http.Request, p principal) ([]auth.Membership, error) {
	id := p.userID()
	dir := s.users()
	if dir == nil {
		if isPublic(r) {
			return nil, errors.New("this store keeps no users")
		}
		return nil, nil
	}
	u, err := dir.User(ctx, id)
	switch {
	case errors.Is(err, auth.ErrNoUser) && !isPublic(r) && id == auth.OwnerID:
		return nil, nil
	case err != nil:
		return nil, errors.New("not signed in: your user is gone")
	}
	if len(u.Members) == 0 {
		return nil, errors.New("you are not a member of any tenant")
	}
	return u.Members, nil
}

// Role rules. A user's role in the tenant sets what they may do there; the system
// principal (operator, desk, worker) is not held to one, as its access bits already are.
var roleRank = map[auth.Role]int{auth.RoleViewer: 1, auth.RoleMember: 2, auth.RoleAdmin: 3, auth.RoleOwner: 4}

// atLeast says whether role is min or above it.
func atLeast(role, min auth.Role) bool { return roleRank[role] >= roleRank[min] }

// routeRule is what a route asks of a user: their least role in the tenant, and whether
// the route is the deployment's rather than a tenant's (the daemon's config, its binary,
// its cluster, its one front desk), so only the default tenant may call it for now.
type routeRule struct {
	min        auth.Role
	deployment bool
}

// ruleFor is the route's rule. A route missing from routeRules panics at registration, so
// a new route cannot be served unclassified.
func ruleFor(pattern string) routeRule {
	rule, ok := routeRules[pattern]
	if !ok {
		panic("route " + pattern + " has no entry in routeRules (tenant_roles.go)")
	}
	return rule
}

// roleVerdict says why the caller may not call a route under rule, or "" when they may.
func roleVerdict(p principal, rule routeRule) string {
	if rule.deployment && p.tenant != 0 && p.tenant != store.DefaultTenantID {
		return "this route is the deployment's: only the default tenant may use it"
	}
	if p.role == "" {
		return ""
	}
	if !atLeast(p.role, rule.min) {
		return fmt.Sprintf("your role in tenant %s is %s: this needs %s or above", p.tenantSlug, p.role, rule.min)
	}
	return ""
}

// ownedPaths are the path parameters that name a tenant's row, and how to find it in the
// tenant's view. owned answers a request naming another tenant's row 404, as if it did
// not exist, before the handler runs: handlers that hand an id to the Fold or the Runner
// (which keep the unscoped store) are safe by it.
var ownedPaths = []struct {
	prefix string
	find   func(ctx context.Context, st store.Store, id int64) error
}{
	{api.PathLanes + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Lane(ctx, id); return err }},
	{api.PathRuns + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Run(ctx, id); return err }},
	{api.PathRepos + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Repo(ctx, id); return err }},
	{api.PathWorkspaces + "/{id}", findWorkspace},
	{api.PathDecisions + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Decision(ctx, id); return err }},
	{api.PathRequests + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Request(ctx, id); return err }},
	{api.PathArtifacts + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Artifact(ctx, id); return err }},
	{api.PathNext + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.NextItem(ctx, id); return err }},
	{api.PathTopics + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Topic(ctx, id); return err }},
	{api.PathQuestions + "/{id}", func(ctx context.Context, st store.Store, id int64) error { _, err := st.Question(ctx, id); return err }},
}

// owned wraps a route whose pattern names a tenant's row by {id}: another tenant's id is
// 404 before h runs. An id no tenant has, or one that is not a number, is left to the
// handler, which answers it as it always has.
func (s *Server) owned(pattern string, h http.HandlerFunc) http.HandlerFunc {
	_, path, _ := strings.Cut(pattern, " ")
	for _, o := range ownedPaths {
		if path != o.prefix && !strings.HasPrefix(path, o.prefix+"/") {
			continue
		}
		find, what := o.find, noun(o.prefix)
		return func(w http.ResponseWriter, r *http.Request) {
			if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil && s.elsewhere(r, find, id) {
				writeError(w, http.StatusNotFound, fmt.Errorf("no %s %d", what, id))
				return
			}
			h(w, r)
		}
	}
	return h
}

// noun is what a path's {id} names, for the 404's words: "lane" for /v1/lanes/{id}.
func noun(prefix string) string {
	p := strings.TrimSuffix(prefix, "/{id}")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, "s")
}

// ref is a row a request's body or query names by id, for ours.
type ref struct {
	what string
	id   int64
}

func repoRef(id int64) ref { return ref{"repo", id} }
func laneRef(id int64) ref { return ref{"lane", id} }
func runRef(id int64) ref  { return ref{"run", id} }
func itemRef(id int64) ref { return ref{"next-up item", id} }
func wsRef(id int64) ref   { return ref{"workspace", id} }

// ours says whether no row the request names is another tenant's, answering 404 when one
// is. A handler calls it before it hands an id from its body to the Fold or the
// Runner, which keep the unscoped store; an id of 0 names nothing and passes.
func (s *Server) ours(w http.ResponseWriter, r *http.Request, refs ...ref) bool {
	for _, x := range refs {
		if x.id == 0 {
			continue
		}
		find := map[string]func(context.Context, store.Store, int64) error{
			"repo":         func(ctx context.Context, st store.Store, id int64) error { _, err := st.Repo(ctx, id); return err },
			"lane":         func(ctx context.Context, st store.Store, id int64) error { _, err := st.Lane(ctx, id); return err },
			"run":          func(ctx context.Context, st store.Store, id int64) error { _, err := st.Run(ctx, id); return err },
			"next-up item": func(ctx context.Context, st store.Store, id int64) error { _, err := st.NextItem(ctx, id); return err },
			"workspace":    findWorkspace,
		}[x.what]
		if s.elsewhere(r, find, x.id) {
			writeError(w, http.StatusNotFound, fmt.Errorf("no %s %d", x.what, x.id))
			return false
		}
	}
	return true
}

// findWorkspace finds a workspace in st: nil, or ErrNotFound.
func findWorkspace(ctx context.Context, st store.Store, id int64) error {
	wss, err := st.Workspaces(ctx)
	if err != nil {
		return err
	}
	for _, ws := range wss {
		if ws.ID == id {
			return nil
		}
	}
	return store.ErrNotFound
}

// elsewhere says whether the row is another tenant's: missing from the request's view but
// in the store. A request outside any tenant (none is resolved) sees every row.
func (s *Server) elsewhere(r *http.Request, find func(context.Context, store.Store, int64) error, id int64) bool {
	ctx := r.Context()
	if principalOf(ctx).tenant <= 0 {
		return false
	}
	if find(ctx, s.rdb(r), id) == nil {
		return false
	}
	return find(ctx, s.Store, id) == nil
}
