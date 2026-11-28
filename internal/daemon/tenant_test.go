package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// workspaceNames is what GET /v1/workspaces shows the caller, by name.
func workspaceNames(t *testing.T, code int, body string) string {
	t.Helper()
	if code != http.StatusOK {
		return "(" + http.StatusText(code) + ": " + body + ")"
	}
	var wss []struct{ Name string }
	if err := json.Unmarshal([]byte(body), &wss); err != nil {
		t.Fatalf("workspaces: %v: %s", err, body)
	}
	var names []string
	for _, ws := range wss {
		names = append(names, ws.Name)
	}
	return strings.Join(names, ",")
}

// A request acts in one tenant: a user's only one, or the one they name with the header
// when they are in several; never one they are not a member of. The operator names any;
// worker and desk tokens act in the default tenant only.
func TestRequestsActInOneTenant(t *testing.T) {
	rig := leakDaemon(t, func(st store.Store) {
		ctx := context.Background()
		seedWorld(t, st.ForTenant(store.DefaultTenantID), "home")
		seedWorld(t, st.ForTenant(otherTenant), "away")
		users := st.(interface{ Users() auth.Users }).Users()
		carol, err := users.AddUser(ctx, "carol", "default", auth.RoleMember, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := users.SetRole(ctx, carol.ID, "other", auth.RoleViewer); err != nil {
			t.Fatal(err)
		}
		if _, err := users.AddUser(ctx, "dave", "other", auth.RoleAdmin, time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	for _, name := range []string{"carol", "dave"} {
		rig.resessionNew(t, name)
	}
	ws := func(user, tenant string) string {
		hdr := map[string]string{}
		if tenant != "" {
			hdr[api.TenantHeader] = tenant
		}
		code, body := rig.call(t, user, http.MethodGet, api.PathWorkspaces, nil, hdr)
		return workspaceNames(t, code, body)
	}
	for _, c := range []struct{ user, tenant, want string }{
		{"alice", "", "ws-home"},
		{"alice", "default", "ws-home"},
		{"alice", "other", "(Forbidden: " + `{"error":"no such tenant, or you are not a member of it"}` + "\n)"},
		{"alice", "nowhere", "(Forbidden: " + `{"error":"no such tenant, or you are not a member of it"}` + "\n)"},
		{"bob", "", "ws-away"},
		{"carol", "", "ws-home"},
		{"carol", "other", "ws-away"},
		{"dave", "", "ws-away"},
	} {
		if got := ws(c.user, c.tenant); got != c.want {
			t.Errorf("%s with tenant %q sees %q, want %q", c.user, c.tenant, got, c.want)
		}
	}
	// carol is a viewer in other: she reads there and changes nothing.
	code, body := rig.call(t, "carol", http.MethodPost, api.PathNext, map[string]any{"title": "x"}, map[string]string{api.TenantHeader: "other"})
	if code != http.StatusForbidden || !strings.Contains(body, "your role in tenant other is viewer") {
		t.Errorf("carol adds next-up in other: %d %s", code, body)
	}
	if code, body := rig.call(t, "carol", http.MethodPost, api.PathNext, map[string]any{"title": "x"}, nil); code != http.StatusOK {
		t.Errorf("carol adds next-up at home: %d %s", code, body)
	}

	// The operator: the default tenant, or the one the header names.
	op := func(tenant string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, rig.base+api.PathWorkspaces, nil)
		req.Header.Set("Authorization", "Bearer "+rig.s.Token)
		if tenant != "" {
			req.Header.Set(api.TenantHeader, tenant)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}
	for tenant, want := range map[string]string{"": "ws-home", "default": "ws-home", "other": "ws-away", "spare": ""} {
		code, body := op(tenant)
		if got := workspaceNames(t, code, body); got != want {
			t.Errorf("operator with tenant %q sees %q, want %q", tenant, got, want)
		}
	}
	if code, _ := op("nowhere"); code != http.StatusNotFound {
		t.Errorf("operator naming no tenant: %d, want 404", code)
	}

	// A desk token acts in the default tenant and is refused another.
	tok, err := rig.s.MintDesk(true, "")
	if err != nil {
		t.Fatal(err)
	}
	for tenant, want := range map[string]int{"": http.StatusOK, "default": http.StatusOK, "other": http.StatusForbidden} {
		req, _ := http.NewRequest(http.MethodGet, rig.base+api.PathWorkspaces, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		if tenant != "" {
			req.Header.Set(api.TenantHeader, tenant)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("desk token with tenant %q: %d, want %d", tenant, resp.StatusCode, want)
		}
	}
}

// resessionNew gives a user the suite's rig did not start with a browser session.
func (rig *leakRig) resessionNew(t *testing.T, user string) {
	t.Helper()
	u, err := rig.s.users().UserByAlias(context.Background(), auth.AliasLocal, user)
	if err != nil {
		t.Fatal(err)
	}
	val, ws, err := rig.s.web.startFor(rig.s.listenPort(), true, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	rig.cookies[user] = &http.Cookie{Name: "shepherd_" + rig.s.listenPort(), Value: val}
	rig.csrf[user] = ws.csrf
}
