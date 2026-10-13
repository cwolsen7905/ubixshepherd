package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
)

// runUpdate is `shepherd update status|dismiss|upgrade`: what the daemon's daily release
// check found, hiding its notice, and having the daemon install it (docs/update-check.md).
// It reads the daemon's cache and never calls GitHub itself; the daemon does the upgrade.
func runUpdate(ctx context.Context, env Env, args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("update "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	now := fs.Bool("now", false, "upgrade only: restart at once, interrupting running agents")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || (sub != "status" && sub != "dismiss" && sub != "upgrade") || (*now && sub != "upgrade") {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	if sub == "upgrade" {
		return runUpgrade(ctx, env, c, *now, *asJSON)
	}
	st, err := c.Update(ctx)
	if err != nil {
		return err
	}
	if sub == "dismiss" {
		if st.Latest == "" {
			return fmt.Errorf("no release has been found yet, so there is nothing to dismiss")
		}
		if st, err = c.DismissUpdate(ctx, st.Latest); err != nil {
			return err
		}
	}
	if *asJSON {
		return printJSON(env, st)
	}
	writeUpdate(env.Stdout, st)
	return nil
}

func writeUpdate(w io.Writer, st api.UpdateStatus) {
	fmt.Fprintf(w, "this build: %s", st.Current)
	if !st.CurrentKnown {
		fmt.Fprint(w, " (not a release version, so it is not compared)")
	}
	fmt.Fprintln(w)
	switch {
	case !st.Enabled && st.Latest == "":
		fmt.Fprintln(w, "update check: off (update.check in the config)")
		return
	case st.Latest == "":
		fmt.Fprintf(w, "latest (%s): not found yet", st.Channel)
		if st.Error != "" {
			fmt.Fprintf(w, "; the last check failed: %s", st.Error)
		}
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "latest (%s): %s\n", st.Channel, st.Latest)
	switch {
	case st.Available:
		fmt.Fprintln(w, "  a newer release is available")
	case st.CurrentKnown:
		fmt.Fprintln(w, "  up to date")
	}
	if st.URL != "" {
		fmt.Fprintf(w, "  %s\n", st.URL)
	}
	if st.Dismissed {
		fmt.Fprintln(w, "  the notice is dismissed until a newer release appears")
	}
	if !st.Enabled {
		fmt.Fprintln(w, "  update check is off (update.check in the config); this is the last answer")
	}
	if st.Error != "" {
		fmt.Fprintf(w, "  the last check failed: %s\n", st.Error)
	}
	writeUpgrade(w, st)
	if !st.CheckedAt.IsZero() {
		fmt.Fprintf(w, "  checked %s\n", st.CheckedAt.Local().Format("Mon 15:04"))
	}
}

// writeUpgrade says how the available release gets installed, or where an upgrade stands.
func writeUpgrade(w io.Writer, st api.UpdateStatus) {
	u := st.Upgrade
	switch u.State {
	case api.UpgradeInstalling:
		fmt.Fprintf(w, "  installing %s\n", u.Target)
	case api.UpgradeInstalled:
		fmt.Fprintf(w, "  %s is installed; the daemon restarts onto it when no agent is running (%d running)\n", u.Target, u.ActiveRuns)
		fmt.Fprintln(w, "  shepherd update upgrade --now restarts at once and interrupts them")
	case api.UpgradeRestarting:
		fmt.Fprintf(w, "  restarting onto %s\n", u.Target)
	case api.UpgradeFailed, api.UpgradeRolledBack:
		fmt.Fprintf(w, "  the upgrade to %s failed: %s\n", u.FailedVersion, u.Error)
	}
	if st.Available || !st.CurrentKnown {
		switch {
		case u.CanUpgrade && u.State == api.UpgradeIdle:
			fmt.Fprintln(w, "  shepherd update upgrade installs it")
		case u.Hint != "":
			fmt.Fprintf(w, "  %s\n", u.Hint)
		}
	}
	if u.Auto {
		fmt.Fprintln(w, "  update.auto is on: the daemon installs a newer release by itself")
	}
}

// upgradeWait bounds how long `update upgrade` follows the daemon.
const upgradeWait = 10 * time.Minute

// runUpgrade starts the daemon's upgrade and follows it: the install, the wait for an
// idle moment (or --now), and the restart, until the new version answers.
func runUpgrade(ctx context.Context, env Env, c *client.Client, now, asJSON bool) error {
	st, err := c.Update(ctx)
	if err != nil {
		return err
	}
	if st.Latest == "" {
		return fmt.Errorf("no release has been found yet, so there is nothing to install")
	}
	if st.Upgrade.State == api.UpgradeIdle || st.Upgrade.State == api.UpgradeFailed || st.Upgrade.State == api.UpgradeRolledBack {
		if !st.Upgrade.CanUpgrade {
			if st.Upgrade.Hint != "" {
				return fmt.Errorf("%s", st.Upgrade.Hint)
			}
			return fmt.Errorf("%s is not newer than this build (%s)", st.Latest, st.Current)
		}
	}
	version := st.Latest
	if st.Upgrade.State == api.UpgradeInstalled {
		version = st.Upgrade.Target
	}
	if st, err = c.UpgradeUpdate(ctx, version, now); err != nil {
		return err
	}
	if asJSON {
		return printJSON(env, st)
	}
	fmt.Fprintf(env.Stdout, "upgrading to %s\n", version)
	deadline := time.Now().Add(upgradeWait)
	last := ""
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		cur, err := c.Update(ctx)
		if err != nil {
			continue // the daemon is restarting
		}
		if cur.Current == version {
			fmt.Fprintf(env.Stdout, "now running %s\n", cur.Current)
			return nil
		}
		u := cur.Upgrade
		if u.State != last {
			last = u.State
			switch u.State {
			case api.UpgradeInstalled:
				fmt.Fprintf(env.Stdout, "installed; restarting when no agent is running (%d running); --now restarts at once\n", u.ActiveRuns)
			case api.UpgradeRestarting:
				fmt.Fprintln(env.Stdout, "restarting")
			}
		}
		if u.State == api.UpgradeFailed || u.State == api.UpgradeRolledBack {
			return fmt.Errorf("upgrade to %s failed: %s", version, u.Error)
		}
		if u.State == api.UpgradeIdle && cur.Current != version && !cur.Available {
			return nil
		}
	}
	return fmt.Errorf("still upgrading after %s; shepherd update status shows where it stands", upgradeWait)
}
