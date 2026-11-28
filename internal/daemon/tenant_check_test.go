package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// rawStoreAllowed are the functions that may name the unscoped store, s.Store, by
// file:function. Everything else reaches the store through db(ctx), which is the
// request's tenant view in a request and the unscoped store only in background work.
var rawStoreAllowed = map[string]string{
	"tenant.go:db":             "is the one place a request's tenant view is made",
	"tenant.go:elsewhere":      "tells another tenant's row from a row no tenant has",
	"tenant.go:users":          "the user directory is the deployment's, not a tenant's",
	"account.go:PreparePublic": "start-up: the deployment's account store and sign-in",
	"reload.go:postFeed":       "checks there is a store before it writes through db",
	"crook.go:startCrookLog":   "the verdict writer: it puts each verdict in the tenant of its request",
	"feedhub.go:newFeedHub":    "the feed's wake signal reads every tenant's items to wake the desk; streams read through db",
	"desk.go:background":       "the front desk is background work over the unscoped store",
	"cluster.go:*":             "the cluster's routes are deployment routes (routeRules), refused to other tenants; the file is held by another lane",
	"clusterlink.go:*":         "the worker link is the deployment's",
	"clusterwords.go:*":        "the cluster's words are the deployment's",
	"widen.go:*":               "the widen route's {id} is checked by owned; the file is held by another lane",
}

// backgroundInHandler are handlers that may start from a fresh context, by
// file:function, because what they start is not the request's (a context no request made
// gets the unscoped store from db).
var backgroundInHandler = map[string]string{
	"daemon.go:reviewBots": "the review bots read is a deployment route with one cache for the daemon",
}

// The daemon's handlers use the request's tenant store: no function names s.Store but
// the few that must, and no function serving a request makes a fresh context, which
// would read db as the unscoped store.
func TestHandlersUseTheTenantStore(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := f + ":" + fn.Name.Name
			allowed := rawStoreAllowed[key] != "" || rawStoreAllowed[f+":*"] != ""
			handler := takesRequest(fn.Type)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncLit:
					if takesRequest(x.Type) {
						handler = true
					}
				case *ast.SelectorExpr:
					if x.Sel.Name == "Store" && isServer(x.X) {
						seen[key] = true
						if !allowed {
							bad = append(bad, fset.Position(x.Pos()).String()+": "+fn.Name.Name+" names s.Store: use s.db(ctx) or s.rdb(r)")
						}
					}
				case *ast.CallExpr:
					if handler && isFreshContext(x) && backgroundInHandler[key] == "" && rawStoreAllowed[f+":*"] == "" {
						bad = append(bad, fset.Position(x.Pos()).String()+": "+fn.Name.Name+" serves a request but makes a fresh context: db would give it the unscoped store")
					}
				}
				return true
			})
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	// The allow list only shrinks: an entry no function needs any more goes.
	for k := range rawStoreAllowed {
		if !strings.HasSuffix(k, ":*") && !seen[k] {
			t.Errorf("rawStoreAllowed has %s, which no longer names s.Store: remove it", k)
		}
	}
}

// takesRequest says whether a function has an *http.Request parameter.
func takesRequest(ft *ast.FuncType) bool {
	for _, p := range ft.Params.List {
		if st, ok := p.Type.(*ast.StarExpr); ok {
			if sel, ok := st.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Request" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "http" {
					return true
				}
			}
		}
	}
	return false
}

// isServer says whether an expression is the Server: s, or something's .s.
func isServer(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "s"
	case *ast.SelectorExpr:
		return x.Sel.Name == "s"
	}
	return false
}

// isFreshContext says whether a call is context.Background() or context.TODO().
func isFreshContext(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "context" && (sel.Sel.Name == "Background" || sel.Sel.Name == "TODO")
}
