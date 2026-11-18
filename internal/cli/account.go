package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/daemon"
)

// runAccount manages the owner account of a hosted daemon (docs/hosted.md). It runs on
// the daemon's own machine with the operator token: the account is never set from afar.
func runAccount(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	fs := flags("account "+sub, env)
	stdin := fs.Bool("password-stdin", false, "read the new password from standard input, one line (for scripts)")
	if pos, err := parse(fs, rest); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	if sub != "set" && sub != "sign-out-all" {
		return errUsage
	}
	if env.Remote != nil {
		return errors.New("shepherd account works on the daemon's own machine: the account is never set over the network")
	}
	var password string
	if sub == "set" {
		var err error
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
	base := c.Addr()
	if sub == "sign-out-all" {
		var out api.SignedOutEverywhere
		if err := webCall(ctx, base, rt.Token, env.Client, http.MethodPost, api.PathWebSignoutAll, struct{}{}, &out); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "ended %s and %s\n", plural(out.Sessions, "browser session", "browser sessions"), plural(out.Devices, "device token", "device tokens"))
		return nil
	}
	var out api.AccountSetup
	if err := webCall(ctx, base, rt.Token, env.Client, http.MethodPost, api.PathAccount, api.AccountSet{Password: password}, &out); err != nil {
		return err
	}
	w := env.Stdout
	fmt.Fprintln(w, "The owner account is set. Every browser session and device token has ended.")
	fmt.Fprintln(w, "\nAdd this to an authenticator app (shown once):")
	fmt.Fprintf(w, "  %s\n  or type the key: %s\n", out.URI, out.Secret)
	fmt.Fprintln(w, "\nRecovery codes (shown once; each works once in place of a code):")
	for _, rc := range out.RecoveryCodes {
		fmt.Fprintf(w, "  %s\n", rc)
	}
	return nil
}

// readNewPassword asks for the password twice on a terminal, without echo, or reads one
// line from stdin with --password-stdin. It never takes a password from a flag or the
// environment, where a process list or a child process could read it.
func readNewPassword(env Env, fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(env.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password on standard input")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if !env.Interactive {
		return "", errors.New("shepherd account set asks for the password on a terminal; use --password-stdin in a script")
	}
	fd := int(os.Stdin.Fd())
	fmt.Fprint(env.Stderr, "New password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(env.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(env.Stderr, "Again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(env.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("the two passwords differ")
	}
	return string(a), nil
}
