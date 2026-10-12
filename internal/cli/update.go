package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// runUpdate is `shepherd update status|dismiss`: what the daemon's daily release check
// found, and hiding its notice (docs/update-check.md). It reads the daemon's cache and
// never calls GitHub itself.
func runUpdate(ctx context.Context, env Env, args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("update "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || (sub != "status" && sub != "dismiss") {
		return errUsage
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
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
	if !st.CheckedAt.IsZero() {
		fmt.Fprintf(w, "  checked %s\n", st.CheckedAt.Local().Format("Mon 15:04"))
	}
}
