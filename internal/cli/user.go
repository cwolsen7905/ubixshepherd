package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
)

// runUser manages a hosted daemon's users (docs/hosted.md). Like `shepherd account`, it
// runs on the daemon's own machine with the operator token: users are never managed from
// afar.
func runUser(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	fs := flags("user "+sub, env)
	stdin := fs.Bool("password-stdin", false, "read the password from standard input, one line (for scripts)")
	role := fs.String("role", "", "the role in the tenant: owner, admin, member (default) or viewer")
	tenant := fs.String("tenant", "", "the tenant to add the user to (default: default)")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, rest)
	if err != nil {
		return err
	}
	want := map[string]int{"add": 1, "list": 0, "role": 3, "set-password": 1, "signout": 1, "remove": 1}
	n, ok := want[sub]
	if !ok || len(pos) != n {
		return errUsage
	}
	if env.Remote != nil {
		return errors.New("shepherd user works on the daemon's own machine: users are never managed over the network")
	}
	var password string
	if sub == "add" || sub == "set-password" {
		if password, err = readNewPassword(env, *stdin); err != nil {
			return err
		}
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	rt, err := daemon.ReadRuntime(env.Layout.Runtime())
	if err != nil {
		return err
	}
	call := func(method, path string, body, out any) error {
		return webCall(ctx, c.Addr(), rt.Token, env.Client, method, path, body, out)
	}
	at := func(name, action string) string {
		return api.PathUsers + "/" + url.PathEscape(name) + "/" + action
	}
	w := env.Stdout
	switch sub {
	case "list":
		var out api.UserList
		if err := call(http.MethodGet, api.PathUsers, nil, &out); err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, out)
		}
		for _, u := range out.Users {
			var ms []string
			for _, m := range u.Memberships {
				ms = append(ms, m.Tenant+":"+m.Role)
			}
			state := ""
			if !u.HasPassword {
				state = "  (no password)"
			}
			fmt.Fprintf(w, "%-20s %s%s\n", u.Name, strings.Join(ms, " "), state)
		}
		return nil
	case "add", "set-password":
		var out api.AccountSetup
		var err error
		if sub == "add" {
			err = call(http.MethodPost, api.PathUsers, api.UserAdd{Name: pos[0], Password: password, Tenant: *tenant, Role: *role}, &out)
		} else {
			err = call(http.MethodPost, at(pos[0], "password"), api.UserPassword{Password: password}, &out)
		}
		if err != nil {
			return err
		}
		if sub == "add" {
			fmt.Fprintf(w, "User %s is added.\n", pos[0])
		} else {
			fmt.Fprintf(w, "The password of %s is set. Their browser sessions and device tokens have ended.\n", pos[0])
		}
		fmt.Fprintln(w, "\nGive them this for an authenticator app (shown once):")
		fmt.Fprintf(w, "  %s\n  or type the key: %s\n", out.URI, out.Secret)
		fmt.Fprintln(w, "\nTheir recovery codes (shown once; each works once in place of a code):")
		for _, rc := range out.RecoveryCodes {
			fmt.Fprintf(w, "  %s\n", rc)
		}
		return nil
	case "role":
		var out api.User
		if err := call(http.MethodPost, at(pos[0], "role"), api.UserRole{Tenant: pos[1], Role: pos[2]}, &out); err != nil {
			return err
		}
		if pos[2] == "none" {
			fmt.Fprintf(w, "%s is no longer in %s\n", pos[0], pos[1])
		} else {
			fmt.Fprintf(w, "%s is %s in %s\n", pos[0], pos[2], pos[1])
		}
		return nil
	case "signout":
		var out api.SignedOutEverywhere
		if err := call(http.MethodPost, at(pos[0], "signout"), struct{}{}, &out); err != nil {
			return err
		}
		fmt.Fprintf(w, "ended %s and %s of %s\n", plural(out.Sessions, "browser session", "browser sessions"), plural(out.Devices, "device token", "device tokens"), pos[0])
		return nil
	default: // remove
		var out api.UserRemoved
		if err := call(http.MethodPost, at(pos[0], "remove"), struct{}{}, &out); err != nil {
			return err
		}
		fmt.Fprintf(w, "removed %s, and ended %s\n", pos[0], plural(out.Sessions, "browser session", "browser sessions"))
		return nil
	}
}
