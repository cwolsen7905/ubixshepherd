package daemon

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/auth"
	"github.com/ubixsys/ubixshepherd/internal/login"
)

const alicePW = "alice's own password"

// addAlice adds alice as a member of default with a password, through the operator route.
func (r *hrig) addAlice() api.AccountSetup {
	r.t.Helper()
	var setup api.AccountSetup
	if code := call(r.t, r.loop, r.s.Token, "POST", api.PathUsers, api.UserAdd{Name: "alice", Password: alicePW}, &setup); code != 200 {
		r.t.Fatalf("add alice: %d", code)
	}
	if len(setup.RecoveryCodes) != 10 || setup.Secret == "" {
		r.t.Fatalf("alice's setup: %+v", setup)
	}
	return setup
}

// loginAs signs a user in on the public listener: a browser session, or a device token.
func (r *hrig) loginAs(user, pw, code string, device bool) (session, string) {
	r.t.Helper()
	path, hdr := api.PathWebLogin, map[string]string{"Origin": pubURL}
	if device {
		path, hdr = api.PathDeviceLogin, nil
	}
	resp, b := r.req("POST", path, hdr, map[string]string{"user": user, "password": pw, "code": code, "name": user + "-laptop"})
	if resp.StatusCode != 200 {
		r.t.Fatalf("%s login: %d %s", user, resp.StatusCode, b)
	}
	var out struct{ CSRF, Token string }
	json.Unmarshal(b, &out)
	if device {
		return session{}, out.Token
	}
	for _, c := range resp.Cookies() {
		if c.Name == login.CookieName {
			return session{cookie: c.Name + "=" + c.Value, csrf: out.CSRF}, ""
		}
	}
	r.t.Fatal("no session cookie")
	return session{}, ""
}

// Two users each see only their own device tokens; signing out everywhere ends the
// caller's sessions and tokens only, across a restart; the operator can sign a user out
// and remove one, and a removed user's session and token stop working.
func TestHostedUsersAreKeptApart(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	alice := r.addAlice()
	aliceCode := func() string {
		c, err := auth.TOTPCodeAt(alice.Secret, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	ownerSess, _ := r.loginAs("", ownPW, r.totp(), false) // no name: the owner, as before
	_, ownerTok := r.loginAs("owner", ownPW, r.setup.RecoveryCodes[0], true)
	aliceSess, _ := r.loginAs("alice", alicePW, aliceCode(), false)
	_, aliceTok := r.loginAs("alice", alicePW, alice.RecoveryCodes[0], true)
	bearer := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

	devices := func(hdr map[string]string) []string {
		t.Helper()
		resp, b := r.req("GET", api.PathDevices, hdr, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("devices: %d %s", resp.StatusCode, b)
		}
		var out api.DeviceList
		json.Unmarshal(b, &out)
		var names []string
		for _, d := range out.Devices {
			names = append(names, d.Name)
		}
		return names
	}
	if got := devices(bearer(aliceTok)); len(got) != 1 || got[0] != "alice-laptop" {
		t.Fatalf("alice's devices = %v", got)
	}
	if got := devices(ownerSess.h()); len(got) != 1 || got[0] != "owner-laptop" {
		t.Fatalf("the owner's devices = %v", got)
	}

	alive := func(label string, hdr map[string]string, want int) {
		t.Helper()
		path := api.PathStatus
		if _, ok := hdr["Cookie"]; ok {
			path = api.PathWebSession
		}
		if resp, _ := r.req("GET", path, hdr, nil); resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", label, resp.StatusCode, want)
		}
	}

	// Alice signs out everywhere: hers end, the owner's stay, also after a restart.
	h := aliceSess.h()
	h[api.CSRFHeader] = aliceSess.csrf
	resp, b := r.req("POST", api.PathWebSignoutAll, h, struct{}{})
	var out api.SignedOutEverywhere
	json.Unmarshal(b, &out)
	if resp.StatusCode != 200 || out.Sessions != 1 || out.Devices != 1 {
		t.Fatalf("alice's sign out everywhere: %d %s", resp.StatusCode, b)
	}
	r.start()
	alive("alice's session", aliceSess.h(), 401)
	alive("alice's token", bearer(aliceTok), 401)
	alive("the owner's session", ownerSess.h(), 200)
	alive("the owner's token", bearer(ownerTok), 200)

	// The operator signs alice out, then removes her; the owner is untouched.
	aliceSess, _ = r.loginAs("alice", alicePW, alice.RecoveryCodes[1], false)
	var so api.SignedOutEverywhere
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/alice/signout", struct{}{}, &so); code != 200 || so.Sessions != 1 {
		t.Fatalf("operator signs alice out: %d %+v", code, so)
	}
	alive("alice's session after the operator's sign-out", aliceSess.h(), 401)
	aliceSess, _ = r.loginAs("alice", alicePW, alice.RecoveryCodes[2], false)
	_, aliceTok = r.loginAs("alice", alicePW, alice.RecoveryCodes[3], true)
	var rm api.UserRemoved
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/alice/remove", struct{}{}, &rm); code != 200 || !rm.Removed {
		t.Fatalf("remove alice: %d %+v", code, rm)
	}
	alive("a removed user's session", aliceSess.h(), 401)
	alive("a removed user's token", bearer(aliceTok), 401)
	alive("the owner's session after alice went", ownerSess.h(), 200)
	if resp, b := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, map[string]string{"user": "alice", "password": alicePW, "code": alice.RecoveryCodes[4]}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a removed user signs in: %d %s", resp.StatusCode, b)
	}
}

