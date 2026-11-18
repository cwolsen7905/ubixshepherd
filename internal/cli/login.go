package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/config"
)

// tokenDir is where device tokens are kept, one 0600 file per hosted daemon, in the
// client's own home. Never hosts.yaml, the environment or a log.
func tokenDir(env Env) string { return filepath.Join(env.Layout.Home, "tokens") }

// hostedTarget is the hosted daemon a login or logout names: its address as the first
// argument, or --host https://... before the command.
func hostedTarget(env Env, pos []string) (string, error) {
	switch {
	case len(pos) > 1:
		return "", errUsage
	case len(pos) == 1:
		u, err := config.NormalizeURL(pos[0])
		if err != nil {
			return "", fmt.Errorf("bad address %q: it %v", pos[0], err)
		}
		return u, nil
	case env.Remote != nil && env.Remote.url != "":
		return env.Remote.url, nil
	case env.Remote != nil:
		return "", errors.New("login is for a hosted daemon at an https:// address, not one reached over ssh")
	}
	return "", errUsage
}

// runLogin is `shepherd login`: password and a code (an authenticator code, or one
// recovery code) for a device token, kept in a 0600 file in the home.
func runLogin(ctx context.Context, env Env, args []string) error {
	fs := flags("login", env)
	name := fs.String("name", "", "the name this device is listed under (default: this machine's name)")
	stdin := fs.Bool("password-stdin", false, "read the password, then the code, one line each from standard input (for scripts)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	url, err := hostedTarget(env, pos)
	if err != nil {
		return err
	}
	if *name == "" {
		*name, _ = os.Hostname()
	}
	pw, code, err := readLogin(env, url, *stdin)
	if err != nil {
		return err
	}
	creds, err := client.Login(ctx, url, hostedHTTP, client.LoginRequest{Password: pw, Code: code, Name: *name})
	if err != nil {
		return err
	}
	if err := client.SaveCredentials(tokenDir(env), creds); err != nil {
		return fmt.Errorf("logged in, but the token could not be saved: %w", err)
	}
	fmt.Fprintf(env.Stdout, "Logged in to %s as %q.\n", url, creds.Name)
	fmt.Fprintf(env.Stdout, "The token stays valid while you use it: it ends after 30 days unused, and on %s whatever its use.\n", creds.MaxExpires.Local().Format("2006-01-02"))
	return nil
}

// readLogin asks for the password (without echo) and the code on a terminal, or reads
// them from stdin, one line each, with --password-stdin. Never a flag or the environment.
func readLogin(env Env, url string, fromStdin bool) (password, code string, err error) {
	if fromStdin {
		r := bufio.NewReader(env.Stdin)
		pw, perr := r.ReadString('\n')
		cd, cerr := r.ReadString('\n')
		if (perr != nil && pw == "") || (cerr != nil && cd == "") {
			return "", "", errors.New("--password-stdin wants two lines on standard input: the password, then the code")
		}
		return strings.TrimRight(pw, "\r\n"), strings.TrimSpace(cd), nil
	}
	if !env.Interactive {
		return "", "", errors.New("shepherd login asks on a terminal; use --password-stdin in a script")
	}
	fmt.Fprintf(env.Stderr, "Logging in to %s\nPassword: ", url)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(env.Stderr)
	if err != nil {
		return "", "", err
	}
	fmt.Fprint(env.Stderr, "Code (authenticator, or a recovery code): ")
	cd, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && cd == "" {
		return "", "", errors.New("no code entered")
	}
	return string(pw), strings.TrimSpace(cd), nil
}

// runLogout ends this machine's login to a hosted daemon: the daemon is asked to revoke
// the device token (best effort: a token already ended or a daemon out of reach does not
// stop it), and the saved file is deleted either way.
func runLogout(ctx context.Context, env Env, args []string) error {
	fs := flags("logout", env)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	url, err := hostedTarget(env, pos)
	if err != nil {
		return err
	}
	creds, err := client.LoadCredentials(tokenDir(env), url)
	if errors.Is(err, client.ErrNotLoggedIn) {
		fmt.Fprintf(env.Stdout, "Not logged in to %s.\n", url)
		return nil
	}
	if err != nil { // damaged: nothing to revoke with, so just remove it
		if derr := client.DeleteCredentials(tokenDir(env), url); derr != nil {
			return derr
		}
		fmt.Fprintf(env.Stdout, "Removed the saved login for %s.\n", url)
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	revoked := true
	if err := client.Revoke(rctx, creds, hostedHTTP); err != nil {
		var again *client.LoginAgain
		revoked = errors.As(err, &again) // already ended counts
		if !revoked {
			fmt.Fprintf(env.Stderr, "could not revoke the token on %s (%v); revoke it from Settings, \"Signed-in devices\"\n", url, err)
		}
	}
	if err := client.DeleteCredentials(tokenDir(env), url); err != nil {
		return err
	}
	if revoked {
		fmt.Fprintf(env.Stdout, "Logged out of %s; the token is revoked.\n", url)
	} else {
		fmt.Fprintf(env.Stdout, "Logged out of %s on this machine.\n", url)
	}
	return nil
}
