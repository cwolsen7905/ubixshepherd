package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// runKeepGoing is `shepherd keep-going on|off|status`: the switch that lets the front
// desk act on what is already approved (docs/keep-going.md). It is a client of the
// daemon's /v1/keep-going; the daemon enforces the limits and the origin rule.
func runKeepGoing(ctx context.Context, env Env, args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("keep-going "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	capUSD := fs.Float64("cap", 0, "on: dollars it may spend this session (default keep_going.cap)")
	runs := fs.Int("runs", 0, "on: runs that may run at once (default keep_going.max_runs)")
	idle := fs.Float64("idle", 0, "on: hours with nothing started or answered before it turns off (default keep_going.idle_hours)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errUsage
	}
	if sub != "on" && (*capUSD != 0 || *runs != 0 || *idle != 0) {
		return fmt.Errorf("--cap, --runs and --idle go with `keep-going on`")
	}
	if *capUSD < 0 || *runs < 0 || *idle < 0 {
		return fmt.Errorf("--cap, --runs and --idle cannot be negative")
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, "")
	if err != nil {
		return err
	}
	var st api.KeepGoingStatus
	switch sub {
	case "status":
		st, err = c.KeepGoing(ctx, h.Workspace.ID)
	case "on":
		st, err = c.SetKeepGoing(ctx, api.KeepGoingSet{WorkspaceID: h.Workspace.ID, On: true, Cap: *capUSD, MaxRuns: *runs, IdleHours: *idle})
	case "off":
		st, err = c.SetKeepGoing(ctx, api.KeepGoingSet{WorkspaceID: h.Workspace.ID})
	default:
		return errUsage
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env, st)
	}
	writeKeepGoing(env.Stdout, st)
	return nil
}

func writeKeepGoing(w io.Writer, st api.KeepGoingStatus) {
	defer writeRecent(w, st.Recent)
	if st.Summary != "" {
		defer fmt.Fprint(w, st.Summary)
	}
	if !st.On {
		fmt.Fprintln(w, "keep-going: off")
		if st.ReasonOff != "" {
			fmt.Fprintf(w, "  turned off %s: %s\n", st.OffAt.Local().Format("Mon 15:04"), reasonWords(st.ReasonOff))
		}
		fmt.Fprintf(w, "  limits when on: $%.2f, %d runs at once, idle %s, %d starts per lane per day\n",
			st.CapUSD, st.MaxRuns, hoursText(st.IdleHours), st.LaneStarts)
		return
	}
	fmt.Fprintf(w, "keep-going: on, since %s (%s)\n", st.Since.Local().Format("Mon 15:04"), st.By)
	fmt.Fprintf(w, "  spent:  $%.2f of $%.2f\n", st.SpentUSD, st.CapUSD)
	fmt.Fprintf(w, "  runs:   %d of %d\n", st.RunsNow, st.MaxRuns)
	if !st.LastAction.IsZero() {
		fmt.Fprintf(w, "  idle:   since %s, off after %s\n", st.LastAction.Local().Format("15:04"), hoursText(st.IdleHours))
	}
	if !st.DayEnd.IsZero() {
		fmt.Fprintf(w, "  ends:   %s at the latest\n", st.DayEnd.Local().Format("Mon 15:04"))
	}
	if st.BudgetUSD > 0 {
		fmt.Fprintf(w, "  budget: $%.2f of $%.2f today\n", st.SpentTodayUSD, st.BudgetUSD)
	}
	fmt.Fprintf(w, "  a lane gets %d autonomous starts a day\n", st.LaneStarts)
}

func writeRecent(w io.Writer, recent []api.KeepGoingAction) {
	if len(recent) == 0 {
		return
	}
	fmt.Fprintln(w, "  recent:")
	for _, a := range recent {
		fmt.Fprintf(w, "    %s  %s\n", a.At.Local().Format("Mon 15:04"), a.Text)
	}
}

func reasonWords(r string) string {
	switch r {
	case "cap":
		return "it reached its spend cap"
	case "budget":
		return "the daily budget was crossed"
	case "idle":
		return "nothing was started or answered for too long"
	case "day_end":
		return "the day ended"
	case "person":
		return "turned off by the person"
	}
	return r
}

func hoursText(h float64) string {
	return (time.Duration(h * float64(time.Hour))).Round(time.Minute).String()
}