// Wrong password, wrong code and an unknown user all get the one answer.
func TestHostedUnknownUserGetsTheWrongPasswordAnswer(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	r.addAlice()
	var answers []string
	for _, b := range []map[string]string{
		{"user": "alice", "password": "not it at all", "code": "123456"},
		{"user": "alice", "password": alicePW, "code": "000000"},
		{"user": "mallory", "password": alicePW, "code": "123456"},
	} {
		resp, body := r.req("POST", api.PathWebLogin, map[string]string{"Origin": pubURL}, b)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%v: %d %s", b, resp.StatusCode, body)
		}
		answers = append(answers, string(body))
	}
	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Fatalf("answers differ: %q", answers)
	}
}

// The operator's user routes: list, roles, the last owner kept, and none of it reachable
// on the public listener.
func TestHostedUserRoutes(t *testing.T) {
	r := newHRig(t)
	r.setAccount()
	r.addAlice()
	var dup map[string]any
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers, api.UserAdd{Name: "alice", Password: alicePW}, &dup); code != http.StatusConflict {
		t.Fatalf("alice again: %d", code)
	}
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers, api.UserAdd{Name: "bob", Password: "short"}, &dup); code != http.StatusBadRequest {
		t.Fatalf("a short password: %d", code)
	}
	var list api.UserList
	call(t, r.loop, r.s.Token, "GET", api.PathUsers, nil, &list)
	if len(list.Users) != 2 || list.Users[0].Name != "alice" || list.Users[0].Memberships[0].Role != "member" || list.Users[1].Memberships[0].Role != "owner" {
		t.Fatalf("users = %+v", list.Users)
	}
	var u api.User
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/alice/role", api.UserRole{Tenant: "default", Role: "admin"}, &u); code != 200 || u.Memberships[0].Role != "admin" {
		t.Fatalf("alice admin: %d %+v", code, u)
	}
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/owner/role", api.UserRole{Tenant: "default", Role: "member"}, &dup); code != http.StatusConflict {
		t.Fatalf("demoting the last owner: %d", code)
	}
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/owner/remove", struct{}{}, &dup); code != http.StatusConflict {
		t.Fatalf("removing the last owner: %d", code)
	}
	if code := call(t, r.loop, r.s.Token, "POST", api.PathUsers+"/nobody/signout", struct{}{}, &dup); code != http.StatusNotFound {
		t.Fatalf("an unknown user: %d", code)
	}
	// From the network, even with the operator token, the routes are not there.
	if resp, _ := r.req("GET", api.PathUsers, map[string]string{"Authorization": "Bearer " + r.s.Token}, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("users on the public listener: %d", resp.StatusCode)
	}
}
